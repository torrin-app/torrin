package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) serveFileEnc(w http.ResponseWriter, r *http.Request, key string) {
	head, err := s.store.Head(r.Context(), key)
	if err != nil {
		httpError(w, 404, "not found")
		return
	}
	plainTotal, err := s.cipher.PlainSize(head.Size)
	if err != nil {
		httpError(w, 500, "bad object")
		return
	}

	h := w.Header()
	if head.ContentType != "" {
		h.Set("Content-Type", head.ContentType)
	}
	h.Set("Accept-Ranges", "bytes")
	if r.URL.Query().Get("dl") == "1" {
		name := key[strings.LastIndex(key, "/")+1:]
		h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	s.recordView(r, key)

	start, end, isRange := parseRange(r.Header.Get("Range"), plainTotal)
	if !isRange {
		obj, err := s.store.Get(r.Context(), key, "")
		if err != nil {
			httpError(w, 404, "not found")
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
		httpError(w, 404, "not found")
		return
	}
	defer obj.Body.Close()

	h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, plainTotal))
	h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	s.cipher.DecryptRange(w, obj.Body, plan)
}

func parseRange(header string, total int64) (start, end int64, ok bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	lo, hi := spec[:dash], spec[dash+1:]
	if lo == "" {
		n, err := strconv.ParseInt(hi, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, true
	}
	start, err := strconv.ParseInt(lo, 10, 64)
	if err != nil || start >= total {
		return 0, 0, false
	}
	end = total - 1
	if hi != "" {
		if e, err := strconv.ParseInt(hi, 10, 64); err == nil && e < end {
			end = e
		}
	}
	if end < start {
		return 0, 0, false
	}
	return start, end, true
}
