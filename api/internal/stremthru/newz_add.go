package stremthru

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/usenet/nzburl"
)

const maxNewzBytes = 20 * 1024 * 1024

func (h *Handler) addNewz(w http.ResponseWriter, r *http.Request, user *auth.User) {
	if !h.usenetOK(r.Context(), user) {
		stError(w, 403, "your plan does not include usenet")
		return
	}
	link, body, ok := h.readNewz(w, r)
	if !ok {
		return
	}

	parsed, err := nzb.ParseBytes(body)
	if err != nil || len(parsed.Files) == 0 {
		stError(w, 400, "invalid nzb file")
		return
	}

	texts := []string{parsed.Name()}
	for _, f := range parsed.Files {
		texts = append(texts, f.Subject, f.Filename)
	}
	if v := safety.Screen(texts...); v.Blocked {
		if v.Ban {
			h.Users.BanUser(r.Context(), user.ID, v.Reason)
			h.Users.AuditLog(r.Context(), user.ID, "banned", v.Reason, newzIP(r))
		}
		stError(w, 403, "content blocked by safety policy")
		return
	}

	plan, _ := plans.Get(user.PlanID)
	size := parsed.TotalSize()
	if plan.MaxTorrentBytes > 0 && size > plan.MaxTorrentBytes {
		stError(w, 422, "nzb too large")
		return
	}

	contentHash := nzb.Hash(parsed)
	id := contentHash
	if link != "" {
		id = nzburl.Hash(link)
		h.Jobs.MapNzbURL(r.Context(), id, contentHash)
	}

	name := parsed.Name()
	if name == "" {
		name = "usenet download"
	}
	status, code, msg := h.ensureNewzJob(r.Context(), user, plan, contentHash, name, size, body)
	if code != 0 {
		stError(w, code, msg)
		return
	}
	stJSON(w, 201, map[string]any{"data": map[string]any{"id": id, "hash": id, "status": status}})
}

func (h *Handler) readNewz(w http.ResponseWriter, r *http.Request) (link string, body []byte, ok bool) {
	if fh, _, err := formFile(r); err == nil {
		defer fh.Close()
		data, err := io.ReadAll(io.LimitReader(fh, maxNewzBytes))
		if err != nil {
			stError(w, 400, "could not read nzb")
			return "", nil, false
		}
		return "", data, true
	}

	var req struct {
		Link string `json:"link"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Link == "" {
		stError(w, 400, "link required")
		return "", nil, false
	}
	resp, err := safeurl.Client(30 * time.Second).Get(req.Link)
	if err != nil {
		stError(w, 502, "could not fetch nzb")
		return "", nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		stError(w, 502, "could not fetch nzb")
		return "", nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxNewzBytes))
	if err != nil {
		stError(w, 502, "could not fetch nzb")
		return "", nil, false
	}
	return req.Link, data, true
}

func (h *Handler) ensureNewzJob(ctx context.Context, user *auth.User, plan plans.Plan, contentHash, name string, size int64, body []byte) (string, int, string) {
	if cached, _ := h.Store.Has(ctx, manifest.Path(contentHash)); cached {
		mName, mSize, files := h.manifestMeta(ctx, contentHash)
		if mName != "" {
			name = mName
		}
		job := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: name, FileSize: mSize, Source: jobs.SourceUsenet, Status: jobs.StatusComplete, Files: files}
		if err := h.Jobs.Create(ctx, job); err != nil {
			return "", 500, "could not start this download"
		}
		return "downloaded", 0, ""
	}

	if existing, err := h.Jobs.GetByInfoHash(ctx, contentHash); err == nil && existing != nil &&
		existing.Status != jobs.StatusFailed && existing.Status != jobs.StatusEvicted {
		if existing.UserID == user.ID {
			return stStatus(existing.Status), 0, ""
		}
		linked := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: existing.Name, Source: jobs.SourceUsenet, Status: existing.Status, Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node}
		active := existing.Status.Active()
		if active && !h.Slots.Acquire(ctx, user.ID, plan) {
			return "", 429, "slot limit reached"
		}
		if err := h.Jobs.Create(ctx, linked); err != nil {
			if active {
				h.Slots.Release(user.ID)
			}
			return "", 500, "could not start this download"
		}
		if active {
			h.Slots.Release(user.ID)
		}
		return stStatus(linked.Status), 0, ""
	}

	if !h.Slots.Acquire(ctx, user.ID, plan) {
		return "", 429, "slot limit reached"
	}
	if err := h.Store.Put(ctx, nzb.StorageKey(contentHash), bytes.NewReader(body), "application/x-nzb"); err != nil {
		h.Slots.Release(user.ID)
		return "", 500, "failed to store nzb"
	}
	h.Users.SetJobNZB(ctx, contentHash, body)
	job := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: name, FileSize: size, Source: jobs.SourceUsenet, Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority}
	err := h.Jobs.Create(ctx, job)
	h.Slots.Release(user.ID)
	if err != nil {
		return "", 500, "could not start this download"
	}
	job.Node = cluster.TargetNode(context.Background(), h.Jobs, string(jobs.SourceUsenet), job.MaxBytes)
	h.Jobs.Update(context.Background(), job)
	h.Bus.Publish(events.JobAssigned, events.Assigned{JobID: job.ID, InfoHash: job.InfoHash, Source: string(jobs.SourceUsenet), MaxBytes: job.MaxBytes, Node: job.Node})
	return "queued", 0, ""
}

func formFile(r *http.Request) (io.ReadCloser, string, error) {
	f, hdr, err := r.FormFile("file")
	if err != nil {
		return nil, "", err
	}
	return f, hdr.Filename, nil
}

func newzIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
