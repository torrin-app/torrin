package main

import (
	"io"
	"net/http"
	"strconv"

	"github.com/torrin-app/torrin/byos/internal/mirror"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/video"
)

func (d *deps) srcMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /src/{token}/{jobid}/{index}", d.serveSource)
	mux.HandleFunc("HEAD /src/{token}/{jobid}/{index}", d.serveSource)
	return mux
}

func (d *deps) serveSource(w http.ResponseWriter, r *http.Request) {
	if d.srcToken == "" || r.PathValue("token") != d.srcToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		http.Error(w, "bad index", http.StatusBadRequest)
		return
	}
	job, err := d.repo.Get(r.Context(), r.PathValue("jobid"))
	if err != nil || job == nil || idx < 0 || idx >= len(job.Files) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f := job.Files[idx]
	w.Header().Set("Content-Type", video.ContentType(f.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(f.Size, 10))
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		return
	}
	body, err := mirror.OpenDecrypted(r.Context(), d.src, d.cipher, manifest.ResolveKey(job.InfoHash, idx, f.Key, f.Name), f.Enc)
	if err != nil {
		http.Error(w, "source read failed", http.StatusBadGateway)
		return
	}
	defer body.Close()
	io.Copy(w, body)
}
