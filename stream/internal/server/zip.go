package server

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

const zipPrefetchWorkers = 8

const maxZipFiles = 200

func (s *Server) serveZip(w http.ResponseWriter, r *http.Request, infoHash string) {
	user := r.URL.Query().Get("u")
	rangeHdr := r.Header.Get("Range")
	isHead := r.Method == http.MethodHead
	node := s.nodeFor(r.Context(), infoHash)

	data, err := s.store.GetBytesNode(r.Context(), node, manifest.Path(infoHash))
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
		if f.Crc32 == 0 || f.Enc {
			seekable = false
		}
	}
	if !seekable {
		s.streamZip(w, r, infoHash, node, m, user, isHead)
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
		key := manifest.ResolveKey(infoHash, idx, m.Files[idx].DirectURL, m.Files[idx].FileName)
		obj, err := s.store.GetNode(r.Context(), node, key, fmt.Sprintf("bytes=%d-%d", off, off+length-1))
		if err != nil {
			return nil, err
		}
		return obj.Body, nil
	}
	layout.writeRange(w, start, end, fetch)
}

func (s *Server) streamZip(w http.ResponseWriter, r *http.Request, infoHash, node string, m *manifest.Manifest, user string, isHead bool) {
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
	s.prewarmZip(r.Context(), infoHash, node, m)

	zw := zip.NewWriter(w)
	ok := true
	for i, f := range m.Files {
		obj, err := s.zipFetch(r.Context(), infoHash, node, i, f)
		if err != nil {
			slog.Warn("zip: file fetch failed", "hash", infoHash, "file", f.FileName, "err", err)
			ok = false
			break
		}
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: f.FileName, Method: zip.Store})
		if err != nil {
			obj.Body.Close()
			ok = false
			break
		}
		var copyErr error
		if f.Enc && s.cipher != nil {
			copyErr = s.cipher.DecryptAll(entry, obj.Body)
		} else {
			_, copyErr = streamCopy(entry, obj.Body)
		}
		obj.Body.Close()
		if copyErr != nil {
			slog.Warn("zip: stream failed", "hash", infoHash, "file", f.FileName, "err", copyErr)
			ok = false
			break
		}
	}
	if ok {
		zw.Close()
	}
}

func (s *Server) zipFetch(ctx context.Context, infoHash, node string, i int, f manifest.File) (*storage.Object, error) {
	key := manifest.ResolveKey(infoHash, i, f.DirectURL, f.FileName)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		obj, err := s.store.GetNode(ctx, node, key, "")
		if err == nil {
			return obj, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *Server) prewarmZip(ctx context.Context, infoHash, node string, m *manifest.Manifest) {
	var next atomic.Int64
	for w := 0; w < zipPrefetchWorkers; w++ {
		go func() {
			for {
				i := int(next.Add(1)) - 1
				if i >= len(m.Files) || ctx.Err() != nil {
					return
				}
				key := manifest.ResolveKey(infoHash, i, m.Files[i].DirectURL, m.Files[i].FileName)
				obj, err := s.store.GetNode(ctx, node, key, "")
				if err != nil {
					continue
				}
				io.Copy(io.Discard, obj.Body)
				obj.Body.Close()
			}
		}()
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
