package stremthru

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/keyed"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
)

func (h *Handler) listMagnets(w http.ResponseWriter, r *http.Request, user *auth.User) {
	userJobs, _ := jobs.ListAll(r.Context(), h.Jobs, user.ID)
	items := []map[string]any{}
	for _, j := range userJobs {
		items = append(items, h.magnetData(r.Context(), j))
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items, "total_items": len(items)}})
}

func (h *Handler) getMagnet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	job, err := h.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.UserID != user.ID {
		stError(w, 404, "not found")
		return
	}
	if job.Status == jobs.StatusComplete || job.Status == jobs.StatusSeeding {
		h.Jobs.RecordView(r.Context(), job.InfoHash, user.ID)
	}
	target, validTarget := playbackTarget(r.URL.Query().Get("sid"))
	if !validTarget {
		stError(w, 400, "invalid series sid")
		return
	}
	stJSON(w, 200, map[string]any{"data": h.magnetDataForTarget(r.Context(), job, target)})
}

func (h *Handler) addMagnet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var req struct {
		Magnet string `json:"magnet"`
		Link   string `json:"link"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		stError(w, 400, "magnet required")
		return
	}
	if req.Magnet == "" {
		req.Magnet = req.Link
	}
	if req.Magnet == "" {
		stError(w, 400, "magnet required")
		return
	}
	req.Magnet = html.UnescapeString(req.Magnet)
	infoHash := extractHash(req.Magnet)
	if infoHash == "" {
		stError(w, 400, "invalid magnet")
		return
	}
	defer keyed.Lock(infoHash)()
	target, validTarget := playbackTarget(r.URL.Query().Get("sid"))
	if !validTarget {
		stError(w, 400, "invalid series sid")
		return
	}

	source, mag := jobs.SourceTorrent, req.Magnet
	hdTitle := ""
	var hdSize int64
	if pURL, t, src, sz := h.Jobs.ReleaseLink(r.Context(), infoHash); pURL != "" {
		if !plans.CanBYOK(user.PlanID) {
			stError(w, 403, "this release requires a paid plan")
			return
		}
		if jobs.Source(src) != jobs.SourceUsenet || h.canUsenet(r.Context(), user) {
			source, mag, hdTitle, hdSize = jobs.Source(src), pURL, t, sz
		}
	}

	cache, cached := h.cachedJobFiles(r.Context(), infoHash)
	plan, _ := plans.Get(user.PlanID)

	// A job is the user's account row for the whole pack. The episode belongs
	// to this request and must never create another row.
	if owned, ownedErr := h.Jobs.GetByUserInfoHash(r.Context(), user.ID, infoHash); ownedErr == nil && owned != nil {
		if target.IMDBID != "" && owned.IMDBID == "" {
			h.Jobs.SetIMDB(r.Context(), infoHash, target.IMDBID)
			owned.IMDBID = target.IMDBID
		}
		stJSON(w, 200, map[string]any{"data": h.magnetDataForTarget(r.Context(), owned, target)})
		return
	}

	existing, err := h.Jobs.GetReusableByInfoHash(r.Context(), infoHash)
	if err == nil && existing != nil {
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: infoHash, Name: existing.Name, Magnet: mag,
			Source: source, Status: existing.Status, IMDBID: existing.IMDBID,
			Season: target.Season, Episode: target.Episode,
			Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node,
		}
		if target.IMDBID != "" {
			linked.IMDBID = target.IMDBID
		}
		activeLink := existing.Status.Active()
		if activeLink {
			disposition, err := h.Slots.Admit(r.Context(), linked, plan, false)
			if err != nil {
				stQueueError(w, err)
				return
			}
			if disposition == jobs.AdmissionAdmitted {
				h.assign(linked)
			}
		} else if err := h.Jobs.Create(r.Context(), linked); err != nil {
			stError(w, 500, "could not create download")
			return
		}
		stJSON(w, 200, map[string]any{"data": h.magnetDataForTarget(r.Context(), linked, target)})
		return
	}

	if !cached {
		if over, _ := h.Users.MonthlyQuotaExceeded(r.Context(), user.ID, plan.MonthlyIngestBytes); over {
			stError(w, 429, "monthly download limit reached, resets on the 1st")
			return
		}
	}
	if !cached && coldPullBlocked(r.Context(), h.Jobs, user.ID, plan.ColdPullsPerHour) {
		stError(w, 429, "hourly download limit reached, try later or upgrade")
		return
	}
	name := displayName(req.Magnet)
	if hdTitle != "" {
		name = hdTitle
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: mag, Name: name, FileSize: hdSize,
		Source: source, IMDBID: target.IMDBID, Season: target.Season, Episode: target.Episode,
		Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	if cached {
		job.Status = jobs.StatusComplete
		if cache.name != "" {
			job.Name = cache.name
		}
		job.FileSize, job.Files, job.Node = cache.size, cache.files, cache.node
	}
	if cached {
		if err := h.Jobs.Create(r.Context(), job); err != nil {
			stError(w, 500, "could not create download")
			return
		}
	} else {
		disposition, err := h.Slots.Admit(r.Context(), job, plan, false)
		if err != nil {
			stQueueError(w, err)
			return
		}
		if disposition == jobs.AdmissionAdmitted {
			h.assign(job)
		}
	}
	stJSON(w, 200, map[string]any{"data": h.magnetDataForTarget(r.Context(), job, target)})
}

func (h *Handler) deleteMagnet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	id := r.PathValue("id")
	job, err := h.Jobs.Get(r.Context(), id)
	if err != nil || job.UserID != user.ID {
		stError(w, 404, "not found")
		return
	}
	if job.Seed && job.Status == jobs.StatusSeeding {
		stError(w, 409, "seeding until it meets its ratio/time target")
		return
	}
	if job.Status == jobs.StatusComplete {
		if siblings, _ := h.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(siblings) <= 1 {
			job.UserID = "system"
			h.Jobs.Update(r.Context(), job)
			w.WriteHeader(204)
			return
		}
	}
	active := job.Status.Active() && job.Status != jobs.StatusQueued
	h.Jobs.Delete(r.Context(), id)
	if job.InputKey != "" {
		h.Store.Delete(r.Context(), job.InputKey)
	}
	if active {
		h.Bus.Publish(events.JobDeleted, events.Deleted{JobID: job.ID, InfoHash: job.InfoHash, Source: string(job.Source), Node: job.Node, UserID: job.UserID})
	}
	h.Slots.Wake()
	w.WriteHeader(204)
}

type coldPullChecker interface {
	ColdPullAllowed(ctx context.Context, userID string, perHour int) (bool, error)
}

func coldPullBlocked(ctx context.Context, c coldPullChecker, userID string, perHour int) bool {
	ok, err := c.ColdPullAllowed(ctx, userID, perHour)
	return err == nil && !ok
}

func displayName(m string) string { return magnet.DisplayName(m) }

func stQueueError(w http.ResponseWriter, err error) {
	if errors.Is(err, jobs.ErrQueueFull) {
		stError(w, 429, "download queue full")
		return
	}
	stError(w, 500, "could not queue download")
}

func (h *Handler) canUsenet(ctx context.Context, user *auth.User) bool {
	plan, _ := plans.Get(user.PlanID)
	if plan.SystemUsenet {
		return true
	}
	_, err := h.Users.GetUsenetCreds(ctx, user.ID)
	return err == nil && plans.CanBYOK(plan.ID)
}
