package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/torrin-app/torrin/shared/storage"
)

func (s *Server) serveFileEnc(w http.ResponseWriter, r *http.Request, key string) {
	head, err := s.store.Head(r.Context(), key)
	if err != nil {
		s.notFound(w, r, key, err)
		return
	}
	plainTotal, err := s.cipher.PlainSize(head.Size)
	if err != nil {
		slog.Warn("stream: bad encrypted object", "key", key, "size", head.Size, "err", err)
		httpError(w, 500, "bad object")
		return
	}

	h := w.Header()
	if head.ContentType != "" {
		h.Set("Content-Type", head.ContentType)
	}
	h.Set("Accept-Ranges", "bytes")
	s.setDownloadDisposition(w, r, key)
	s.recordView(r, key)

	start, end, isRange := storage.ParseRange(r.Header.Get("Range"), plainTotal)
	if !isRange {
		obj, err := s.store.Get(r.Context(), key, "")
		if err != nil {
			s.notFound(w, r, key, err)
			return
		}
		defer obj.Body.Close()
		h.Set("Content-Length", strconv.FormatInt(plainTotal, 10))
		w.WriteHeader(http.StatusOK)
		s.cipher.DecryptAll(w, obj.Body)
		return
	}

	plan, err := s.cipher.PlanRange(start, end+1, plainTotal)
	if err != nil {
		httpError(w, 416, "range not satisfiable")
		return
	}
	encRange := fmt.Sprintf("bytes=%d-%d", plan.EncStart, plan.EncEnd-1)
	obj, err := s.store.Get(r.Context(), key, encRange)
	if err != nil {
		s.notFound(w, r, key, err)
		return
	}
	defer obj.Body.Close()

	h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, plainTotal))
	h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	s.cipher.DecryptRange(w, obj.Body, plan)
}
