package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
)

func (s *Server) setEvictPolicy(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "storage cleanup rules require a paid plan")
		return
	}
	var req struct {
		EvictAfterDays int     `json:"evict_after_days"`
		EvictMaxGB     float64 `json:"evict_max_gb"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "invalid request body")
		return
	}
	if req.EvictAfterDays < 0 || req.EvictMaxGB < 0 {
		web.WriteError(w, 400, "cleanup values cannot be negative")
		return
	}
	creds, err := s.Users.GetStorageCreds(r.Context(), user.ID)
	if err != nil || creds == nil {
		web.WriteError(w, 404, "no storage configured")
		return
	}
	creds.EvictAfterDays = req.EvictAfterDays
	creds.EvictMaxBytes = int64(req.EvictMaxGB * 1e9)
	if err := s.Users.SaveStorageCreds(r.Context(), user.ID, creds); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"evict_after_days": creds.EvictAfterDays, "evict_max_bytes": creds.EvictMaxBytes})
}

func (s *Server) evictOwnStorage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "managing your own storage requires a paid plan")
		return
	}
	var req struct {
		InfoHash string `json:"info_hash"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.InfoHash == "" {
		web.WriteError(w, 400, "info_hash required")
		return
	}
	creds, err := s.Users.GetStorageCreds(r.Context(), user.ID)
	if err != nil || creds == nil {
		web.WriteError(w, 404, "no storage configured")
		return
	}
	sibs, _ := s.JobsPG.ListByInfoHash(r.Context(), req.InfoHash)
	owned := false
	for _, j := range sibs {
		if j.UserID == user.ID {
			owned = true
			break
		}
	}
	if !owned {
		web.WriteError(w, 404, "not in your library")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := auth.PurgeRelease(ctx, s.RClone, user.ID, creds, req.InfoHash); err != nil {
		web.WriteError(w, 502, "could not remove that from your storage, please retry")
		return
	}
	for _, j := range sibs {
		if j.UserID == user.ID {
			s.JobsPG.DeleteBYOSObject(ctx, j.ID)
		}
	}
	s.Users.AuditLog(r.Context(), user.ID, "storage_content_evicted", req.InfoHash, clientIP(r))
	web.WriteJSON(w, 200, map[string]string{"status": "removed"})
}
