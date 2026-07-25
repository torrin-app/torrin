package server

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

var copyBufPool = sync.Pool{New: func() any { b := make([]byte, 256*1024); return &b }}

func streamCopy(dst io.Writer, src io.Reader) (int64, error) {
	buf := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(buf)
	return io.CopyBuffer(dst, src, *buf)
}

type Storage interface {
	Get(ctx context.Context, key, rng string) (*storage.Object, error)
	Head(ctx context.Context, key string) (*storage.Object, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Verify(path string, expires int64, userID, sig string) bool
}

type Server struct {
	store  Storage
	cors   string
	apiURL string
	view   *http.Client
	zips   *zipGuard
	byos   *byosBackend
}

func New(store Storage, corsOrigin, apiURL string) *Server {
	return &Server{store: store, cors: corsOrigin, apiURL: apiURL, view: &http.Client{Timeout: 5 * time.Second}, zips: newZipGuard(maxZipsPerUser)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/", s.serve)
	return mux
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpError(w, 405, "method not allowed")
		return
	}

	key, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/"))
	if err != nil || key == "" {
		httpError(w, 400, "missing path")
		return
	}

	q := r.URL.Query()
	expires, sig, user := q.Get("expires"), q.Get("sig"), q.Get("u")
	if expires == "" || sig == "" {
		httpError(w, 401, "missing signature parameters")
		return
	}
	expNum, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || time.Now().Unix() > expNum {
		httpError(w, 401, "link expired")
		return
	}
	if !s.store.Verify(key, expNum, user, sig) {
		httpError(w, 403, "invalid signature")
		return
	}

	if hash, ok := manifest.ZipHash(key); ok {
		s.serveZip(w, r, hash)
		return
	}
	if q.Get("byos") == "1" && s.serveBYOS(w, r, key, user) {
		return
	}
	if r.Method == http.MethodHead {
		s.serveHead(w, r, key)
		return
	}
	s.serveFile(w, r, key)
}

func (s *Server) serveHead(w http.ResponseWriter, r *http.Request, key string) {
	obj, err := s.store.Head(r.Context(), key)
	if err != nil {
		httpError(w, 404, "not found")
		return
	}
	h := w.Header()
	if obj.ContentType != "" {
		h.Set("Content-Type", obj.ContentType)
	}
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	h.Set("X-File-Size", strconv.FormatInt(obj.Size, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, key string) {
	rng := r.Header.Get("Range")
	obj, err := s.store.Get(r.Context(), key, rng)
	if err != nil {
		httpError(w, 404, "not found")
		return
	}
	defer obj.Body.Close()
	s.recordView(r, key)

	h := w.Header()
	if obj.ContentType != "" {
		h.Set("Content-Type", obj.ContentType)
	}
	h.Set("Accept-Ranges", "bytes")
	if r.URL.Query().Get("dl") == "1" {
		name := key[strings.LastIndex(key, "/")+1:]
		h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}

	status := http.StatusOK
	if rng != "" && obj.ContentRange != "" {
		h.Set("Content-Range", obj.ContentRange)
		status = http.StatusPartialContent
	}
	if obj.Size > 0 {
		h.Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	}
	w.WriteHeader(status)
	streamCopy(w, obj.Body)
}

func (s *Server) recordView(r *http.Request, key string) {
	if s.apiURL == "" {
		return
	}
	if rng := r.Header.Get("Range"); rng != "" && !strings.HasPrefix(rng, "bytes=0-") {
		return
	}
	id := key
	if i := strings.Index(key, "/"); i > 0 {
		id = key[:i]
	}
	if len(id) != 40 {
		return
	}
	go func() {
		req, err := http.NewRequest(http.MethodPost, s.apiURL+"/internal/view/"+id, nil)
		if err != nil {
			return
		}
		if resp, err := s.view.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}

func (s *Server) setCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", s.cors)
	h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Range")
	h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, X-File-Size")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
}

func httpError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}
