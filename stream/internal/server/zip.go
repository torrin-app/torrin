package server

import (
	"archive/zip"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/shared/manifest"
)

const maxZipFiles = 200

func (s *Server) serveZip(w http.ResponseWriter, r *http.Request, infoHash string) {
	user := r.URL.Query().Get("u")
	if r.Method != http.MethodHead {
		if !s.zips.acquire(user) {
			w.Header().Set("Retry-After", "30")
			httpError(w, http.StatusTooManyRequests, "too many concurrent zip downloads")
			return
		}
		defer s.zips.release(user)
	}

	data, err := s.store.GetBytes(r.Context(), manifest.Path(infoHash))
	if err != nil {
		httpError(w, 404, "not found")
		return
	}
	name, _, files := manifest.Meta(data)
	if len(files) == 0 {
		httpError(w, 404, "no files")
		return
	}
	if len(files) > maxZipFiles {
		httpError(w, 413, "too many files")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="`+zipName(name)+`"`)
	h.Set("Accept-Ranges", "none")
	h.Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	s.recordView(r, infoHash+"/")

	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, f := range files {
		obj, err := s.store.Get(r.Context(), manifest.Key(infoHash, f.Index, f.Name), "")
		if err != nil {
			return
		}
		entry, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Store})
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

func zipName(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	if name == "" {
		name = "download"
	}
	return name + ".zip"
}
