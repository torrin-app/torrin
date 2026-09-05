package handlers

import (
	"context"
	"encoding/json"
	"errors"
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
		if src := releaseSourceFor(input); src != "" {
			if middleware.GetPlan(r).ID == "free" {
				web.WriteError(w, 403, "grabbing from release sites requires a paid plan")
				return
			}
			s.submitMagnet(w, r, hosterInfoHash(input), input, releaseTitleFromURL(r.Context(), input), src, false)
			return
		}
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

func (s *Server) submitMagnet(w http.ResponseWriter, r *http.Request, infoHash, magnet, name string, source jobs.Source, budgetGate bool) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	defer lockGrab(infoHash)()

	// 1. Reuse this account's live row before consulting shared cache state.
	siblings, _ := s.Jobs.ListByInfoHash(r.Context(), infoHash)
	if existing := userJobForHash(siblings, user.ID); existing != nil {
		if !existing.Status.Active() {
			existing.StreamURLs = s.signStreams(existing, r)
		}
		web.WriteJSON(w, 200, existing)
		return
	}

	// 2. Already cached → instant.
	if manifest.Playable(r.Context(), s.Store, infoHash) {
		job, err := s.buildCachedJob(r.Context(), infoHash, magnet, user.ID, source)
		if err != nil {
			web.WriteError(w, 500, "could not read from cache")
			return
		}
		job.StreamURLs = s.signStreams(job, r)
		web.WriteJSON(w, 200, job)
		return
	}

	// 3. Link another user's reusable physical cache to this account.
	if existing := reusableJobForHash(siblings); existing != nil {
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: infoHash, Name: existing.Name, Magnet: magnet,
			Source: source, Status: existing.Status, Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node,
		}
		activeLink := linked.Status.Active()
		responseStatus := http.StatusOK
		if activeLink {
			disposition, err := s.Slots.Admit(r.Context(), linked, plan, budgetGate)
			if err != nil {
				writeQueueError(w, err, s.Slots.MaxQueued())
				return
			}
			if disposition == jobs.AdmissionAdmitted {
				s.assign(linked)
			}
			responseStatus = admissionStatus(disposition)
		} else if err := s.Jobs.Create(r.Context(), linked); err != nil {
			web.WriteError(w, 500, "could not start this download")
			return
		}
		if !linked.Status.Active() {
			linked.StreamURLs = s.signStreams(linked, r)
		}
		web.WriteJSON(w, responseStatus, linked)
		return
	}

	// 2b. Re-hydrate from a Cairn usenet archive instead of re-leeching the source.
	if s.Users != nil {
		if _, _, ok := s.Users.GetCairnArchive(r.Context(), infoHash); ok {
			job := &jobs.Job{
				UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: name,
				Source: jobs.SourceUsenet, Status: jobs.StatusPending,
				MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
			}
			disposition, err := s.Slots.Admit(r.Context(), job, plan, budgetGate)
			if err != nil {
				writeQueueError(w, err, s.Slots.MaxQueued())
				return
			}
			if disposition == jobs.AdmissionAdmitted {
				s.assign(job)
			}
			web.WriteJSON(w, admissionStatus(disposition), job)
			return
		}
	}

	// 2c. In the user's storage but evicted from cache → serve from BYOS, re-warm the cache in the background.
	if s.serveFromBYOS(w, r, infoHash, magnet, source) {
		return
	}

	// 3. New download, monthly quota + durable queue admission.
	if s.Users != nil {
		if over, _ := s.Users.MonthlyQuotaExceeded(r.Context(), user.ID, plan.MonthlyIngestBytes); over {
			web.WriteError(w, 429, "monthly download limit reached, resets on the 1st")
			return
		}
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: name,
		Source: source, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	disposition, err := s.Slots.Admit(r.Context(), job, plan, budgetGate)
	if err != nil {
		writeQueueError(w, err, s.Slots.MaxQueued())
		return
	}
	if disposition == jobs.AdmissionAdmitted {
		s.assign(job)
	}
	web.WriteJSON(w, admissionStatus(disposition), job)
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
	disposition, updated, err := s.Slots.Readmit(r.Context(), job.ID, plan)
	if err != nil {
		writeQueueError(w, err, s.Slots.MaxQueued())
		return
	}
	if disposition == jobs.AdmissionAdmitted {
		s.assign(updated)
	}
	web.WriteJSON(w, admissionStatus(disposition), updated)
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
	if job.InputKey != "" {
		s.Store.Delete(r.Context(), job.InputKey)
	}
	if job.Status.Active() && job.Status != jobs.StatusQueued {
		s.Bus.Publish(events.JobDeleted, events.Deleted{JobID: job.ID, InfoHash: job.InfoHash, Source: string(job.Source), Node: job.Node, UserID: job.UserID})
	}
	s.Slots.Wake()
	if job.Source == jobs.SourceUsenet && s.Users != nil {
		s.Users.TombstoneUsenet(r.Context(), user.ID, job.InfoHash, time.Now().Add(usenetDeleteTombstone))
		if sibs, _ := s.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(sibs) == 0 {
			s.Users.ClearJobNZB(r.Context(), job.InfoHash)
		}
	}
	// Normal in-flight torrents are cancelled by the ingest subscriber so it
	// can meter partial bytes before removing qBittorrent state. Seed downloads
	// may use a separate engine and still need direct cleanup here.
	if job.Seed && job.Status.Active() && job.Status != jobs.StatusQueued && job.Source == jobs.SourceTorrent && s.QbitSeed != nil {
		if sibs, _ := s.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(sibs) == 0 {
			if s.QbitSeed.Login() == nil {
				s.QbitSeed.Delete(job.InfoHash)
			}
		}
	}
	web.WriteJSON(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) assign(job *jobs.Job) {
	cluster.Assign(context.Background(), s.Bus, s.JobsPG, s.Jobs, job)
}

func writeQueueError(w http.ResponseWriter, err error, max int) {
	if errors.Is(err, jobs.ErrQueueFull) {
		web.WriteError(w, 429, fmt.Sprintf("download queue full (%d/%d)", max, max))
		return
	}
	web.WriteError(w, 500, "could not queue this download")
}
