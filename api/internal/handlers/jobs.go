package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/safety"
)

func (s *Server) submitJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Magnet string `json:"magnet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Magnet == "" {
		web.WriteError(w, 400, "magnet required")
		return
	}
	s.ingestText(w, r, req.Magnet)
}

func (s *Server) ingestText(w http.ResponseWriter, r *http.Request, input string) {
	user := middleware.GetUser(r)
	if s.blockedBySafety(w, r, user.ID, input) {
		return
	}
	if isWebURL(input) {
		s.submitMagnet(w, r, urlKey(input), input, "", jobs.SourceYtdlp, true)
		return
	}
	infoHash := extractInfoHash(input)
	if infoHash == "" {
		web.WriteError(w, 400, "cannot extract infohash")
		return
	}
	s.submitMagnet(w, r, infoHash, input, "", jobs.SourceTorrent, true)
}

func (s *Server) blockedBySafety(w http.ResponseWriter, r *http.Request, userID string, texts ...string) bool {
	v := safety.Screen(texts...)
	if !v.Blocked {
		return false
	}
	if v.Ban {
		s.Users.BanUser(r.Context(), userID, v.Reason)
		s.Users.AuditLog(r.Context(), userID, "banned", v.Reason, clientIP(r))
	}
	web.WriteError(w, 403, "content blocked by safety policy")
	return true
}

func (s *Server) submitMagnet(w http.ResponseWriter, r *http.Request, infoHash, magnet, name string, source jobs.Source, budgetGate bool) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)

	// 1. Already cached → instant.
	if cached, _ := s.Store.Has(r.Context(), manifest.Path(infoHash)); cached {
		job, err := s.buildCachedJob(r.Context(), infoHash, magnet, user.ID, source)
		if err != nil {
			web.WriteError(w, 500, "could not read from cache")
			return
		}
		job.StreamURLs = s.signStreams(job, r)
		web.WriteJSON(w, 200, job)
		return
	}

	// 2. Dedup against an existing job for this hash.
	if existing, err := s.Jobs.GetByInfoHash(r.Context(), infoHash); err == nil && existing != nil && existing.Status != jobs.StatusFailed && existing.Status != jobs.StatusEvicted {
		if existing.UserID == user.ID {
			if !existing.Status.Active() {
				existing.StreamURLs = s.signStreams(existing, r)
			}
			web.WriteJSON(w, 200, existing)
			return
		}
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: infoHash, Name: existing.Name, Magnet: magnet,
			Source: source, Status: existing.Status, Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node,
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

	// 2b. Re-hydrate from a Cairn usenet archive instead of re-leeching the source.
	if s.Users != nil {
		if _, _, ok := s.Users.GetCairnArchive(r.Context(), infoHash); ok {
			if !s.Slots.Acquire(r.Context(), user.ID, plan) {
				web.WriteError(w, 429, slotMsg(s, r, user.ID, plan.MaxConcurrent))
				return
			}
			job := &jobs.Job{
				UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: name,
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
			return
		}
	}

	// 3. New download, slot limit.
	if !s.Slots.Acquire(r.Context(), user.ID, plan) {
		web.WriteError(w, 429, slotMsg(s, r, user.ID, plan.MaxConcurrent))
		return
	}
	status := jobs.StatusPending
	if budgetGate {
		if used, _ := s.Jobs.BudgetUsed(r.Context()); s.Budget-used < 1_000_000_000 {
			status = jobs.StatusQueued
		}
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: name,
		Source: source, Status: status, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	err := s.Jobs.Create(r.Context(), job)
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

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	if !job.Status.Active() || job.Status == jobs.StatusSeeding {
		job.StreamURLs = s.signStreams(job, r)
	}
	web.WriteJSON(w, 200, job)
}

func (s *Server) jobZip(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	if job.Status != jobs.StatusComplete && job.Status != jobs.StatusSeeding {
		web.WriteError(w, 409, "job not complete")
		return
	}
	http.Redirect(w, r, georoute.URL(r, s.Store.SignURLNodeUser(job.Node, manifest.ZipKey(job.InfoHash), user.ID, 24*time.Hour)), http.StatusTemporaryRedirect)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	q := r.URL.Query()
	limit := 50
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = n
	}
	if limit > 200 {
		limit = 200
	}
	var list []*jobs.Job
	if bs := q.Get("before"); bs != "" {
		before, err := time.Parse(time.RFC3339Nano, bs)
		if err != nil {
			web.WriteError(w, 400, "invalid before cursor")
			return
		}
		list, _ = s.Jobs.ListByUserBefore(r.Context(), user.ID, before, q.Get("before_id"), limit)
	} else {
		list, _ = s.Jobs.ListByUser(r.Context(), user.ID, limit)
	}
	for _, j := range list {
		if (!j.Status.Active() || j.Status == jobs.StatusSeeding) && len(j.Files) > 0 {
			j.StreamURLs = s.signStreams(j, r)
		}
	}
	web.WriteJSON(w, 200, list)
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	if job.Status != jobs.StatusFailed && job.Status != jobs.StatusEvicted {
		web.WriteError(w, 409, "only failed or evicted jobs can be retried")
		return
	}
	s.requeue(w, r, job, user.ID, plan)
}

func (s *Server) recheckJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}
	if job.Status != jobs.StatusComplete {
		web.WriteError(w, 409, "only completed downloads can be rechecked")
		return
	}
	if job.Source != jobs.SourceTorrent && job.Source != jobs.SourceUsenet {
		web.WriteError(w, 400, "recheck is only available for torrent and usenet downloads")
		return
	}
	s.requeue(w, r, job, user.ID, plan)
}

func (s *Server) requeue(w http.ResponseWriter, r *http.Request, job *jobs.Job, userID string, plan plans.Plan) {
	if !s.Slots.Acquire(r.Context(), userID, plan) {
		web.WriteError(w, 429, slotMsg(s, r, userID, plan.MaxConcurrent))
		return
	}
	job.Status = jobs.StatusPending
	job.Error = ""
	job.CreatedAt = time.Now().UTC()
	err := s.JobsPG.Requeue(r.Context(), job.ID)
	s.Slots.Release(userID)
	if err != nil {
		web.WriteError(w, 500, "failed to requeue job")
		return
	}
	s.assign(job)
	web.WriteJSON(w, 202, job)
}

func (s *Server) deleteJob(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job == nil || job.UserID != user.ID {
		web.WriteError(w, 404, "job not found")
		return
	}

	if job.Seed && job.Status == jobs.StatusSeeding {
		web.WriteError(w, 409, "seeding until it meets its ratio/time target, this frees automatically")
		return
	}

	if job.Status == jobs.StatusComplete {
		if sibs, _ := s.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(sibs) <= 1 {
			job.UserID = "system"
			if err := s.Jobs.Update(r.Context(), job); err != nil {
				web.WriteError(w, 500, "could not delete this download")
				return
			}
			web.WriteJSON(w, 200, map[string]string{"status": "deleted"})
			return
		}
	}

	s.Jobs.Delete(r.Context(), job.ID)
	if job.Status.Active() {
		s.Bus.Publish(events.JobDeleted, events.Deleted{JobID: job.ID, InfoHash: job.InfoHash, Source: string(job.Source), Node: job.Node})
	}
	if job.Source == jobs.SourceUsenet && s.Users != nil {
		s.Users.TombstoneUsenet(r.Context(), user.ID, job.InfoHash, time.Now().Add(usenetDeleteTombstone))
		if sibs, _ := s.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(sibs) == 0 {
			s.Users.ClearJobNZB(r.Context(), job.InfoHash)
		}
	}
	qb := s.Qbit
	if job.Seed {
		qb = s.QbitSeed
	}
	if job.Status.Active() && job.Source == jobs.SourceTorrent && qb != nil {
		if sibs, _ := s.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(sibs) == 0 {
			if qb.Login() == nil {
				qb.Delete(job.InfoHash)
			}
		}
	}
	web.WriteJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) assign(job *jobs.Job) {
	cluster.Assign(context.Background(), s.Bus, s.JobsPG, s.Jobs, job)
}

func slotMsg(s *Server, r *http.Request, userID string, max int) string {
	return fmt.Sprintf("slot limit reached (%d/%d)", s.Slots.ActiveSlots(r.Context(), userID), max)
}
