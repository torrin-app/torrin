package server

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/shared/manifest"
)

const maxZipFiles = 200

func (s *Server) serveZip(w http.ResponseWriter, r *http.Request, infoHash string) {
	user := r.URL.Query().Get("u")
	rangeHdr := r.Header.Get("Range")
	isHead := r.Method == http.MethodHead

	data, err := s.store.GetBytes(r.Context(), manifest.Path(infoHash))
	if err != nil {
		httpError(w, 404, "not found")
		return
	}
	m, err := manifest.Parse(data)
	if err != nil || len(m.Files) == 0 {
		httpError(w, 404, "no files")
		return
	}
	if len(m.Files) > maxZipFiles {
		httpError(w, 413, "too many files")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="`+zipName(m.Name)+`"`)
	h.Set("Cache-Control", "no-store")

	entries := make([]zipEntry, len(m.Files))
	seekable := true
	for i, f := range m.Files {
		entries[i] = zipEntry{name: f.FileName, size: f.FileSize, crc: f.Crc32}
		if f.Crc32 == 0 {
			seekable = false
		}
	}
	if !seekable {
		s.streamZip(w, r, infoHash, m, user, isHead)
		return
	}

	layout := buildZipLayout(entries)
	h.Set("Accept-Ranges", "bytes")

	start, end := int64(0), layout.total-1
	status := http.StatusOK
	if rangeHdr != "" {
		s2, e2, ok := parseByteRange(rangeHdr, layout.total)
		if !ok {
			h.Set("Content-Range", fmt.Sprintf("bytes */%d", layout.total))
			httpError(w, http.StatusRequestedRangeNotSatisfiable, "bad range")
			return
		}
		start, end, status = s2, e2, http.StatusPartialContent
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, layout.total))
	}
	h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if isHead {
		w.WriteHeader(status)
		return
	}

	if rangeHdr == "" {
		if !s.zips.acquire(user) {
			h.Set("Retry-After", "30")
			httpError(w, http.StatusTooManyRequests, "too many concurrent zip downloads")
			return
		}
		defer s.zips.release(user)
	}

	s.recordView(r, infoHash+"/")
	w.WriteHeader(status)
	fetch := func(idx int, off, length int64) (io.ReadCloser, error) {
		key := manifest.Key(infoHash, idx, m.Files[idx].FileName)
		obj, err := s.store.Get(r.Context(), key, fmt.Sprintf("bytes=%d-%d", off, off+length-1))
		if err != nil {
			return nil, err
		}
		return obj.Body, nil
	}
	layout.writeRange(w, start, end, fetch)
}

func (s *Server) streamZip(w http.ResponseWriter, r *http.Request, infoHash string, m *manifest.Manifest, user string, isHead bool) {
	h := w.Header()
	h.Set("Accept-Ranges", "none")
	if isHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !s.zips.acquire(user) {
		h.Set("Retry-After", "30")
		httpError(w, http.StatusTooManyRequests, "too many concurrent zip downloads")
		return
	}
	defer s.zips.release(user)

	s.recordView(r, infoHash+"/")
	zw := zip.NewWriter(w)
	defer zw.Close()
	for i, f := range m.Files {
		obj, err := s.store.Get(r.Context(), manifest.Key(infoHash, i, f.FileName), "")
		if err != nil {
			return
		}
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: f.FileName, Method: zip.Store})
		if err != nil {
			obj.Body.Close()
			return
		}
		_, copyErr := streamCopy(entry, obj.Body)
		obj.Body.Close()
		if copyErr != nil {
			return
		}
	}
}

func parseByteRange(h string, size int64) (int64, int64, bool) {
	const p = "bytes="
	if !strings.HasPrefix(h, p) || size == 0 {
		return 0, 0, false
	}
	spec := h[len(p):]
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, false
	}
	startStr, endStr := spec[:dash], spec[dash+1:]
	if startStr == "" {
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || start < 0 || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if endStr != "" {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil || end < start {
			return 0, 0, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true
}

func zipName(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	if name == "" {
		name = "download"
	}
	return name + ".zip"
}
