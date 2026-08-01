package server

import (
	"net/http"
	"time"
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
	http.Redirect(w, r, s.store.SignURL(n.key, 4*time.Hour), http.StatusTemporaryRedirect)
}
