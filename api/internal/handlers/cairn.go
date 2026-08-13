package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
)

func canCairn(plan plans.Plan, user *auth.User) bool {
	return plan.ID != "free" && user.Recurrence != "days"
}

func (s *Server) registerCairnRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/cairn", authMW(http.HandlerFunc(s.cairnCreate)))
	mux.Handle("GET /api/cairn", authMW(http.HandlerFunc(s.cairnList)))
	mux.Handle("POST /api/cairn/{hash}/restore", authMW(http.HandlerFunc(s.cairnRestore)))
	mux.Handle("DELETE /api/cairn/{hash}", authMW(http.HandlerFunc(s.cairnDelete)))
}

func (s *Server) cairnCreate(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !canCairn(middleware.GetPlan(r), user) {
		web.WriteError(w, 403, "cairn is available on monthly, yearly, and lifetime plans")
		return
	}
	var req struct {
		InfoHash string `json:"info_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid request body")
		return
	}
	hash := strings.ToLower(strings.TrimSpace(req.InfoHash))
	if len(hash) != 40 {
		web.WriteError(w, 400, "invalid info hash")
		return
	}

	_, _, archived := s.Users.GetCairnArchive(r.Context(), hash)
	node := ""
	if !archived {
		job, err := s.Jobs.GetByInfoHash(r.Context(), hash)
		if err != nil || job == nil || job.Status != jobs.StatusComplete {
			web.WriteError(w, 409, "file must finish downloading before it can be cairned")
			return
		}
		node = job.Node
	}
	if err := s.Users.AddUserCairn(r.Context(), user.ID, hash); err != nil {
		web.WriteError(w, 500, "failed to save cairn")
		return
	}
	if !archived {
		if err := s.Bus.Publish(events.CairnRequested, events.CairnRequest{InfoHash: hash, Node: node}); err != nil {
			web.WriteError(w, 500, "failed to queue cairn")
			return
		}
		web.WriteJSON(w, 202, map[string]string{"status": "queued"})
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "archived"})
}

func (s *Server) cairnList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Users.ListUserCairns(r.Context(), middleware.GetUser(r).ID)
	if err != nil {
		web.WriteError(w, 500, "failed to list cairns")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"cairns": items})
}

func (s *Server) cairnRestore(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	_, name, ok := s.Users.GetCairnArchive(r.Context(), hash)
	if !ok {
		web.WriteError(w, 404, "no cairn archive for this item")
		return
	}
	if cached, _ := s.Store.Has(r.Context(), manifest.Path(hash)); cached {
		job, err := s.buildCachedJob(r.Context(), hash, "", user.ID, jobs.SourceUsenet)
		if err != nil {
			web.WriteError(w, 500, "could not read from cache")
			return
		}
		job.StreamURLs = s.signStreams(job, r)
		web.WriteJSON(w, 200, job)
		return
	}
	if existing, err := s.Jobs.GetByInfoHash(r.Context(), hash); err == nil && existing != nil && existing.UserID == user.ID && existing.Status.Active() {
		web.WriteJSON(w, 200, existing)
		return
	}
	if !s.Slots.Acquire(r.Context(), user.ID, plan) {
		web.WriteError(w, 429, slotMsg(s, r, user.ID, plan.MaxConcurrent))
		return
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: hash, Name: name,
		Source: jobs.SourceUsenet, Status: jobs.StatusPending,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	err := s.Jobs.Create(r.Context(), job)
	s.Slots.Release(user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not start this download")
		return
	}
	s.assign(job)
	web.WriteJSON(w, 202, job)
}

func (s *Server) cairnDelete(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	if err := s.Users.DeleteUserCairn(r.Context(), middleware.GetUser(r).ID, hash); err != nil {
		web.WriteError(w, 500, "failed to remove cairn")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "removed"})
}
