package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/torrentfile"
)

const maxTorrentBytes = 5 << 20

func (s *Server) uploadTorrent(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentBytes)
	file, _, err := r.FormFile("torrent")
	if err != nil {
		web.WriteError(w, 400, "torrent file required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		web.WriteError(w, 400, "could not read torrent")
		return
	}
	plan, _ := plans.Get(user.PlanID)
	s.ingestTorrent(w, r, user, plan, data)
}

func (s *Server) ingestTorrent(w http.ResponseWriter, r *http.Request, user *auth.User, plan plans.Plan, data []byte) {
	meta, err := torrentfile.Parse(data)
	if err != nil {
		web.WriteError(w, 400, "invalid torrent file")
		return
	}

	if meta.Private {
		if !s.seedingAllowed(user) {
			web.WriteError(w, 400, "private torrents aren't supported yet")
			return
		}
		if code, msg := s.seedGate(r.Context(), user, plan, meta); code != 0 {
			web.WriteError(w, code, msg)
			return
		}
		job, err := s.createSeedJob(r.Context(), user, plan, meta, data)
		if err != nil {
			web.WriteError(w, 502, "failed to add seed")
			return
		}
		web.WriteJSON(w, 202, job)
		return
	}

	if s.Qbit == nil {
		web.WriteError(w, 503, "torrent engine unavailable")
		return
	}
	if plan.MaxTorrentBytes > 0 && meta.Size > plan.MaxTorrentBytes {
		web.WriteError(w, 413, "torrent exceeds your plan size limit")
		return
	}
	siblings, _ := s.Jobs.ListByInfoHash(r.Context(), meta.InfoHash)
	if existing := userJobForHash(siblings, user.ID); existing != nil {
		web.WriteJSON(w, 200, existing)
		return
	}
	if over, _ := s.Users.MonthlyQuotaExceeded(r.Context(), user.ID, plan.MonthlyIngestBytes); over {
		web.WriteError(w, 429, "monthly download limit reached, resets on the 1st")
		return
	}
	// Each attempt owns its staging key; a losing concurrent submission must
	// never overwrite or delete the canonical job's retry payload.
	inputKey := torrentfile.InputKey(user.ID, meta.InfoHash+"-"+uuid.NewString())
	if err := s.Store.Put(r.Context(), inputKey, bytes.NewReader(data), "application/x-bittorrent"); err != nil {
		web.WriteError(w, 500, "failed to stage torrent")
		return
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: meta.InfoHash, Name: meta.Name, FileSize: meta.Size,
		Source: jobs.SourceTorrent, InputKey: inputKey,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	disposition, err := s.Slots.Admit(r.Context(), job, plan, false)
	if err != nil {
		s.Store.Delete(r.Context(), inputKey)
		writeQueueError(w, err, s.Slots.MaxQueued())
		return
	}
	if disposition == jobs.AdmissionAdmitted {
		s.assign(job)
	}
	if disposition == jobs.AdmissionExisting {
		s.Store.Delete(r.Context(), inputKey)
		web.WriteJSON(w, http.StatusOK, job)
		return
	}
	web.WriteJSON(w, http.StatusAccepted, job)
}

func (s *Server) seedingAllowed(u *auth.User) bool {
	if s.SeedingEnabled {
		return true
	}
	return u != nil && u.Email != "" && s.SeedingAllowUsers[strings.ToLower(u.Email)]
}

func (s *Server) seedGate(ctx context.Context, user *auth.User, plan plans.Plan, meta *torrentfile.Meta) (int, string) {
	if s.QbitSeed == nil {
		return 503, "seed engine unavailable"
	}
	if user.PlanID == "free" {
		return 403, "seeding private trackers requires a paid plan"
	}
	if plan.MaxTorrentBytes > 0 && meta.Size > plan.MaxTorrentBytes {
		return 413, "torrent exceeds your plan size limit"
	}
	if used, limit := s.seedUsage(ctx, user), seedCap(plan, user); used >= limit {
		return 429, fmt.Sprintf("seed limit reached (%d/%d), free a seed or add slots", used, limit)
	}
	return 0, ""
}

func (s *Server) createSeedJob(ctx context.Context, user *auth.User, plan plans.Plan, meta *torrentfile.Meta, data []byte) (*jobs.Job, error) {
	if s.matchCrossSeed(ctx, meta) {
		return s.stageCrossSeed(ctx, user, plan, meta, data)
	}
	if ex, _ := s.Jobs.GetByInfoHash(ctx, meta.InfoHash); ex != nil && ex.UserID == user.ID && ex.Status != jobs.StatusFailed {
		return ex, nil
	}
	if err := s.QbitSeed.Login(); err != nil {
		return nil, err
	}
	if err := s.QbitSeed.AddTorrentFile(data, "torrin-seed"); err != nil {
		return nil, err
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: meta.InfoHash, Name: meta.Name, FileSize: meta.Size,
		Source: jobs.SourceTorrent, Status: jobs.StatusDownloading, Seed: true,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	if err := s.Jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func seedCap(p plans.Plan, u *auth.User) int {
	packs := u.SeedSlotPacks
	if packs > 2 {
		packs = 2
	}
	return p.SeedSlots + packs*10
}

func (s *Server) seedUsage(ctx context.Context, user *auth.User) int {
	list, _ := jobs.ListAll(ctx, s.Jobs, user.ID)
	n := 0
	for _, j := range list {
		if j.Seed && j.Status.Active() {
			n++
		}
	}
	return n
}

func (s *Server) matchCrossSeed(ctx context.Context, meta *torrentfile.Meta) bool {
	seeds, _ := s.Jobs.ListByStatus(ctx, jobs.StatusSeeding)
	want := meta.Sizes()
	for _, j := range seeds {
		if j.InfoHash != meta.InfoHash && torrentfile.SubsetBySize(want, j.FileSizes()) {
			return true
		}
	}
	return false
}

func (s *Server) stageCrossSeed(ctx context.Context, user *auth.User, plan plans.Plan, meta *torrentfile.Meta, data []byte) (*jobs.Job, error) {
	if err := s.Store.Put(ctx, torrentfile.XSeedKey(meta.InfoHash), bytes.NewReader(data), "application/x-bittorrent"); err != nil {
		return nil, err
	}
	files := make([]jobs.File, len(meta.Files))
	for i, f := range meta.Files {
		files[i] = jobs.File{Index: i, Name: f.Path, Size: f.Size}
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: meta.InfoHash, Name: meta.Name, FileSize: meta.Size,
		Source: jobs.SourceCrossSeed, Status: jobs.StatusPending, Seed: true,
		Files: files, MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	if err := s.Jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	s.assign(job)
	return job, nil
}
