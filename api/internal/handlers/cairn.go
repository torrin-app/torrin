package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type cairnListItem struct {
	auth.CairnItem
	Cached       bool          `json:"cached"`
	StreamSource string        `json:"stream_source,omitempty"`
	StreamURLs   []jobs.Stream `json:"stream_urls,omitempty"`
}

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

	_, _, archived := s.Cairns.GetCairnArchive(r.Context(), hash)
	node := ""
	if !archived {
		job, err := s.Jobs.GetByInfoHash(r.Context(), hash)
		if err != nil || job == nil || job.Status != jobs.StatusComplete {
			web.WriteError(w, 409, "file must finish downloading before it can be cairned")
			return
		}
		node = job.Node
	}
	if err := s.Cairns.AddUserCairn(r.Context(), user.ID, hash); err != nil {
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
	user := middleware.GetUser(r)
	items, err := s.Cairns.ListUserCairns(r.Context(), user.ID)
	if err != nil {
		web.WriteError(w, 500, "failed to list cairns")
		return
	}
	out := make([]cairnListItem, len(items))
	for i, item := range items {
		out[i].CairnItem = item
		if streams, ok := s.cachedCairnStreams(r, user.ID, item.InfoHash); ok {
			out[i].Cached, out[i].StreamSource, out[i].StreamURLs = true, "cache", streams
			continue
		}
		if !item.Archived || !s.CairnDirect {
			continue
		}
		streams, err := s.directCairnStreams(r, user.ID, item.InfoHash)
		if err != nil {
			slog.Warn("cairn: direct streams unavailable", "hash", item.InfoHash, "err", err)
			continue
		}
		out[i].Cached, out[i].StreamSource, out[i].StreamURLs = true, "cairn", streams
	}
	web.WriteJSON(w, 200, map[string]any{"cairns": out})
}

func (s *Server) cachedCairnStreams(r *http.Request, userID, hash string) ([]jobs.Stream, bool) {
	if !manifest.Playable(r.Context(), s.Store, hash) {
		return nil, false
	}
	data, err := s.Store.GetBytes(r.Context(), manifest.Path(hash))
	if err != nil {
		return nil, false
	}
	man, err := manifest.Parse(data)
	if err != nil || len(man.Files) == 0 {
		return nil, false
	}
	files := make([]jobs.File, len(man.Files))
	for i, f := range man.Files {
		files[i] = jobs.File{Index: i, Name: f.FileName, Size: f.FileSize, Key: f.DirectURL, Enc: f.Enc}
	}
	job := &jobs.Job{UserID: userID, InfoHash: hash, Node: s.Jobs.NodeForInfoHash(r.Context(), hash), Files: files}
	return s.signStreams(job, r), true
}

func (s *Server) directCairnStreams(r *http.Request, userID, hash string) ([]jobs.Stream, error) {
	data, err := s.CairnStore.GetBytes(r.Context(), nzb.StorageKey(hash))
	if err != nil {
		if fallback, ok := s.Cairns.GetCairnNZB(r.Context(), hash); ok {
			data, err = fallback, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read nzb: %w", err)
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	if len(parsed.Files) == 0 {
		return nil, fmt.Errorf("parse nzb: no files")
	}
	streams := make([]jobs.Stream, len(parsed.Files))
	for i, file := range parsed.Files {
		name := file.Filename
		if name == "" {
			name = file.Subject
		}
		size := file.Size()
		enc := s.CairnCipher != nil
		if enc {
			size, err = s.CairnCipher.PlainSize(size)
			if err != nil {
				return nil, fmt.Errorf("file %d encrypted size: %w", i, err)
			}
		}
		u := s.Store.SignURLNodeUser("", cairn.StreamPath(hash, i, name), userID, 24*time.Hour) + manifest.StreamQuery(hash, enc)
		streams[i] = jobs.Stream{FileName: name, Size: size, SignedURL: georoute.URL(r, u)}
	}
	return streams, nil
}

func (s *Server) cairnRestore(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	_, name, ok := s.Cairns.GetCairnArchive(r.Context(), hash)
	if !ok {
		web.WriteError(w, 404, "no cairn archive for this item")
		return
	}
	if manifest.Playable(r.Context(), s.Store, hash) {
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
	if !s.Cairns.HasUserCairn(r.Context(), user.ID, hash) {
		web.WriteError(w, 403, "you can only restore your own cairn archives")
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
	if err := s.Cairns.DeleteUserCairn(r.Context(), middleware.GetUser(r).ID, hash); err != nil {
		web.WriteError(w, 500, "failed to remove cairn")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "removed"})
}
