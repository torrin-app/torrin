package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

const libraryPageSize = 20

func (s *Server) registerLibraryRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("GET /api/rd/library", authMW(http.HandlerFunc(s.rdLibrary)))
	mux.Handle("GET /api/alldebrid/library", authMW(http.HandlerFunc(s.adLibrary)))
	mux.Handle("GET /api/torbox/library", authMW(http.HandlerFunc(s.tbLibrary)))
	mux.Handle("POST /api/rd/import", authMW(http.HandlerFunc(s.importHashes)))
	mux.Handle("POST /api/alldebrid/import", authMW(http.HandlerFunc(s.importHashes)))
	mux.Handle("POST /api/torbox/import", authMW(http.HandlerFunc(s.importHashes)))
}

type libraryItem struct {
	Hash     string `json:"hash"`
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
	Status   string `json:"status"`
	Cached   bool   `json:"cached"`
}

func (s *Server) rdLibrary(w http.ResponseWriter, r *http.Request) {
	key, _ := s.Users.GetRDKey(r.Context(), middleware.GetUser(r).ID)
	s.library(w, r, key, providers.RDLibrary)
}

func (s *Server) adLibrary(w http.ResponseWriter, r *http.Request) {
	key, _ := s.Users.GetADKey(r.Context(), middleware.GetUser(r).ID)
	s.library(w, r, key, providers.ADLibrary)
}

func (s *Server) tbLibrary(w http.ResponseWriter, r *http.Request) {
	key, _ := s.Users.GetTBKey(r.Context(), middleware.GetUser(r).ID)
	s.library(w, r, key, providers.TBLibrary)
}

func (s *Server) library(w http.ResponseWriter, r *http.Request, key string, list func(context.Context, string) ([]providers.LibraryItem, error)) {
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "debrid library access requires a paid plan")
		return
	}
	if key == "" {
		web.WriteError(w, 400, "no provider key configured")
		return
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	all, err := list(r.Context(), key)
	if err != nil {
		web.WriteError(w, 502, "failed to fetch library")
		return
	}
	start := min((page-1)*libraryPageSize, len(all))
	end := min(start+libraryPageSize, len(all))
	items := make([]libraryItem, 0, end-start)
	for _, it := range all[start:end] {
		hash := strings.ToLower(it.Hash)
		cached, _ := s.Store.Has(r.Context(), manifest.Path(hash))
		items = append(items, libraryItem{Hash: hash, Filename: it.Filename, Bytes: it.Size, Status: "ready", Cached: cached})
	}
	web.WriteJSON(w, 200, map[string]any{"page": page, "items": items, "count": len(all)})
}

func (s *Server) importHashes(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	if !plans.CanBYOK(plan.ID) {
		web.WriteError(w, 403, "library import requires a paid plan")
		return
	}
	var req struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteError(w, 400, "hashes required")
		return
	}
	created, errs := []string{}, []string{}
	for _, h := range req.Hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if !magnet.Valid(h) {
			errs = append(errs, h+": invalid hash")
			continue
		}
		mag := magnet.Build(h, "")
		if cached, _ := s.Store.Has(r.Context(), manifest.Path(h)); cached {
			if _, err := s.buildCachedJob(r.Context(), h, mag, user.ID, jobs.SourceTorrent); err != nil {
				errs = append(errs, h+": "+err.Error())
				continue
			}
			created = append(created, h)
			continue
		}
		if existing, _ := s.Jobs.GetByInfoHash(r.Context(), h); existing != nil && existing.UserID == user.ID {
			created = append(created, h)
			continue
		}
		job := &jobs.Job{
			UserID: user.ID, InfoHash: h, Magnet: mag, Source: jobs.SourceTorrent,
			Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
		}
		hasSlot := s.Slots.Acquire(r.Context(), user.ID, plan)
		if !hasSlot {
			job.Status = jobs.StatusQueued
		}
		if err := s.Jobs.Create(r.Context(), job); err != nil {
			if hasSlot {
				s.Slots.Release(user.ID)
			}
			errs = append(errs, h+": "+err.Error())
			continue
		}
		if hasSlot {
			s.Slots.Release(user.ID)
			s.assign(job)
		}
		created = append(created, h)
	}
	web.WriteJSON(w, 200, map[string]any{"created": created, "errors": errs})
}
