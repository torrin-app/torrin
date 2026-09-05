package stremthru

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
)

func (h *Handler) usenetOK(ctx context.Context, user *auth.User) bool {
	plan, _ := plans.Get(user.PlanID)
	_, err := h.Users.GetUsenetCreds(ctx, user.ID)
	return (err == nil && plans.CanBYOK(plan.ID)) || plan.SystemUsenet
}

func (h *Handler) checkNewz(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var hashes []string
	for _, p := range r.URL.Query()["hash"] {
		for _, x := range strings.Split(p, ",") {
			if x = strings.TrimSpace(x); x != "" {
				hashes = append(hashes, x)
			}
		}
	}
	if len(hashes) > 500 {
		stError(w, 400, "too many hashes, max allowed 500")
		return
	}

	items := []map[string]any{}
	entitled := h.usenetOK(r.Context(), user)
	var mapped map[string]string
	if entitled && len(hashes) > 0 {
		mapped, _ = h.Jobs.ContentHashesByURL(r.Context(), hashes)
	}
	for _, hh := range hashes {
		item := map[string]any{"hash": hh, "status": "unknown", "files": []any{}}
		if ch, ok := mapped[hh]; ok {
			if _, files, cached := h.cachedFiles(r.Context(), user.ID, ch); cached {
				item["status"] = "cached"
				item["files"] = files
			}
		}
		items = append(items, item)
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items}})
}

func (h *Handler) listNewz(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if !h.usenetOK(r.Context(), user) {
		stJSON(w, 200, map[string]any{"data": map[string]any{"items": []any{}, "total_items": 0}})
		return
	}
	userJobs, _ := jobs.ListAll(r.Context(), h.Jobs, user.ID)
	var usenet []*jobs.Job
	var contentHashes []string
	for _, j := range userJobs {
		if j.Source == jobs.SourceUsenet {
			usenet = append(usenet, j)
			contentHashes = append(contentHashes, j.InfoHash)
		}
	}
	urlByContent, _ := h.Jobs.URLHashesByContent(r.Context(), contentHashes)
	items := make([]map[string]any, 0, len(usenet))
	for _, j := range usenet {
		id := j.InfoHash
		if u, ok := urlByContent[j.InfoHash]; ok {
			id = u
		}
		items = append(items, map[string]any{
			"id": id, "hash": id, "name": j.Name, "size": j.FileSize,
			"status": stStatus(j.Status), "added_at": j.CreatedAt.Format(time.RFC3339),
		})
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items, "total_items": len(items)}})
}

func (h *Handler) getNewz(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if !h.usenetOK(r.Context(), user) {
		stError(w, 403, "your plan does not include usenet")
		return
	}
	id := r.PathValue("id")
	job, contentHash, ok := h.resolveNewz(r.Context(), user, id)
	if !ok {
		stError(w, 404, "not found")
		return
	}
	name, files, cached := h.cachedFiles(r.Context(), user.ID, contentHash)
	if !cached {
		files = []map[string]any{}
	}
	if name == "" {
		name = job.Name
	}
	status := stStatus(job.Status)
	if cached {
		status = "downloaded"
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{
		"id": id, "hash": id, "name": name, "size": job.FileSize,
		"status": status, "files": files, "added_at": job.CreatedAt.Format(time.RFC3339),
	}})
}

func (h *Handler) removeNewz(w http.ResponseWriter, r *http.Request, user *auth.User) {
	id := r.PathValue("id")
	job, _, ok := h.resolveNewz(r.Context(), user, id)
	if !ok || job.UserID != user.ID {
		stError(w, 404, "not found")
		return
	}
	h.Jobs.Delete(r.Context(), job.ID)
	if job.Status.Active() && job.Status != jobs.StatusQueued {
		h.Bus.Publish(events.JobDeleted, events.Deleted{JobID: job.ID, InfoHash: job.InfoHash, Source: string(job.Source), Node: job.Node, UserID: job.UserID})
	}
	h.Slots.Wake()
	stJSON(w, 200, map[string]any{"data": map[string]any{"id": id}})
}

func (h *Handler) resolveNewz(ctx context.Context, user *auth.User, id string) (*jobs.Job, string, bool) {
	contentHash := id
	if ch, err := h.Jobs.ContentHashByURL(ctx, id); err == nil && ch != "" {
		contentHash = ch
	}
	if siblings, _ := h.Jobs.ListByInfoHash(ctx, contentHash); len(siblings) > 0 {
		for _, j := range siblings {
			if j.UserID == user.ID {
				return j, contentHash, true
			}
		}
		return siblings[0], contentHash, true
	}
	if job, err := h.Jobs.Get(ctx, id); err == nil && job != nil {
		return job, job.InfoHash, true
	}
	return nil, "", false
}
