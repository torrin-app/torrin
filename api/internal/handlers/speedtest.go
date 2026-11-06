package handlers

import (
	"net/http"
	"net/url"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) speedtestTicket(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUser(r).ID
	signed := s.Store.SignURLNodeUser("", "speedtest", userID, 10*time.Minute)
	u, err := url.Parse(signed)
	if err != nil {
		web.WriteError(w, 500, "could not issue speed test ticket")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"ticket": u.RawQuery})
}
