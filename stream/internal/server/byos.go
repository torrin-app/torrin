package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/rclonerc"
	"github.com/torrin-app/torrin/shared/storage"
)

type byosJobs interface {
	GetByInfoHash(ctx context.Context, infoHash string) (*jobs.Job, error)
}

type byosBackend struct {
	users     *auth.Store
	jobs      byosJobs
	rc        *rclonerc.Client
	rcloneURL string
	hc        *http.Client
}

func (s *Server) SetBYOS(users *auth.Store, jobsRepo byosJobs, rc *rclonerc.Client, rcloneURL string) {
	s.byos = &byosBackend{users: users, jobs: jobsRepo, rc: rc, rcloneURL: strings.TrimRight(rcloneURL, "/"), hc: storage.StreamHTTPClient()}
}

func (s *Server) serveBYOS(w http.ResponseWriter, r *http.Request, key, userID string) bool {
	b := s.byos
	if b == nil || userID == "" {
		return false
	}
	creds, err := b.users.GetStorageCreds(r.Context(), userID)
	if err != nil || creds == nil || !creds.Enabled || !creds.IsRclone() {
		return false
	}
	storeKey := r.URL.Query().Get("bk")
	if storeKey == "" {
		storeKey = b.resolveStoreKey(r.Context(), r.URL.Query().Get("ih"), key)
	}
	if storeKey == "" {
		return false
	}
	rctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	remote, err := auth.EnsureRemote(rctx, b.rc, userID, creds)
	cancel()
	if err != nil {
		slog.Warn("stream: byos remote setup failed", "user", userID, "err", err)
		return false
	}

	upstream := b.rcloneURL + "/[" + remote + ":]/" + escapeRclonePath(creds.Prefix+storeKey)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, nil)
	if err != nil {
		return false
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		slog.Warn("stream: byos upstream unreachable", "user", userID, "err", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		slog.Warn("stream: byos upstream error", "user", userID, "status", resp.StatusCode)
		io.Copy(io.Discard, resp.Body)
		return false
	}

	h := w.Header()
	for _, k := range []string{"Content-Type", "Content-Range", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	h.Set("Accept-Ranges", "bytes")
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		streamCopy(w, resp.Body)
	}
	return true
}

func (b *byosBackend) resolveStoreKey(ctx context.Context, ih, key string) string {
	if !strings.HasPrefix(key, "blobs/") {
		return key
	}
	if b.jobs == nil || len(ih) != 40 {
		return ""
	}
	job, err := b.jobs.GetByInfoHash(ctx, ih)
	if err != nil || job == nil {
		return ""
	}
	for i, f := range job.Files {
		if manifest.ResolveKey(ih, i, f.Key, f.Name) == key {
			return manifest.Key(ih, i, f.Name)
		}
	}
	return ""
}

func escapeRclonePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
