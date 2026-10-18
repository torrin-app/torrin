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

// The checker needs failed storage reads to remain distinct from a missing file.
func (h *Handler) inspectWarmJobFiles(ctx context.Context, infoHash string) (playableJobFiles, string) {
	if h.Store == nil {
		return playableJobFiles{}, "not_configured"
	}
	data, err := h.Store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return playableJobFiles{}, checkFailure(ctx, err, "storage_error")
	}
	name, size, files := manifest.Meta(data)
	if len(files) == 0 {
		return playableJobFiles{}, "invalid_manifest"
	}
	present, err := h.Store.Has(ctx, manifest.ResolveKey(infoHash, 0, files[0].Key, files[0].Name))
	if err != nil {
		return playableJobFiles{}, checkFailure(ctx, err, "storage_error")
	}
	if !present {
		return playableJobFiles{}, "missing_file"
	}
	return playableJobFiles{name: name, size: size, files: files, node: h.nodeForInfoHash(ctx, infoHash)}, "cached"
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
	cached, reason := h.inspectCairnJobFiles(ctx, infoHash)
	return cached, reason == "cached"
}

type checkedCairnRepository interface {
	LookupCairnArchive(context.Context, string) (string, string, bool, error)
	LookupCairnNZB(context.Context, string) ([]byte, bool, error)
}

func (h *Handler) inspectCairnJobFiles(ctx context.Context, infoHash string) (playableJobFiles, string) {
	if !h.CairnDirect || h.Cairns == nil || h.CairnStore == nil {
		return playableJobFiles{}, "not_configured"
	}
	var name string
	var archived bool
	checked, supportsErrors := h.Cairns.(checkedCairnRepository)
	if supportsErrors {
		var err error
		_, name, archived, err = checked.LookupCairnArchive(ctx, infoHash)
		if err != nil {
			return playableJobFiles{}, checkFailure(ctx, err, "database_error")
		}
	} else {
		_, name, archived = h.Cairns.GetCairnArchive(ctx, infoHash)
	}
	if !archived {
		if supportsErrors {
			return playableJobFiles{}, "not_cached"
		}
		return playableJobFiles{}, "lookup_unverified"
	}
	data, err := h.CairnStore.GetBytes(ctx, nzb.StorageKey(infoHash))
	if err != nil {
		if supportsErrors {
			fallback, ok, lookupErr := checked.LookupCairnNZB(ctx, infoHash)
			if lookupErr != nil {
				return playableJobFiles{}, checkFailure(ctx, lookupErr, "database_error")
			}
			if ok {
				data, err = fallback, nil
			}
		} else if fallback, ok := h.Cairns.GetCairnNZB(ctx, infoHash); ok {
			data, err = fallback, nil
		}
	}
	if err != nil {
		return playableJobFiles{}, checkFailure(ctx, err, "storage_error")
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil || len(parsed.Files) == 0 {
		return playableJobFiles{}, "invalid_archive"
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
			return playableJobFiles{}, "invalid_archive"
		}
		size := file.Size()
		if enc {
			size, err = h.CairnCipher.PlainSize(size)
			if err != nil {
				return playableJobFiles{}, "invalid_archive"
			}
		}
		if size < 0 || total > math.MaxInt64-size {
			return playableJobFiles{}, "invalid_archive"
		}
		total += size
		files[i] = jobs.File{Index: i, Name: fileName, Size: size, Key: cairn.StreamPath(infoHash, i, fileName), Enc: enc}
	}
	if name == "" {
		name = parsed.Name()
	}
	return playableJobFiles{name: name, size: total, files: files}, "cached"
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
