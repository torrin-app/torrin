package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/providers"
)

var (
	hostersMu     sync.Mutex
	hostersCache  []providers.Host
	hostersExpiry time.Time
)

func (s *Server) hosters(w http.ResponseWriter, r *http.Request) {
	hostersMu.Lock()
	cache, exp := hostersCache, hostersExpiry
	hostersMu.Unlock()

	if cache != nil && time.Now().Before(exp) {
		web.WriteJSON(w, 200, cache)
		return
	}

	list, err := providers.FetchHosts(r.Context())
	if err != nil {
		if cache != nil {
			web.WriteJSON(w, 200, cache)
			return
		}
		web.WriteError(w, 502, "could not reach alldebrid")
		return
	}

	hostersMu.Lock()
	hostersCache, hostersExpiry = list, time.Now().Add(5*time.Minute)
	hostersMu.Unlock()
	web.WriteJSON(w, 200, list)
}
