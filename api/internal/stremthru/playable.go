package stremthru

import (
	"context"
	"math"
	"path/filepath"

	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type playableJobFiles struct {
	name  string
	size  int64
	files []jobs.File
	node  string
}

func (h *Handler) nodeForInfoHash(ctx context.Context, infoHash string) string {
	if h.Jobs == nil {
		return ""
	}
	return h.Jobs.NodeForInfoHash(ctx, infoHash)
}

func (h *Handler) cachedLookup() jobs.CachedLookup {
	if h.CachedJobs != nil {
		return h.CachedJobs
	}
	if h.Jobs != nil {
		return h.Jobs
	}
	return nil
}

func (h *Handler) warmJobFiles(ctx context.Context, infoHash string) (playableJobFiles, bool) {
	if !manifest.Playable(ctx, h.Store, infoHash) {
		return playableJobFiles{}, false
	}
	name, size, files := h.manifestMeta(ctx, infoHash)
	if len(files) == 0 {
		return playableJobFiles{}, false
	}
	return playableJobFiles{name: name, size: size, files: files, node: h.nodeForInfoHash(ctx, infoHash)}, true
}

func (h *Handler) nodeJobFiles(ctx context.Context, infoHash string) (playableJobFiles, bool) {
	lookup := h.cachedLookup()
	if lookup == nil {
		return playableJobFiles{}, false
	}
	byHash, err := lookup.CachedByHashes(ctx, []string{infoHash})
	if err != nil {
		return playableJobFiles{}, false
	}
	job := byHash[infoHash]
	if !isWarmNodeJob(infoHash, job) {
		return playableJobFiles{}, false
	}
	size := job.FileSize
	if size == 0 {
		for _, file := range job.Files {
			size += file.Size
		}
	}
	return playableJobFiles{name: job.Name, size: size, files: job.Files, node: job.Node}, true
}

func isWarmNodeJob(infoHash string, job *jobs.Job) bool {
	if job == nil || job.Node == "" || len(job.Files) == 0 {
		return false
	}
	for i, file := range job.Files {
		key := manifest.ResolveKey(infoHash, i, file.Key, file.Name)
		if _, _, _, ok := cairn.ParseStreamPath(key); ok {
			return false
		}
	}
	return true
}

func (h *Handler) cairnJobFiles(ctx context.Context, infoHash string) (playableJobFiles, bool) {
	if !h.CairnDirect || h.Cairns == nil || h.CairnStore == nil {
		return playableJobFiles{}, false
	}
	_, name, archived := h.Cairns.GetCairnArchive(ctx, infoHash)
	if !archived {
		return playableJobFiles{}, false
	}
	data, err := h.CairnStore.GetBytes(ctx, nzb.StorageKey(infoHash))
	if err != nil {
		if fallback, ok := h.Cairns.GetCairnNZB(ctx, infoHash); ok {
			data, err = fallback, nil
		}
	}
	if err != nil {
		return playableJobFiles{}, false
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil || len(parsed.Files) == 0 {
		return playableJobFiles{}, false
	}
	enc := h.CairnCipher != nil
	files := make([]jobs.File, len(parsed.Files))
	var total int64
	for i, file := range parsed.Files {
		fileName := file.Filename
		if fileName == "" {
			fileName = file.Subject
		}
		fileName = filepath.Base(fileName)
		if fileName == "." || fileName == "" {
			return playableJobFiles{}, false
		}
		size := file.Size()
		if enc {
			size, err = h.CairnCipher.PlainSize(size)
			if err != nil {
				return playableJobFiles{}, false
			}
		}
		if size < 0 || total > math.MaxInt64-size {
			return playableJobFiles{}, false
		}
		total += size
		files[i] = jobs.File{Index: i, Name: fileName, Size: size, Key: cairn.StreamPath(infoHash, i, fileName), Enc: enc}
	}
	if name == "" {
		name = parsed.Name()
	}
	return playableJobFiles{name: name, size: total, files: files}, true
}

func (h *Handler) cachedJobFiles(ctx context.Context, infoHash string) (playableJobFiles, bool) {
	if cached, ok := h.warmJobFiles(ctx, infoHash); ok {
		return cached, true
	}
	if cached, ok := h.nodeJobFiles(ctx, infoHash); ok {
		return cached, true
	}
	return h.cairnJobFiles(ctx, infoHash)
}

func (h *Handler) cachedFiles(ctx context.Context, userID, infoHash string) (string, []map[string]any, bool) {
	cached, ok := h.cachedJobFiles(ctx, infoHash)
	if !ok {
		return "", nil, false
	}
	return cached.name, h.buildFileEntries(userID, infoHash, cached.node, cached.files), true
}
