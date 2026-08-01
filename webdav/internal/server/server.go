package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
)

const davAllow = "OPTIONS, GET, HEAD, PROPFIND, PROPPATCH, LOCK, UNLOCK"

type urlSigner interface {
	SignURL(path string, expiry time.Duration) string
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
	user, err := s.authenticate(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Torrin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	tree := buildTree(s.completed(r.Context(), user.ID))
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		s.get(w, r, user.ID, tree)
		return
	}
	(&webdav.Handler{FileSystem: davFS{root: tree}, LockSystem: webdav.NewMemLS()}).ServeHTTP(w, r)
}

func (s *Server) completed(ctx context.Context, userID string) []*jobs.Job {
	list, _ := s.jobs.ListByUser(ctx, userID, 5000)
	return list
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
		return nil, fmt.Errorf("subscription expired")
	}
	return user, nil
}
