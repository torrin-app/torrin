package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

const maxUsenetConns = 30

const (
	usenetFailCooldown    = 30 * time.Minute
	usenetDeleteTombstone = 24 * time.Hour
)

func (s *Server) registerUsenetRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("POST /api/jobs/nzb", authMW(http.HandlerFunc(s.submitNZB)))
	mux.Handle("POST /api/usenet/credentials", authMW(http.HandlerFunc(s.setUsenetCreds)))
	mux.Handle("GET /api/usenet/credentials", authMW(http.HandlerFunc(s.getUsenetCreds)))
	mux.Handle("DELETE /api/usenet/credentials", authMW(http.HandlerFunc(s.delUsenetCreds)))
	mux.Handle("POST /api/usenet/test", authMW(http.HandlerFunc(s.testUsenetCreds)))
}

func (s *Server) submitNZB(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 20*1024*1024)
	body, err := readNZBBody(r)
	if err != nil {
		web.WriteError(w, 400, "could not read nzb")
		return
	}
	name := strings.TrimSuffix(r.URL.Query().Get("name"), ".nzb")
	s.ingestNZB(w, r, middleware.GetUser(r), middleware.GetPlan(r), body, name, "", true)
}

func readNZBBody(r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		file, _, err := r.FormFile("nzb")
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(file)
	}
	return io.ReadAll(r.Body)
}

func usenetEntitled(hasOwnCreds bool, plan plans.Plan) bool {
	return (hasOwnCreds && plans.CanBYOK(plan.ID)) || plan.SystemUsenet
}

func userJobForHash(sibs []*jobs.Job, userID string) *jobs.Job {
	for _, j := range sibs {
		if j.UserID == userID && j.Status != jobs.StatusFailed {
			return j
		}
	}
	return nil
}

func tombstoneBlocks(explicit, tombstoned bool) bool {
	return !explicit && tombstoned
}

func recentlyFailed(sibs []*jobs.Job, userID string, within time.Duration) *jobs.Job {
	cutoff := time.Now().Add(-within)
	for _, j := range sibs {
		if j.UserID == userID && j.Status == jobs.StatusFailed && j.UpdatedAt.After(cutoff) {
			return j
		}
	}
	return nil
}

func (s *Server) ingestNZB(w http.ResponseWriter, r *http.Request, user *auth.User, plan plans.Plan, body []byte, nameHint, imdb string, explicit bool) {
	if _, err := s.Users.GetUsenetCreds(r.Context(), user.ID); !usenetEntitled(err == nil, plan) {
		web.WriteError(w, 403, "usenet needs your own provider, add NNTP credentials in settings, or upgrade to a plan that includes usenet")
		return
	}

	parsed, err := nzb.ParseBytes(body)
	if err != nil || len(parsed.Files) == 0 {
		web.WriteError(w, 400, "invalid nzb file")
		return
	}

	screenTexts := []string{parsed.Name()}
	for _, f := range parsed.Files {
		screenTexts = append(screenTexts, f.Subject, f.Filename)
	}
	if s.blockedBySafety(w, r, user.ID, screenTexts...) {
		return
	}

	hash := nzb.Hash(parsed)
	defer lockGrab(hash)()

	sibs, _ := s.Jobs.ListByInfoHash(r.Context(), hash)
	if j := userJobForHash(sibs, user.ID); j != nil {
		if !j.Status.Active() {
			j.StreamURLs = s.signStreams(j, r)
		}
		web.WriteJSON(w, 200, j)
		return
	}

	if s.Users != nil {
		tombstoned := s.Users.UsenetTombstoned(r.Context(), user.ID, hash)
		if tombstoneBlocks(explicit, tombstoned) {
			web.WriteError(w, 409, "you removed this download, search for it again to re-add")
			return
		}
		if tombstoned {
			s.Users.ClearUsenetTombstone(r.Context(), user.ID, hash)
		}
	}
	if j := recentlyFailed(sibs, user.ID, usenetFailCooldown); j != nil {
		msg := j.Error
		if msg == "" {
			msg = "this release failed recently, try again later"
		}
		web.WriteError(w, 409, msg)
		return
	}

	name := nameHint
	if name == "" {
		name = parsed.Name()
	}
	if name == "" {
		name = "usenet download"
	}

	size := parsed.TotalSize()
	if plan.MaxTorrentBytes > 0 && size > plan.MaxTorrentBytes {
		web.WriteError(w, 400, fmt.Sprintf("nzb too large (%dGB, max %dGB)", size/1e9, plan.MaxTorrentBytes/1e9))
		return
	}

	if manifest.Playable(r.Context(), s.Store, hash) {
		job, err := s.buildCachedJob(r.Context(), hash, "", user.ID, jobs.SourceUsenet)
		if err != nil {
			web.WriteError(w, 500, "could not read from cache")
			return
		}
		job.Name = name
		if imdb != "" {
			s.JobsPG.SetIMDB(r.Context(), hash, imdb)
			job.IMDBID = imdb
		}
		job.StreamURLs = s.signStreams(job, r)
		web.WriteJSON(w, 200, job)
		return
	}

	if existing, err := s.Jobs.GetByInfoHash(r.Context(), hash); err == nil && existing != nil && existing.Status != jobs.StatusFailed {
		if existing.UserID == user.ID {
			if !existing.Status.Active() {
				existing.StreamURLs = s.signStreams(existing, r)
			}
			web.WriteJSON(w, 200, existing)
			return
		}
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: hash, Name: existing.Name, Source: jobs.SourceUsenet,
			Status: existing.Status, Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node,
			IMDBID: existing.IMDBID,
		}
		activeLink := linked.Status.Active()
		if activeLink && !s.Slots.Acquire(r.Context(), user.ID, plan) {
			web.WriteError(w, 429, slotMsg(s, r, user.ID, plan.MaxConcurrent))
			return
		}
		if err := s.Jobs.Create(r.Context(), linked); err != nil {
			if activeLink {
				s.Slots.Release(user.ID)
			}
			web.WriteError(w, 500, "could not start this download")
			return
		}
		if activeLink {
			s.Slots.Release(user.ID)
		}
		if !linked.Status.Active() {
			linked.StreamURLs = s.signStreams(linked, r)
		}
		web.WriteJSON(w, 200, linked)
		return
	}

	if over, _ := s.Users.MonthlyQuotaExceeded(r.Context(), user.ID, plan.MonthlyIngestBytes); over {
		web.WriteError(w, 429, "monthly download limit reached, resets on the 1st")
		return
	}
	if !s.Slots.Acquire(r.Context(), user.ID, plan) {
		web.WriteError(w, 429, slotMsg(s, r, user.ID, plan.MaxConcurrent))
		return
	}
	if err := s.Store.Put(r.Context(), nzb.StorageKey(hash), bytes.NewReader(body), "application/x-nzb"); err != nil {
		s.Slots.Release(user.ID)
		web.WriteError(w, 500, "failed to store nzb")
		return
	}
	s.Users.SetJobNZB(r.Context(), hash, body)

	status := jobs.StatusPending
	if used, _ := s.Jobs.BudgetUsed(r.Context()); s.Budget-used < 1_000_000_000 {
		status = jobs.StatusQueued
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: hash, Name: name, FileSize: size,
		Source: jobs.SourceUsenet, Status: status, IMDBID: imdb,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	err = s.Jobs.Create(r.Context(), job)
	s.Slots.Release(user.ID)
	if err != nil {
		web.WriteError(w, 500, "could not start this download")
		return
	}
	if status == jobs.StatusPending {
		s.assign(job)
	}
	web.WriteJSON(w, 202, job)
}

func (s *Server) setUsenetCreds(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !plans.CanBYOK(middleware.GetPlan(r).ID) {
		web.WriteError(w, 403, "bring-your-own usenet requires a paid plan")
		return
	}
	var c auth.UsenetCreds
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil || c.Host == "" {
		web.WriteError(w, 400, "host required")
		return
	}
	if err := normalizeAndTest(r.Context(), &c); err != nil {
		web.WriteError(w, 400, "could not connect to usenet provider, check host/port/login")
		return
	}
	if err := s.Users.SaveUsenetCreds(r.Context(), user.ID, &c); err != nil {
		web.WriteError(w, 500, "failed to save credentials")
		return
	}
	s.Users.AuditLog(r.Context(), user.ID, "usenet_creds_added", c.Host, clientIP(r))
	web.WriteJSON(w, 200, map[string]any{"configured": true})
}

func (s *Server) testUsenetCreds(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	c, err := s.Users.GetUsenetCreds(r.Context(), user.ID)
	if err != nil || c == nil || c.Host == "" {
		web.WriteError(w, 400, "no saved credentials to test")
		return
	}
	if err := normalizeAndTest(r.Context(), c); err != nil {
		web.WriteError(w, 400, "could not connect to usenet provider, check host/port/login")
		return
	}
	web.WriteJSON(w, 200, map[string]any{"ok": true})
}

func normalizeAndTest(ctx context.Context, c *auth.UsenetCreds) error {
	if c.Port == 0 {
		c.Port = 119
		if c.SSL {
			c.Port = 563
		}
	}
	if c.MaxConns == 0 {
		c.MaxConns = 20
	}
	if c.MaxConns > maxUsenetConns {
		c.MaxConns = maxUsenetConns
	}
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return download.TestCredentials(tctx, download.Credentials{
		Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, SSL: c.SSL, MaxConns: c.MaxConns,
	})
}

func (s *Server) getUsenetCreds(w http.ResponseWriter, r *http.Request) {
	c, err := s.Users.GetUsenetCreds(r.Context(), middleware.GetUser(r).ID)
	if err != nil || c == nil {
		web.WriteJSON(w, 200, map[string]any{"configured": false})
		return
	}
	web.WriteJSON(w, 200, map[string]any{
		"configured": true,
		"host":       c.Host, "port": c.Port, "username": c.Username,
		"ssl": c.SSL, "max_conns": c.MaxConns,
	})
}

func (s *Server) delUsenetCreds(w http.ResponseWriter, r *http.Request) {
	s.Users.DeleteUsenetCreds(r.Context(), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}
