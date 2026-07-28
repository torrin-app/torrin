package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/safety"
)

const prewarmUser = "prewarm"

func (s *Server) prewarm(w http.ResponseWriter, r *http.Request) {
	if s.Internal == "" || !secretEqual(r.Header.Get("X-Internal-Secret"), s.Internal) {
		web.WriteError(w, 403, "forbidden")
		return
	}

	var req struct {
		Magnet     string `json:"magnet"`
		IMDBID     string `json:"imdb_id"`
		Name       string `json:"name"`
		Candidates []struct {
			InfoHash string `json:"info_hash"`
			Name     string `json:"name"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		web.WriteError(w, 400, "invalid json")
		return
	}

	type cand struct{ magnet, infoHash, name string }
	var cands []cand
	for _, c := range req.Candidates {
		ih := strings.ToLower(strings.TrimSpace(c.InfoHash))
		if len(ih) != 40 {
			continue
		}
		nm := c.Name
		if nm == "" {
			nm = req.Name
		}
		cands = append(cands, cand{
			magnet:   magnet.Build(ih, nm),
			infoHash: ih, name: nm,
		})
	}
	if len(cands) == 0 && req.Magnet != "" {
		if m := cleanMagnet(req.Magnet); strings.HasPrefix(m, "magnet:") {
			if ih := extractInfoHash(m); ih != "" {
				cands = append(cands, cand{magnet: m, infoHash: ih, name: req.Name})
			}
		}
	}
	if len(cands) == 0 {
		web.WriteError(w, 400, "no valid candidates")
		return
	}

	ctx := r.Context()
	if active, _ := s.JobsPG.ActiveCount(ctx, prewarmUser); active >= s.PrewarmMaxActive {
		web.WriteJSON(w, 429, map[string]string{"status": "busy"})
		return
	}
	if cachedSize, _ := s.JobsPG.CachedSizeByUser(ctx, prewarmUser); cachedSize >= s.PrewarmCapBytes {
		web.WriteJSON(w, 429, map[string]string{"status": "cap_reached"})
		return
	}

	for i, c := range cands {
		if v := safety.Screen(c.magnet, c.name); v.Blocked {
			continue
		}
		if cached, _ := s.Store.Has(ctx, manifest.Path(c.infoHash)); cached {
			web.WriteJSON(w, 200, map[string]string{"status": "cached"})
			return
		}
		existing, _ := s.Jobs.GetByInfoHash(ctx, c.infoHash)
		if existing != nil {
			if existing.Status == jobs.StatusFailed {
				continue
			}
			if existing.Status != jobs.StatusEvicted {
				web.WriteJSON(w, 200, map[string]string{"status": "exists"})
				return
			}
		}
		job := &jobs.Job{
			UserID:   prewarmUser,
			InfoHash: c.infoHash,
			Magnet:   c.magnet,
			Name:     c.name,
			IMDBID:   req.IMDBID,
			Source:   jobs.SourceTorrent,
			Status:   jobs.StatusPending,
			Priority: -100,
			MaxBytes: s.PrewarmMaxBytes,
		}
		if err := s.Jobs.Create(ctx, job); err != nil {
			web.WriteError(w, 500, "internal error")
			return
		}
		var remaining []string
		for _, rc := range cands[i+1:] {
			remaining = append(remaining, rc.infoHash)
		}
		s.JobsPG.StorePrewarmFallback(ctx, job.ID, req.IMDBID, c.name, remaining)
		s.assign(job)
		web.WriteJSON(w, 200, map[string]string{"status": "queued", "id": job.ID, "hash": c.infoHash})
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "exhausted"})
}
