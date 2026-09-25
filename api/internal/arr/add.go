package arr

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/keyed"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func (h *Handler) addTorrent(ctx context.Context, user *auth.User, plan plans.Plan, magnetLink, nameHint string) (string, string, int, string) {
	infoHash := magnet.Hash(magnetLink)
	if infoHash == "" {
		return "", "", 400, "invalid magnet"
	}
	defer keyed.Lock(infoHash)()
	cached := manifest.Playable(ctx, h.Store, infoHash)

	if existing, err := h.Jobs.GetByUserInfoHash(ctx, user.ID, infoHash); err == nil && existing != nil {
		return existing.ID, infoHash, 0, ""
	}
	if existing, err := h.Jobs.GetReusableByInfoHash(ctx, infoHash); err == nil && existing != nil {
		linked := &jobs.Job{UserID: user.ID, InfoHash: infoHash, Magnet: magnetLink, Name: existing.Name,
			Source: jobs.SourceTorrent, Status: existing.Status, IMDBID: existing.IMDBID,
			Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node}
		if linked.Status.Active() {
			disposition, err := h.Slots.Admit(ctx, linked, plan, true)
			if err != nil {
				if errors.Is(err, jobs.ErrQueueFull) {
					return "", "", 429, "download queue full"
				}
				return "", "", 500, "could not create download"
			}
			if disposition == jobs.AdmissionAdmitted {
				cluster.Assign(context.Background(), h.Bus, h.Jobs, h.Jobs, linked)
			}
		} else if _, err := h.Jobs.CreateOnce(ctx, linked); err != nil {
			return "", "", 500, "could not create download"
		}
		return linked.ID, infoHash, 0, ""
	}

	if cached {
		job := &jobs.Job{UserID: user.ID, InfoHash: infoHash, Magnet: magnetLink, Name: strings.TrimSpace(nameHint),
			Source: jobs.SourceTorrent, Status: jobs.StatusComplete}
		if name, size, files := h.manifestMeta(ctx, infoHash); files != nil {
			job.Name, job.FileSize, job.Files = name, size, files
		}
		if _, err := h.Jobs.CreateOnce(ctx, job); err != nil {
			return "", "", 500, "could not create download"
		}
		return job.ID, infoHash, 0, ""
	}

	if over, _ := h.Users.MonthlyQuotaExceeded(ctx, user.ID, plan.MonthlyIngestBytes); over {
		return "", "", 429, "monthly download limit reached, resets on the 1st"
	}
	if ok, _ := h.Jobs.ColdPullAllowed(ctx, user.ID, plan.ColdPullsPerHour); !ok {
		return "", "", 429, "hourly download limit reached, try later or upgrade"
	}
	job := &jobs.Job{UserID: user.ID, InfoHash: infoHash, Magnet: magnetLink, Name: strings.TrimSpace(nameHint),
		Source: jobs.SourceTorrent, Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority}
	disposition, err := h.Slots.Admit(ctx, job, plan, true)
	if err != nil {
		if errors.Is(err, jobs.ErrQueueFull) {
			return "", "", 429, "download queue full"
		}
		return "", "", 500, "could not create download"
	}
	if disposition == jobs.AdmissionAdmitted {
		cluster.Assign(context.Background(), h.Bus, h.Jobs, h.Jobs, job)
	}
	return job.ID, infoHash, 0, ""
}

func (h *Handler) addUsenet(ctx context.Context, user *auth.User, plan plans.Plan, body []byte, nameHint string) (string, string, int, string) {
	parsed, err := nzb.ParseBytes(body)
	if err != nil || len(parsed.Files) == 0 {
		return "", "", 400, "invalid nzb file"
	}
	texts := []string{parsed.Name()}
	for _, f := range parsed.Files {
		texts = append(texts, f.Subject, f.Filename)
	}
	if v := safety.Screen(texts...); v.Blocked {
		if v.Ban {
			h.Users.BanUser(ctx, user.ID, v.Reason)
		}
		return "", "", 403, "content blocked by safety policy"
	}
	size := parsed.TotalSize()
	if plan.MaxTorrentBytes > 0 && size > plan.MaxTorrentBytes {
		return "", "", 422, "nzb too large"
	}
	contentHash := nzb.Hash(parsed)
	defer keyed.Lock(contentHash)()
	name := strings.TrimSpace(nameHint)
	if name == "" {
		name = parsed.Name()
	}
	if name == "" {
		name = "usenet download"
	}

	if existing, err := h.Jobs.GetByUserInfoHash(ctx, user.ID, contentHash); err == nil && existing != nil {
		return existing.ID, contentHash, 0, ""
	}
	if manifest.Playable(ctx, h.Store, contentHash) {
		mName, mSize, files := h.manifestMeta(ctx, contentHash)
		if mName != "" {
			name = mName
		}
		job := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: name, FileSize: mSize,
			Source: jobs.SourceUsenet, Status: jobs.StatusComplete, Files: files}
		if _, err := h.Jobs.CreateOnce(ctx, job); err != nil {
			return "", "", 500, "could not start this download"
		}
		return job.ID, contentHash, 0, ""
	}
	if existing, err := h.Jobs.GetReusableByInfoHash(ctx, contentHash); err == nil && existing != nil {
		linked := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: existing.Name, Source: jobs.SourceUsenet,
			Status: existing.Status, Files: existing.Files, FileSize: existing.FileSize, Node: existing.Node}
		if linked.Status.Active() {
			disposition, err := h.Slots.Admit(ctx, linked, plan, true)
			if err != nil {
				if errors.Is(err, jobs.ErrQueueFull) {
					return "", "", 429, "download queue full"
				}
				return "", "", 500, "could not start this download"
			}
			if disposition == jobs.AdmissionAdmitted {
				cluster.Assign(context.Background(), h.Bus, h.Jobs, h.Jobs, linked)
			}
		} else if _, err := h.Jobs.CreateOnce(ctx, linked); err != nil {
			return "", "", 500, "could not start this download"
		}
		return linked.ID, contentHash, 0, ""
	}

	if over, _ := h.Users.MonthlyQuotaExceeded(ctx, user.ID, plan.MonthlyIngestBytes); over {
		return "", "", 429, "monthly download limit reached, resets on the 1st"
	}
	if ok, _ := h.Jobs.ColdPullAllowed(ctx, user.ID, plan.ColdPullsPerHour); !ok {
		return "", "", 429, "hourly download limit reached, try later or upgrade"
	}
	if err := h.Store.Put(ctx, nzb.StorageKey(contentHash), bytes.NewReader(body), "application/x-nzb"); err != nil {
		return "", "", 500, "failed to store nzb"
	}
	h.Users.SetJobNZB(ctx, contentHash, body)
	job := &jobs.Job{UserID: user.ID, InfoHash: contentHash, Name: name, FileSize: size, Source: jobs.SourceUsenet,
		Status: jobs.StatusPending, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority}
	disposition, err := h.Slots.Admit(ctx, job, plan, true)
	if err != nil {
		if errors.Is(err, jobs.ErrQueueFull) {
			return "", "", 429, "download queue full"
		}
		return "", "", 500, "could not start this download"
	}
	if disposition == jobs.AdmissionAdmitted {
		cluster.Assign(context.Background(), h.Bus, h.Jobs, h.Jobs, job)
	}
	return job.ID, contentHash, 0, ""
}

func (h *Handler) usenetOK(ctx context.Context, user *auth.User) bool {
	plan, _ := plans.Get(user.PlanID)
	if plan.SystemUsenet {
		return true
	}
	_, err := h.Users.GetUsenetCreds(ctx, user.ID)
	return err == nil && plans.CanBYOK(plan.ID)
}

func (h *Handler) manifestMeta(ctx context.Context, infoHash string) (string, int64, []jobs.File) {
	data, err := h.Store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return "", 0, nil
	}
	return manifest.Meta(data)
}
