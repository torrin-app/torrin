package handlers

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AdminKey == "" {
			web.WriteError(w, 403, "admin not configured")
			return
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(key), []byte(s.AdminKey)) != 1 {
			web.WriteError(w, 401, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	a := s.adminAuth
	mux.Handle("GET /api/admin/stats", a(http.HandlerFunc(s.adminStats)))
	mux.Handle("GET /api/admin/users", a(http.HandlerFunc(s.adminUsers)))
	mux.Handle("POST /api/admin/users/{id}/plan", a(http.HandlerFunc(s.adminSetPlan)))
	mux.Handle("POST /api/admin/users/{id}/ban", a(http.HandlerFunc(s.adminBanUser)))
	mux.Handle("POST /api/admin/users/{id}/unban", a(http.HandlerFunc(s.adminUnbanUser)))
	mux.Handle("GET /api/admin/users/{id}/wallet", a(http.HandlerFunc(s.adminWalletGet)))
	mux.Handle("POST /api/admin/users/{id}/wallet", a(http.HandlerFunc(s.adminWalletAdjust)))
	mux.Handle("GET /api/admin/audit/{id}", a(http.HandlerFunc(s.adminAudit)))
	mux.Handle("GET /api/admin/jobs", a(http.HandlerFunc(s.adminJobs)))
	mux.Handle("DELETE /api/admin/jobs/{id}", a(http.HandlerFunc(s.adminDeleteJob)))
	mux.Handle("GET /api/admin/cache", a(http.HandlerFunc(s.adminCache)))
	mux.Handle("DELETE /api/admin/cache/{hash}", a(http.HandlerFunc(s.adminEvictCache)))
	mux.Handle("POST /api/admin/relabel/{hash}", a(http.HandlerFunc(s.adminRelabel)))
	mux.Handle("GET /api/admin/blocklist", a(http.HandlerFunc(s.adminGetBlocklist)))
	mux.Handle("POST /api/admin/blocklist", a(http.HandlerFunc(s.adminAddBlocklist)))
	mux.Handle("DELETE /api/admin/blocklist/{term}", a(http.HandlerFunc(s.adminDelBlocklist)))
	mux.Handle("GET /api/admin/referrals", a(http.HandlerFunc(s.adminReferrals)))
	mux.Handle("GET /api/admin/partner/{code}", a(http.HandlerFunc(s.adminPartnerReport)))
	mux.Handle("POST /api/admin/partner/{code}/token", a(http.HandlerFunc(s.adminMintPartnerToken)))
	mux.Handle("POST /api/admin/send-keys", a(http.HandlerFunc(s.adminSendKeys)))
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
