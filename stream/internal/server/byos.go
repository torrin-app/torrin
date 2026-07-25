package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/rclonerc"
)

type byosBackend struct {
	users     *auth.Store
	rc        *rclonerc.Client
	rcloneURL string
	hc        *http.Client
}

func (s *Server) SetBYOS(users *auth.Store, rc *rclonerc.Client, rcloneURL string) {
	s.byos = &byosBackend{users: users, rc: rc, rcloneURL: strings.TrimRight(rcloneURL, "/"), hc: &http.Client{}}
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
	var params map[string]string
	json.Unmarshal([]byte(creds.ConfigJSON), &params)
	rctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	remote, err := b.rc.EnsureUserRemote(rctx, userID, creds.Backend, params, true, creds.CryptPass, creds.Bucket)
	cancel()
	if err != nil {
		return false
	}

	upstream := b.rcloneURL + "/[" + remote + ":]/" + escapeRclonePath(creds.Prefix+key)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, nil)
	if err != nil {
		return false
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := b.hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
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

func escapeRclonePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
