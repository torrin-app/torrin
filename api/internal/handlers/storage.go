package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
)

func (s *Server) registerStorageRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/storage/connect", authMW(http.HandlerFunc(s.connectStorage)))
	mux.Handle("POST /api/storage", authMW(http.HandlerFunc(s.setStorage)))
	mux.Handle("GET /api/storage", authMW(http.HandlerFunc(s.getStorage)))
	mux.Handle("DELETE /api/storage", authMW(http.HandlerFunc(s.delStorage)))
}

func (s *Server) setStorage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	var c auth.StorageCreds
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		web.WriteError(w, 400, "invalid storage config")
		return
	}
	if c.Bucket == "" || (c.Endpoint == "" && c.Backend == "") {
		web.WriteError(w, 400, "bucket and endpoint required")
		return
	}
	c.Enabled = true
	if err := s.Users.SaveStorageCreds(r.Context(), user.ID, &c); err != nil {
		web.WriteError(w, 500, "failed to save storage")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_configured", c.Bucket, clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"configured": true})
}

func (s *Server) getStorage(w http.ResponseWriter, r *http.Request) {
	creds, err := s.Users.GetStorageCreds(r.Context(), middleware.GetUser(r).ID)
	if err != nil || creds == nil {
		web.WriteJSON(w, 200, map[string]any{"connected": false})
		return
	}
	out := map[string]any{
		"connected": true,
		"provider":  creds.Provider,
		"enabled":   creds.Enabled,
		"prefix":    creds.Prefix,
	}
	if creds.IsRclone() {
		out["kind"] = "rclone"
		out["backend"] = creds.Backend
	} else {
		out["kind"] = "s3"
		out["endpoint"] = creds.Endpoint
		out["region"] = creds.Region
		out["bucket"] = creds.Bucket
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) delStorage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if err := s.Users.DeleteStorageCreds(r.Context(), user.ID); err != nil {
		web.WriteError(w, 500, "could not remove that")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_creds_removed", "", clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
