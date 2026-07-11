package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/jobs"
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
}

func NewRunner(repo jobs.Repository, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch, bin, proxy, format string) *Runner {
	if bin == "" {
		bin = "yt-dlp"
	}
	if format == "" {
		format = "bv*[vcodec^=avc1]+ba[acodec^=mp4a]/bv*[vcodec^=avc1]+ba/b[vcodec^=avc1]/bv*+ba/b"
	}
	return &Runner{repo: repo, pub: pub, bus: b, ban: ban, scratch: scratch, bin: bin, proxy: proxy, format: format}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		if err := r.process(ctx, job); err != nil {
			jobrun.Fail(ctx, r.repo, r.bus, job, err.Error())
		}
	}()
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	slog.Info("ytdlp job started", "job", job.ID, "url", job.Magnet)

	m, err := r.probe(ctx, job.Magnet)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	if m.IsLive {
		return fmt.Errorf("live streams can't be cached")
	}
	if job.Name == "" {
		job.Name = m.Title
	}
	if screen.Blocked(ctx, r.ban, job, m.Title) {
		return fmt.Errorf("content blocked by safety policy")
	}
	if job.MaxBytes > 0 && m.Size > job.MaxBytes {
		return fmt.Errorf("video %dGB exceeds your plan limit of %dGB", m.Size/1e9, job.MaxBytes/1e9)
	}
	job.FileSize = m.Size
	job.Status = jobs.StatusDownloading
	r.repo.Update(ctx, job)

	dir := filepath.Join(r.scratch, job.InfoHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	if err := r.download(ctx, job, dir, m.Size); err != nil {
		return err
	}
	files := collectVideos(dir)
	if len(files) == 0 {
		return fmt.Errorf("no video file produced")
	}
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, files)
}

type meta struct {
	Title  string
	Size   int64
	IsLive bool
}

func (r *Runner) probe(ctx context.Context, url string) (*meta, error) {
	out, err := r.capture(ctx, "-J", "-f", r.format, "--no-warnings", "--no-playlist", url)
	if err != nil {
		return nil, err
	}
	return parseMeta(out)
}

func parseMeta(out []byte) (*meta, error) {
	var j struct {
		Title            string  `json:"title"`
		Filesize         int64   `json:"filesize"`
		FilesizeApprox   int64   `json:"filesize_approx"`
		Duration         float64 `json:"duration"`
		Tbr              float64 `json:"tbr"`
		IsLive           bool    `json:"is_live"`
		RequestedFormats []struct {
			Filesize       int64   `json:"filesize"`
			FilesizeApprox int64   `json:"filesize_approx"`
			Tbr            float64 `json:"tbr"`
		} `json:"requested_formats"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	size := fileSize(j.Filesize, j.FilesizeApprox)
	var sum int64
	for _, f := range j.RequestedFormats {
		sum += fileSize(f.Filesize, f.FilesizeApprox)
	}
	if sum > 0 {
		size = sum
	}
	if size == 0 {
		tbr := j.Tbr
		var sumTbr float64
		for _, f := range j.RequestedFormats {
			sumTbr += f.Tbr
		}
		if sumTbr > 0 {
			tbr = sumTbr
		}
		size = bitrateSize(tbr, j.Duration)
	}
	return &meta{Title: j.Title, Size: size, IsLive: j.IsLive}, nil
}

func fileSize(exact, approx int64) int64 {
	if exact > 0 {
		return exact
	}
	return approx
}

func bitrateSize(tbrKbps, durationSec float64) int64 {
	if tbrKbps <= 0 || durationSec <= 0 {
		return 0
	}
	return int64(tbrKbps * 1000 / 8 * durationSec)
}

func (r *Runner) download(ctx context.Context, job *jobs.Job, dir string, total int64) error {
	cmd := exec.CommandContext(ctx, r.bin, r.args(
		"-o", filepath.Join(dir, "%(title)s.%(ext)s"),
		"-f", r.format,
		"--merge-output-format", "mp4",
		"--no-playlist", "--no-warnings", "--newline", "--restrict-filenames",
		"--concurrent-fragments", "4",
		"--progress-template", "dl:%(progress.downloaded_bytes)s/%(progress.fragment_index)s/%(progress.fragment_count)s",
		job.Magnet,
	)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchdog := time.AfterFunc(stallTimeout, cancel)
	defer watchdog.Stop()
	go func() { <-dlCtx.Done(); _ = cmd.Process.Kill() }()

	rep := jobs.ProgressReporter(ctx, r.repo, job.ID)
	var prog progress
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		bytes, fragIdx, fragCount, ok := parseProgress(sc.Text())
		if !ok {
			continue
		}
		watchdog.Reset(stallTimeout)

		cur := prog.add(bytes)
		if job.MaxBytes > 0 && cur > job.MaxBytes {
			cancel()
			return fmt.Errorf("download exceeds your plan limit of %dGB", job.MaxBytes/1e9)
		}

		switch {
		case total > 0:
			rep(cur, total)
		case fragCount > 0 && fragIdx > 0 && cur > 0:
			rep(cur, cur*fragCount/fragIdx)
		case fragCount > 0:
			rep(fragIdx, fragCount)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if dlCtx.Err() != nil {
			return fmt.Errorf("download stalled — no progress for %s", stallTimeout)
		}
		return fmt.Errorf("yt-dlp: %w", err)
	}
	return nil
}

func (r *Runner) capture(ctx context.Context, extra ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, r.bin, r.args(extra...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp %s: %w", extra[0], err)
	}
	return out, nil
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

func parseProgress(line string) (bytes, fragIdx, fragCount int64, ok bool) {
	rest, found := strings.CutPrefix(line, "dl:")
	if !found {
		return 0, 0, 0, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	bytes = parseNum(parts[0])
	fragIdx = parseNum(parts[1])
	fragCount = parseNum(parts[2])
	return bytes, fragIdx, fragCount, bytes > 0 || fragCount > 0
}

func parseNum(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

type progress struct {
	prior int64
	cur   int64
}

func (p *progress) add(n int64) int64 {
	if n < p.cur {
		p.prior += p.cur
	}
	p.cur = n
	return p.prior + n
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
		out = append(out, publish.File{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: info.Size()})
	}
	return out
}
