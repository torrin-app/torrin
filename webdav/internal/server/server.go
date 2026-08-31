package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
)

const davAllow = "OPTIONS, GET, HEAD, PROPFIND, PROPPATCH, LOCK, UNLOCK"

type urlSigner interface {
	SignURLNode(node, path string, expiry time.Duration) string
}

type Server struct {
	users *auth.Store
	jobs  jobs.Repository
	store urlSigner
}

func New(users *auth.Store, j jobs.Repository, store urlSigner) *Server {
	return &Server{users: users, jobs: j, store: store}
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("Allow", davAllow)
		w.Header().Set("MS-Author-Via", "DAV")
		w.WriteHeader(http.StatusOK)
		return
	}
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	var userID, authReason string
	defer func() {
		if sw.status >= 400 {
			slog.Warn("webdav", "method", r.Method, "path", r.URL.Path, "status", sw.status, "user", userID, "reason", authReason)
		}
	}()
	user, err := s.authenticate(r)
	if err != nil {
		authReason = err.Error()
		if user != nil {
			userID = user.ID
		}
		sw.Header().Set("WWW-Authenticate", `Basic realm="Torrin"`)
		http.Error(sw, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID = user.ID
	if r.Method == http.MethodPost {
		s.setOverride(sw, r, user.ID)
		return
	}
	overrides, _ := s.users.WebdavOverrides(r.Context(), user.ID)
	tree := buildTree(s.completed(r.Context(), user.ID), overrides)
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.get(sw, r, user.ID, tree)
		return
	}
	(&webdav.Handler{
		FileSystem: davFS{root: tree},
		LockSystem: webdav.NewMemLS(),
		Logger: func(rq *http.Request, err error) {
			if err != nil {
				slog.Warn("webdav", "method", rq.Method, "path", rq.URL.Path, "err", err)
			}
		},
	}).ServeHTTP(sw, r)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (sw *statusWriter) WriteHeader(code int) {
	if sw.wrote {
		return
	}
	sw.wrote, sw.status = true, code
	sw.ResponseWriter.WriteHeader(code)
}

func (s *Server) completed(ctx context.Context, userID string) []*jobs.Job {
	list, _ := jobs.ListAll(ctx, s.jobs, userID)
	return list
}

func (s *Server) setOverride(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		InfoHash  string `json:"info_hash"`
		FileIndex int    `json:"file_index"`
		Alias     string `json:"alias"`
		Excluded  bool   `json:"excluded"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.InfoHash == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(req.Alias) > 255 {
		http.Error(w, "name too long", http.StatusUnprocessableEntity)
		return
	}
	if err := s.users.SetWebdavOverride(r.Context(), userID, req.InfoHash, req.FileIndex, strings.TrimSpace(req.Alias), req.Excluded); err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) authenticate(r *http.Request) (*auth.User, error) {
	u, p, ok := r.BasicAuth()
	if !ok {
		return nil, fmt.Errorf("no credentials")
	}
	apiKey := u
	if strings.HasPrefix(p, "tr_") {
		apiKey = p
	}
	if !strings.HasPrefix(apiKey, "tr_") {
		return nil, fmt.Errorf("invalid api key")
	}
	user, err := s.users.GetByAPIKey(r.Context(), apiKey)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}
	if time.Now().After(user.ExpiresAt) {
		return user, fmt.Errorf("subscription expired")
	}
	return user, nil
}
