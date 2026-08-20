package server

import (
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/manifest"
)

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
	s.jobs.RecordView(r.Context(), n.hash, userID)
	url := s.store.SignURLNode(n.node, n.key, 4*time.Hour) + manifest.StreamQuery(n.hash, n.enc)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}
