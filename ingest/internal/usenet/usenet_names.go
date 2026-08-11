package usenet

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

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
