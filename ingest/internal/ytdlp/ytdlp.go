package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/video"
)

const stallTimeout = 5 * time.Minute

type Runner struct {
	repo    jobs.Repository
	pub     *publish.Publisher
	bus     *bus.Bus
	ban     screen.BanFunc
	scratch string
	bin     string
	proxy   string
	format  string
	aria2   bool
}

func NewRunner(repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch, bin, proxy, format string) *Runner {
	if bin == "" {
		bin = "yt-dlp"
	}
	if format == "" {
		format = "bv*[vcodec^=avc1]+ba[acodec^=mp4a]/bv*[vcodec^=avc1]+ba/b[vcodec^=avc1]/bv*+ba/b"
	}
	_, ariaErr := exec.LookPath("aria2c")
	return &Runner{repo: repo, pub: pub, bus: b, ban: ban, scratch: scratch, bin: bin, proxy: proxy, format: format, aria2: ariaErr == nil}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		if err := r.process(ctx, job); err != nil {
			jobrun.Fail(ctx, r.repo, r.bus, job, err)
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	slog.Info("ytdlp job started", "job", job.ID, "url", job.Magnet)

	m, err := r.probe(ctx, job.Magnet)
	if err != nil {
		return err
	}
	if m.IsLive {
		return failure.Newf("live", "live streams can't be saved")
	}
	if !m.HasVideo {
		return failure.Newf("no_video", "no video in this link, torrin only supports video for now")
	}
	if job.Name == "" {
		job.Name = m.Title
	}
	if screen.Blocked(ctx, r.ban, job, m.Title) {
		return failure.Blocked
	}
	if job.MaxBytes > 0 && m.Size > job.MaxBytes {
		if m.IsPlaylist {
			return failure.Newf("too_large", "this collection is %dGB across %d videos, over your plan limit of %dGB — add a single video instead", m.Size/1e9, m.Count, job.MaxBytes/1e9)
		}
		return failure.Newf("too_large", "this video (%dGB) is over your plan limit of %dGB", m.Size/1e9, job.MaxBytes/1e9)
	}
	job.FileSize = m.Size
	job.Status = jobs.StatusDownloading
	r.repo.Update(ctx, job)

	dir := filepath.Join(r.scratch, job.InfoHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	if err := r.download(ctx, job, dir, m.Size, m.IsPlaylist); err != nil {
		return err
	}
	files := collectVideos(dir)
	if len(files) == 0 {
		return failure.Newf("no_video", "no video in this link, torrin only supports video for now")
	}
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, files)
}

func (r *Runner) probe(ctx context.Context, url string) (*meta, error) {
	out, err := r.capture(ctx, "-J", "-f", r.format, "--no-warnings", "--no-playlist", url)
	if err != nil {
		return nil, err
	}
	m, err := parseMeta(out)
	if err != nil {
		return nil, err
	}
	if m.IsPlaylist {
		return r.probePlaylist(ctx, url)
	}
	return m, nil
}

func (r *Runner) probePlaylist(ctx context.Context, url string) (*meta, error) {
	out, err := r.capture(ctx, "--flat-playlist", "-J", "--no-warnings", url)
	if err != nil {
		return nil, err
	}
	return parsePlaylist(out)
}

func (r *Runner) download(ctx context.Context, job *jobs.Job, dir string, total int64, playlist bool) error {
	tmpl, listFlag := "%(title)s.%(ext)s", "--no-playlist"
	opts := []string{
		"-f", r.format,
		"--merge-output-format", "mp4",
		"--no-warnings", "--newline", "--restrict-filenames",
		"--concurrent-fragments", "4",
	}
	if r.aria2 {
		opts = append(opts, "--downloader", "aria2c", "--downloader-args", "aria2c:-x4 -s4 -k1M")
	}
	if playlist {
		tmpl, listFlag = "%(playlist_index)03d-%(title)s.%(ext)s", "--yes-playlist"
		opts = append(opts, "--ignore-errors")
	}
	opts = append([]string{"-o", filepath.Join(dir, tmpl), listFlag}, opts...)
	if lim := providers.LimiterFrom(ctx); lim != nil {
		opts = append(opts, "--limit-rate", strconv.FormatInt(int64(lim.Limit()), 10))
	}
	opts = append(opts, job.Magnet)
	cmd := exec.CommandContext(ctx, r.bin, r.args(opts...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stderr
	var errBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &errBuf)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-dlCtx.Done()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	rep := jobs.ProgressReporter(ctx, r.repo, job.ID)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var last int64
	stalledAt := time.Now()
	for {
		select {
		case err := <-waitCh:
			rep(dirSize(dir), total)
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if playlist {
					return nil
				}
				if reason := ytdlpReason(errBuf.String()); reason != "" {
					return failure.Newf("ytdlp", "%s", reason)
				}
				return fmt.Errorf("yt-dlp: %w", err)
			}
			return nil
		case <-ticker.C:
			cur := dirSize(dir)
			if tl := providers.TallyFrom(ctx); tl != nil {
				tl.Downloaded.Store(cur)
			}
			if job.MaxBytes > 0 && cur > job.MaxBytes {
				cancel()
				return failure.Newf("too_large", "this download went over your plan limit of %dGB", job.MaxBytes/1e9)
			}
			rep(cur, total)
			if cur > last {
				last, stalledAt = cur, time.Now()
			} else if time.Since(stalledAt) > stallTimeout {
				cancel()
				return failure.Newf("interrupted", "download stalled, no progress for %s", stallTimeout)
			}
		}
	}
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func (r *Runner) capture(ctx context.Context, extra ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.bin, r.args(extra...)...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		if reason := ytdlpReason(errBuf.String()); reason != "" {
			return nil, failure.Newf("ytdlp", "%s", reason)
		}
		return nil, fmt.Errorf("yt-dlp %s: %w", extra[0], err)
	}
	return out, nil
}

func ytdlpReason(stderr string) string {
	var reason string
	for _, ln := range strings.Split(stderr, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(ln), "ERROR:"); ok {
			reason = strings.TrimSpace(s)
		}
	}
	if strings.HasPrefix(reason, "[") {
		if b := strings.IndexByte(reason, ']'); b > 0 {
			rest := strings.TrimSpace(reason[b+1:])
			if c := strings.Index(rest, ": "); c > 0 {
				rest = rest[c+2:]
			}
			reason = strings.TrimSpace(rest)
		}
	}
	if i := strings.Index(reason, " (caused by "); i > 0 {
		reason = reason[:i]
	}
	if i := strings.Index(strings.ToLower(reason), "please report"); i > 0 {
		reason = reason[:i]
	}
	reason = strings.TrimRight(strings.TrimSpace(reason), ".;: ")
	if len(reason) > 200 {
		reason = strings.TrimSpace(reason[:200])
	}
	return reason
}

func (r *Runner) args(extra ...string) []string {
	if r.proxy == "" {
		return extra
	}
	return append([]string{"--proxy", r.proxy}, extra...)
}

func (r *Runner) Extractors(ctx context.Context) ([]byte, error) {
	out, err := r.capture(ctx, "--list-extractors")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ln := range strings.Split(string(out), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			names = append(names, ln)
		}
	}
	return json.Marshal(names)
}

func collectVideos(dir string) []publish.File {
	entries, _ := os.ReadDir(dir)
	var out []publish.File
	for _, e := range entries {
		if e.IsDir() || !video.IsVideo(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, publish.File{Name: cleanName(e.Name()), Path: filepath.Join(dir, e.Name()), Size: info.Size()})
	}
	return out
}

func cleanName(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if video.IsVideo(stem) && strings.EqualFold(filepath.Ext(stem), ext) {
		return stem
	}
	return name
}
