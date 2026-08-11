package usenet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/ingest/internal/screen"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/postproc"
)

type Runner struct {
	repo     jobs.Repository
	store    *storage.Client
	users    *auth.Store
	pub      *publish.Publisher
	bus      *bus.Bus
	ban      screen.BanFunc
	scratch  string
	sysCreds download.Credentials
	cipher   *crypto.Stream
}

func NewRunner(repo jobs.Repository, store *storage.Client, users *auth.Store, pub *publish.Publisher, b *bus.Bus, ban screen.BanFunc, scratch string, sysCreds download.Credentials, cipher *crypto.Stream) *Runner {
	return &Runner{repo: repo, store: store, users: users, pub: pub, bus: b, ban: ban, scratch: scratch, sysCreds: sysCreds, cipher: cipher}
}

func (r *Runner) Run(ctx context.Context, job *jobs.Job, done func()) {
	go func() {
		defer done()
		if err := r.process(ctx, job); err != nil {
			jobrun.Fail(ctx, r.repo, r.bus, job, r.reason(job, err))
		}
	}()
}

func (r *Runner) reason(job *jobs.Job, err error) error {
	var f *failure.Failure
	if errors.As(err, &f) {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), filepath.Join(r.scratch, job.InfoHash)+"/", "")
	return failure.Newf("usenet", "%s", msg)
}

func (r *Runner) process(ctx context.Context, job *jobs.Job) error {
	slog.Info("usenet job started", "job", job.ID)
	data, err := r.store.GetBytes(ctx, nzb.StorageKey(job.InfoHash))
	if err != nil {
		data, err = r.recoverNZB(ctx, job)
		if err != nil {
			return fmt.Errorf("fetch nzb: %w", err)
		}
	}
	return r.RunNZB(ctx, job, data)
}

func (r *Runner) recoverNZB(ctx context.Context, job *jobs.Job) ([]byte, error) {
	data, ok := r.users.GetCairnNZB(ctx, job.InfoHash)
	if !ok {
		return nil, fmt.Errorf("nzb missing")
	}
	if err := r.store.Put(ctx, nzb.StorageKey(job.InfoHash), bytes.NewReader(data), "application/x-nzb"); err != nil {
		slog.Warn("cairn: repopulate nzb cache", "hash", job.InfoHash, "err", err)
	}
	return data, nil
}

func (r *Runner) RunNZB(ctx context.Context, job *jobs.Job, data []byte) error {
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("parse nzb: %w", err)
	}
	if job.MaxBytes > 0 && parsed.TotalSize() > job.MaxBytes {
		return failure.Newf("too_large", "this nzb is %dGB, over your plan limit of %dGB", parsed.TotalSize()/1e9, job.MaxBytes/1e9)
	}
	slog.Info("usenet nzb ok", "job", job.ID, "name", parsed.Name(), "files", len(parsed.Files), "size_gb", parsed.TotalSize()/1e9)

	if job.Name == "" {
		job.Name = parsed.Name()
	}
	job.FileSize = parsed.TotalSize()
	r.repo.Update(ctx, job)

	outDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(outDir)

	files, err := r.fetchToFiles(ctx, job, parsed, outDir, jobs.ProgressReporter(ctx, r.repo, job.ID))
	if err != nil {
		return err
	}
	r.restoreNames(ctx, job.InfoHash, parsed, files)
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, files)
}

func (r *Runner) AssemblePack(ctx context.Context, job *jobs.Job, parts [][]byte) error {
	parsedAll := make([]*nzb.NZB, 0, len(parts))
	var total int64
	for _, p := range parts {
		pn, err := nzb.ParseBytes(p)
		if err != nil {
			return fmt.Errorf("parse episode nzb: %w", err)
		}
		parsedAll = append(parsedAll, pn)
		total += pn.TotalSize()
	}
	if job.MaxBytes > 0 && total > job.MaxBytes {
		return failure.Newf("too_large", "this season pack is %dGB, over your plan limit of %dGB", total/1e9, job.MaxBytes/1e9)
	}
	job.FileSize = total
	r.repo.Update(ctx, job)

	baseDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(baseDir)

	var all []publish.File
	rep := jobs.ProgressReporter(ctx, r.repo, job.ID)
	var doneBytes int64
	for i, pn := range parsedAll {
		epDir := filepath.Join(baseDir, fmt.Sprintf("ep%02d", i+1))
		if err := os.MkdirAll(epDir, 0o755); err != nil {
			return err
		}
		slog.Info("usenet pack episode", "job", job.ID, "ep", i+1, "of", len(parsedAll), "name", pn.Name())
		base := doneBytes
		files, err := r.fetchToFiles(ctx, job, pn, epDir, func(done, _ int64) { rep(base+done, total) })
		if err != nil {
			return fmt.Errorf("episode %d/%d: %w", i+1, len(parsedAll), err)
		}
		doneBytes += pn.TotalSize()
		all = append(all, files...)
	}
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, all)
}

func (r *Runner) fetchToFiles(ctx context.Context, job *jobs.Job, parsed *nzb.NZB, dir string, onProgress func(int64, int64)) ([]publish.File, error) {
	var names []string
	for _, f := range parsed.Files {
		names = append(names, f.Filename, f.Subject)
	}
	if screen.Blocked(ctx, r.ban, job, names...) {
		return nil, fmt.Errorf("content blocked by safety policy")
	}

	creds, system := r.creds(ctx, job.UserID)
	if creds.Host == "" {
		return nil, fmt.Errorf("no usenet provider configured")
	}
	slog.Info("usenet connecting", "job", job.ID, "host", creds.Host, "conns", creds.MaxConns, "shared", system)
	pool, release, err := download.AcquireShared(creds)
	if err != nil {
		return nil, fmt.Errorf("connect usenet: %w", err)
	}
	defer release()

	dlCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lastAt atomic.Int64
	lastAt.Store(time.Now().UnixNano())
	prog := func(doneB, totalB int64) {
		lastAt.Store(time.Now().UnixNano())
		if onProgress != nil {
			onProgress(doneB, totalB)
		}
	}
	go stallWatch(dlCtx, cancel, &lastAt, job.ID)

	slog.Info("usenet downloading", "job", job.ID, "name", parsed.Name())
	if _, err := download.Download(dlCtx, pool, parsed, dir, creds.MaxConns, prog); err != nil {
		if dlCtx.Err() != nil && ctx.Err() == nil {
			return nil, failure.Newf("stalled", "download stalled, no progress for 30 minutes")
		}
		return nil, fmt.Errorf("download: %w", err)
	}

	job.Status = jobs.StatusProcessing
	r.repo.Update(ctx, job)

	if r.cipher != nil {
		if _, _, ok := r.users.GetCairnArchive(ctx, job.InfoHash); ok {
			if err := r.decryptCairn(dir); err != nil {
				return nil, err
			}
		}
	}

	pwds := postproc.PasswordCandidates(parsed.Meta["password"], job.Name, parsed.Name())
	files, err := postproc.Process(dir, pwds, job.Name)
	if err != nil {
		return nil, fmt.Errorf("postproc: %w", err)
	}
	if len(files) == 0 {
		return nil, failure.NoVideo
	}
	pubFiles := make([]publish.File, len(files))
	for i, f := range files {
		pubFiles[i] = publish.File{Name: f.Name, Path: f.Path, Size: f.Size}
	}
	return pubFiles, nil
}

func (r *Runner) decryptCairn(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := decryptInPlace(filepath.Join(dir, e.Name()), r.cipher); err != nil {
			slog.Warn("cairn: decrypt failed", "file", e.Name(), "err", err)
			return failure.Newf("cairn_incomplete", "this archived copy is incomplete and can't be restored")
		}
	}
	return nil
}

func decryptInPlace(path string, cipher *crypto.Stream) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	tmp := path + ".dec"
	out, err := os.Create(tmp)
	if err != nil {
		in.Close()
		return err
	}
	err = cipher.DecryptAll(out, in)
	out.Close()
	in.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (r *Runner) restoreNames(ctx context.Context, hash string, parsed *nzb.NZB, files []publish.File) {
	orig := r.originalNames(ctx, hash, len(parsed.Files))
	if orig == nil {
		return
	}
	byDisk := make(map[string]string, len(parsed.Files))
	for i, f := range parsed.Files {
		byDisk[filepath.Base(download.FileName(f))] = orig[i]
	}
	for i := range files {
		if name := byDisk[filepath.Base(files[i].Name)]; name != "" {
			files[i].Name = name
		}
	}
}

func (r *Runner) originalNames(ctx context.Context, hash string, n int) []string {
	list, err := r.repo.ListByInfoHash(ctx, hash)
	if err != nil {
		return nil
	}
	for _, j := range list {
		if len(j.Files) != n || allBlobNames(j.Files) {
			continue
		}
		names := make([]string, n)
		for i, f := range j.Files {
			names[i] = f.Name
		}
		return names
	}
	return nil
}

func allBlobNames(files []jobs.File) bool {
	for _, f := range files {
		if !isBlobName(f.Name) {
			return false
		}
	}
	return true
}

func isBlobName(name string) bool {
	stem := name[strings.LastIndex(name, "/")+1:]
	if i := strings.LastIndex(stem, "."); i >= 0 {
		stem = stem[:i]
	}
	if len(stem) != 20 {
		return false
	}
	for _, c := range stem {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func stallWatch(ctx context.Context, cancel context.CancelFunc, lastAt *atomic.Int64, jobID string) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if time.Since(time.Unix(0, lastAt.Load())) > 30*time.Minute {
				slog.Warn("usenet download stalled, cancelling", "job", jobID)
				cancel()
				return
			}
		}
	}
}

func (r *Runner) HasProvider(ctx context.Context, userID string) bool {
	c, _ := r.creds(ctx, userID)
	return c.Host != ""
}

func (r *Runner) creds(ctx context.Context, userID string) (download.Credentials, bool) {
	if c, err := r.users.GetUsenetCreds(ctx, userID); err == nil && c.Host != "" {
		return download.Credentials{Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, SSL: c.SSL, MaxConns: c.MaxConns}, false
	}
	if u, err := r.users.GetByID(ctx, userID); err == nil && u != nil {
		if p, ok := plans.Get(u.PlanID); ok && p.SystemUsenet {
			return r.sysCreds, true
		}
	}
	return download.Credentials{}, false
}
