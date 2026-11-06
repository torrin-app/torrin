package usenet

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/ingest/internal/jobrun"
	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/providers"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

const torboxDLConns = 8

const torboxUsenetRefPrefix = "torbox-usenet:"

// torboxUsenetRef parses a library-import job's ref (stored in job.Magnet) into
// the TorBox usenet download id to pull.
func torboxUsenetRef(magnet string) (int, bool) {
	if !strings.HasPrefix(magnet, torboxUsenetRefPrefix) {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(magnet, torboxUsenetRefPrefix))
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

func (r *Runner) runTorBoxImport(ctx context.Context, job *jobs.Job, id int) error {
	key, _ := r.users.GetTBKey(ctx, job.UserID)
	if key == "" {
		return failure.UsenetNotSetup
	}
	outDir := filepath.Join(r.scratch, job.InfoHash)
	defer os.RemoveAll(outDir)

	files, err := r.fetchExistingViaTorBox(ctx, job, id, outDir, key, jobs.ProgressReporter(ctx, r.repo, job.ID))
	if err != nil {
		return err
	}
	var size int64
	for _, f := range files {
		size += f.Size
	}
	job.FileSize = size
	r.repo.Update(ctx, job)
	return jobrun.Complete(ctx, r.repo, r.bus, r.pub, job, files)
}

func (r *Runner) fetchViaTorBox(ctx context.Context, job *jobs.Job, parsed *nzb.NZB, dir, key string, onProgress func(int64, int64)) ([]publish.File, error) {
	name := job.Name
	if name == "" {
		name = parsed.Name()
	}
	slog.Info("usenet via torbox", "job", job.ID, "name", name)
	res, err := providers.TorBoxUsenet(ctx, key, name, parsed.Raw, onProgress)
	if err != nil {
		return nil, err
	}
	defer providers.ReleaseTorBoxUsenet(context.WithoutCancel(ctx), key, res.Handle)
	return r.downloadTorBoxResult(ctx, job, res, dir, onProgress)
}

// fetchExistingViaTorBox imports an already-completed usenet download from the
// user's TorBox by id (library import), reusing the same download+publish path.
func (r *Runner) fetchExistingViaTorBox(ctx context.Context, job *jobs.Job, id int, dir, key string, onProgress func(int64, int64)) ([]publish.File, error) {
	slog.Info("usenet library import via torbox", "job", job.ID, "usenet_id", id)
	res, err := providers.TorBoxUsenetByID(ctx, key, id)
	if err != nil {
		return nil, err
	}
	if job.Name == "" {
		job.Name = res.Name
	}
	return r.downloadTorBoxResult(ctx, job, res, dir, onProgress)
}

func (r *Runner) downloadTorBoxResult(ctx context.Context, job *jobs.Job, res *providers.Result, dir string, onProgress func(int64, int64)) ([]publish.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	job.Status = jobs.StatusProcessing
	r.repo.Update(ctx, job)

	var total, done int64
	for _, l := range res.Files {
		total += l.Size
	}
	client := &http.Client{}
	files := make([]publish.File, 0, len(res.Files))
	for _, l := range res.Files {
		path := filepath.Join(dir, filepath.Base(l.Name))
		rep := func(written, _ int64) {
			if onProgress != nil {
				onProgress(done+written, total)
			}
		}
		if err := providers.FetchFileStalled(ctx, client, l.URL, path, rep, torboxDLConns); err != nil {
			return nil, fmt.Errorf("torbox download %s: %w", l.Name, err)
		}
		size := l.Size
		if info, e := os.Stat(path); e == nil {
			size = info.Size()
		}
		done += size
		files = append(files, publish.File{Name: l.Name, Path: path, Size: size})
	}
	return files, nil
}
