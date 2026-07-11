package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
)

var (
	sitesMu     sync.Mutex
	sitesCache  []byte
	sitesExpiry time.Time
)

func (s *Server) sites(w http.ResponseWriter, r *http.Request) {
	sitesMu.Lock()
	cache, exp := sitesCache, sitesExpiry
	sitesMu.Unlock()
	if cache != nil && time.Now().Before(exp) {
		writeRawJSON(w, cache)
		return
	}

	data, err := s.Store.GetBytes(r.Context(), "meta/extractors.json")
	if err != nil {
		if cache != nil {
			writeRawJSON(w, cache)
			return
		}
		web.WriteError(w, 503, "site list not ready")
		return
	}

	sitesMu.Lock()
	sitesCache, sitesExpiry = data, time.Now().Add(time.Hour)
	sitesMu.Unlock()
	writeRawJSON(w, data)
}

func writeRawJSON(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
