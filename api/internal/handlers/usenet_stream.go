package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func (s *Server) setUsenetStreaming(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		web.WriteError(w, 400, "enabled required")
		return
	}
	plan := middleware.GetPlan(r)
	if req.Enabled && !plan.SystemUsenet && !plans.CanBYOK(plan.ID) {
		web.WriteError(w, 403, "usenet streaming requires a plan with usenet")
		return
	}
	if err := s.Users.SetUsenetStreaming(r.Context(), middleware.GetUser(r).ID, req.Enabled); err != nil {
		web.WriteError(w, 500, "could not save your changes")
		return
	}
	web.WriteJSON(w, 200, map[string]bool{"usenet_streaming": req.Enabled})
}

func (s *Server) streamUsenetResult(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	if !user.UsenetStreaming {
		web.WriteError(w, 403, "enable usenet streaming in settings first")
		return
	}
	if !usenetEntitled(s.hasUsenetProvider(r.Context(), user.ID), plan) {
		web.WriteError(w, 403, "usenet needs your own provider or a plan that includes usenet")
		return
	}
	sources := s.resolveSources(r.Context(), user.ID, plan)
	if len(sources) == 0 {
		web.WriteError(w, 400, "configure a usenet indexer first")
		return
	}
	var req struct {
		ID       string `json:"id"`
		Source   string `json:"source"`
		NZBURL   string `json:"nzb_url"`
		Category string `json:"category"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == "" {
		web.WriteError(w, 400, "id required")
		return
	}
	if req.Source == "system" {
		req.NZBURL = ""
	}
	client := pickSource(sources, req.Source, req.NZBURL)
	if client == nil {
		web.WriteError(w, 404, "indexer not found")
		return
	}
	body, err := client.DownloadNZB(&indexer.Result{ID: req.ID, NZBURL: req.NZBURL, Category: req.Category})
	if err != nil {
		web.WriteError(w, 502, "failed to download NZB")
		return
	}
	parsed, err := nzb.ParseBytes(body)
	if err != nil || len(parsed.Files) == 0 {
		web.WriteError(w, 400, "invalid nzb file")
		return
	}
	screen := []string{parsed.Name()}
	for _, f := range parsed.Files {
		screen = append(screen, f.Subject, f.Filename)
	}
	if s.blockedBySafetyHash(w, r, user.ID, nzb.Hash(parsed), screen...) {
		return
	}
	stream, ok := s.mintUsenetStreamLink(r.Context(), user.ID, body, parsed)
	if !ok {
		web.WriteError(w, 422, "this release can't be streamed, download it instead")
		return
	}
	web.WriteJSON(w, 200, stream)
}

func (s *Server) mintUsenetStreamLink(ctx context.Context, userID string, body []byte, parsed *nzb.NZB) (jobs.Stream, bool) {
	kind, direct := download.Classify(parsed)
	if kind == download.KindNone {
		return jobs.Stream{}, false
	}
	hash := nzb.Hash(parsed)
	if err := s.Store.Put(ctx, nzb.StorageKey(hash), bytes.NewReader(body), "application/x-nzb"); err != nil {
		return jobs.Stream{}, false
	}
	if s.Users != nil {
		s.Users.SetJobNZB(ctx, hash, body)
	}
	name, size := usenetStreamNameSize(parsed, kind, direct)
	link := s.Store.SignURLNodeUser("", nzb.StorageKey(hash), userID, 24*time.Hour) + "&usenet=1"
	return jobs.Stream{FileName: name, Size: size, SignedURL: link}, true
}

func (s *Server) usenetStreamOverlay(ctx context.Context, user *auth.User, body []byte, parsed *nzb.NZB, job *jobs.Job) {
	if !user.UsenetStreaming || len(job.StreamURLs) > 0 {
		return
	}
	kind, direct := download.Classify(parsed)
	if kind == download.KindNone {
		return
	}
	if err := s.Store.Put(ctx, nzb.StorageKey(job.InfoHash), bytes.NewReader(body), "application/x-nzb"); err != nil {
		return
	}
	link := s.Store.SignURLNodeUser("", nzb.StorageKey(job.InfoHash), user.ID, 24*time.Hour) + "&usenet=1"
	name, size := usenetStreamNameSize(parsed, kind, direct)
	job.StreamURLs = []jobs.Stream{{FileName: name, Size: size, SignedURL: link}}
}

func (s *Server) applyUsenetStreamLink(ctx context.Context, user *auth.User, job *jobs.Job) {
	if job.Source != jobs.SourceUsenet || user == nil || !user.UsenetStreaming || len(job.StreamURLs) > 0 {
		return
	}
	if job.Status == jobs.StatusComplete || job.Status == jobs.StatusSeeding {
		return
	}
	data, err := s.Store.GetBytes(ctx, nzb.StorageKey(job.InfoHash))
	if err != nil {
		return
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return
	}
	kind, direct := download.Classify(parsed)
	if kind == download.KindNone {
		return
	}
	link := s.Store.SignURLNodeUser("", nzb.StorageKey(job.InfoHash), user.ID, 24*time.Hour) + "&usenet=1"
	name, size := usenetStreamNameSize(parsed, kind, direct)
	job.StreamURLs = []jobs.Stream{{FileName: name, Size: size, SignedURL: link}}
}

func usenetStreamNameSize(parsed *nzb.NZB, kind download.Kind, direct nzb.File) (string, int64) {
	if kind == download.KindDirect {
		return filepath.Base(download.FileName(direct)), direct.Size()
	}
	name := parsed.Name()
	if name == "" {
		name = "video"
	}
	if !strings.Contains(name, ".") {
		name += ".mkv"
	}
	return name, parsed.TotalSize()
}
