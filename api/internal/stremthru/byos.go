package stremthru

import (
	"context"
	"net/url"
	"slices"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/stremioid"
)

type byosLookup interface {
	BYOSByHashes(context.Context, string, []string) (map[string]*jobs.BYOSObject, error)
}

func (h *Handler) privateCopies(ctx context.Context, userID string, hashes []string) map[string]*jobs.BYOSObject {
	lookup := h.BYOS
	if lookup == nil && h.Jobs != nil {
		lookup = h.Jobs
	}
	if lookup == nil || userID == "" || len(hashes) == 0 {
		return nil
	}
	found, err := lookup.BYOSByHashes(ctx, userID, hashes)
	if err != nil {
		return nil
	}
	// Defense in depth for alternate lookup implementations.
	out := map[string]*jobs.BYOSObject{}
	for hash, o := range found {
		if o != nil && o.UserID == userID && o.InfoHash == hash && slices.Contains(hashes, hash) {
			out[hash] = o
		}
	}
	return out
}

func (h *Handler) playableEntries(userID, hash string, cached playableJobFiles, files []jobs.File, target stremioid.ID) []map[string]any {
	entries := h.buildFileEntries(userID, hash, cached.node, files, target)
	for i := range entries {
		if cached.name != "" {
			entries[i]["release_name"] = cached.name
		}
	}
	if !cached.byos {
		return entries
	}
	for i, file := range files {
		key := manifest.Key(hash, file.Index, file.Name)
		entries[i]["link"] = h.Store.SignURLNodeUser("", key, userID, 24*time.Hour) + "&byos=1&bk=" + url.QueryEscape(key) + manifest.StreamQuery(hash, file.Enc)
		entries[i]["stream_source"] = "cache"
	}
	return entries
}

func privateJob(o *jobs.BYOSObject) *jobs.Job {
	return &jobs.Job{UserID: o.UserID, InfoHash: o.InfoHash, Name: o.Name, Files: o.Files, Status: jobs.StatusComplete}
}
