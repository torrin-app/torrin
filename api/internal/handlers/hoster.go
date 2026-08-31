package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/urlnorm"
)

func (s *Server) submitHoster(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		web.WriteError(w, 400, "url required")
		return
	}
	if !isHosterURL(req.URL) {
		web.WriteError(w, 400, "invalid url")
		return
	}
	if s.blockedBySafety(w, r, user.ID, req.URL) {
		return
	}
	if plan.ID == "free" {
		web.WriteError(w, 403, "hoster links require a paid plan")
		return
	}

	s.submitMagnet(w, r, hosterInfoHash(req.URL), req.URL, "", jobs.SourceHoster, false)
}

func isHosterURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func hosterInfoHash(url string) string {
	h := sha256.Sum256([]byte(urlnorm.Canonical(url)))
	return fmt.Sprintf("%x", h[:20])
}
