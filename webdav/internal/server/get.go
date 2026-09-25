package server

import (
	"io"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/manifest"
)

var streamProxyClient = &http.Client{Transport: http.DefaultTransport}

func (s *Server) get(w http.ResponseWriter, r *http.Request, userID string, tree *node) {
	n := tree.find(segments(r.URL.Path))
	if n == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if n.dir {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodHead {
			return
		}
		renderHTML(w, r.URL.Path, n)
		return
	}
	var url string
	if n.cairn {
		url = s.store.SignURLNodeUser("", n.key, userID, 4*time.Hour) + manifest.StreamQuery(n.hash, n.enc)
	} else {
		s.jobs.RecordView(r.Context(), n.hash, userID)
		url = s.store.SignURLNode(n.node, n.key, 4*time.Hour) + manifest.StreamQuery(n.hash, n.enc)
	}
	proxyStream(w, r, url)
}

func proxyStream(w http.ResponseWriter, r *http.Request, url string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, nil)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	if rng := r.Header.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := streamProxyClient.Do(req)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	h := w.Header()
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}
