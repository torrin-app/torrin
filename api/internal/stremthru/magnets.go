package stremthru

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/keyed"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
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
	stJSON(w, 200, map[string]any{"data": h.magnetData(r.Context(), job)})
}

func (h *Handler) addMagnet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var req struct {
		Magnet string `json:"magnet"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Magnet == "" {
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

	source, mag := jobs.SourceTorrent, req.Magnet
	hdTitle := ""
	var hdSize int64
	if pURL, t, src, sz := h.Jobs.ReleaseLink(r.Context(), infoHash); pURL != "" {
		if !plans.CanBYOK(user.PlanID) {
			stError(w, 403, "this release requires a paid plan")
			return
		}
		source, mag, hdTitle, hdSize = jobs.Source(src), pURL, t, sz
	}

	cached := manifest.Playable(r.Context(), h.Store, infoHash)
	plan, _ := plans.Get(user.PlanID)

	existing, err := h.Jobs.GetByInfoHash(r.Context(), infoHash)
	if err == nil && existing != nil && existing.Status != jobs.StatusFailed && existing.Status != jobs.StatusEvicted {
		if existing.UserID == user.ID {
			stJSON(w, 200, map[string]any{"data": h.magnetData(r.Context(), existing)})
			return
		}
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: infoHash, Name: existing.Name, Magnet: mag,
			Source: source, Status: existing.Status, IMDBID: existing.IMDBID,
			Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node,
		}
		activeLink := existing.Status.Active()
		if activeLink && !h.Slots.Acquire(r.Context(), user.ID, plan) {
			stError(w, 429, "slot limit reached")
			return
		}
		h.Jobs.Create(r.Context(), linked)
		if activeLink {
			h.Slots.Release(user.ID)
		}
		stJSON(w, 200, map[string]any{"data": h.magnetData(r.Context(), linked)})
		return
	}

	if !cached && coldPullBlocked(r.Context(), h.Jobs, user.ID, plan.ColdPullsPerHour) {
		stError(w, 429, "hourly download limit reached, try later or upgrade")
		return
	}
	if !cached && !h.Slots.Acquire(r.Context(), user.ID, plan) {
		stError(w, 429, "slot limit reached")
		return
	}

	name := displayName(req.Magnet)
	if hdTitle != "" {
		name = hdTitle
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: mag, Name: name, FileSize: hdSize,
		Source: source, IMDBID: imdbFromSID(r.URL.Query().Get("sid")),
		Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	if cached {
		job.Status = jobs.StatusComplete
		job.Name, job.FileSize, job.Files = h.manifestMeta(r.Context(), infoHash)
	}
	h.Jobs.Create(r.Context(), job)
	if !cached {
		h.Slots.Release(user.ID)
		h.assign(job)
	}
	stJSON(w, 200, map[string]any{"data": h.magnetData(r.Context(), job)})
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
	active := job.Status.Active()
	h.Jobs.Delete(r.Context(), id)
	if active && h.Qbit != nil {
		if siblings, _ := h.Jobs.ListByInfoHash(r.Context(), job.InfoHash); len(siblings) == 0 {
			h.Qbit.Login()
			h.Qbit.Delete(job.InfoHash)
		}
	}
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

func imdbFromSID(sid string) string {
	if !strings.HasPrefix(sid, "tt") {
		return ""
	}
	if i := strings.Index(sid, ":"); i > 0 {
		sid = sid[:i]
	}
	return strings.TrimPrefix(sid, "tt")
}
