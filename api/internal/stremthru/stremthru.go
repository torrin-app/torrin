package stremthru

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/torrentclaw"
)

type store interface {
	Has(ctx context.Context, key string) (bool, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
	SignURL(path string, expiry time.Duration) string
	SignURLNode(node, path string, expiry time.Duration) string
}

type Deps struct {
	Users    *auth.Store
	Jobs     *jobs.Postgres
	Store    store
	Slots    *middleware.SlotTracker
	Bus      *bus.Bus
	TC       *torrentclaw.Client
	Qbit     *qbit.Client
	SysADKey string
	SysRDKey string
}

type Handler struct {
	Deps
	dedup sync.Map
}

func New(d Deps) *Handler { return &Handler{Deps: d} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v0/store/user", h.withAuth(h.getUser))
	mux.HandleFunc("GET /v0/store/magnets/check", h.withAuth(h.checkMagnets))
	mux.HandleFunc("GET /v0/store/magnets", h.withAuth(h.listMagnets))
	mux.HandleFunc("POST /v0/store/magnets", h.withAuth(h.addMagnet))
	mux.HandleFunc("GET /v0/store/magnets/{id}", h.withAuth(h.getMagnet))
	mux.HandleFunc("DELETE /v0/store/magnets/{id}", h.withAuth(h.deleteMagnet))
	mux.HandleFunc("POST /v0/store/link/generate", h.withAuth(h.generateLink))
}

func (h *Handler) withAuth(next func(http.ResponseWriter, *http.Request, *auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r.Header.Get("X-StremThru-Store-Authorization"))
		if token == "" {
			token = bearer(r.Header.Get("Authorization"))
		}
		if token == "" {
			stError(w, 401, "unauthorized")
			return
		}
		user, err := h.Users.GetByAPIKey(r.Context(), token)
		if err != nil || user == nil {
			stError(w, 401, "invalid api key")
			return
		}
		if user.IsPaused() {
			stError(w, 403, "subscription paused — resume at https://torrin.app")
			return
		}
		if time.Now().After(user.ExpiresAt) {
			stError(w, 403, "subscription expired — renew at https://torrin.app")
			return
		}
		next(w, r, user)
	}
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request, user *auth.User) {
	stJSON(w, 200, map[string]any{"data": map[string]any{
		"id": user.ID, "email": user.Email, "subscription_status": "premium"}})
}

func (h *Handler) generateLink(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var req struct {
		Link string `json:"link"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Link == "" {
		stError(w, 400, "link required")
		return
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{"link": req.Link}})
}

func (h *Handler) assign(job *jobs.Job) {
	h.Bus.Publish(events.JobAssigned, events.Assigned{
		JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
		Source: string(jobs.SourceTorrent), MaxBytes: job.MaxBytes,
	})
}

func (h *Handler) magnetData(ctx context.Context, j *jobs.Job) map[string]any {
	m := map[string]any{
		"id": j.ID, "hash": j.InfoHash, "magnet": magnet.Build(j.InfoHash, j.Name), "name": j.Name, "status": stStatus(j.Status),
		"size": j.FileSize, "added_at": j.CreatedAt.Format(time.RFC3339),
		"files": []map[string]any{},
	}
	if j.Status == jobs.StatusComplete {
		files := j.Files
		if _, _, mf := h.manifestMeta(ctx, j.InfoHash); mf != nil {
			files = mf
		}
		out := make([]map[string]any, len(files))
		for i, f := range files {
			key := manifest.ResolveKey(j.InfoHash, i, f.Key, f.Name)
			link := h.Store.SignURLNode(j.Node, key, 24*time.Hour) + manifest.StreamQuery(j.InfoHash, key, f.Enc)
			out[i] = fileEntry(i, f.Name, f.Size, link, f.MediaInfo)
		}
		m["files"] = out
	}
	return m
}

func fileEntry(index int, name string, size int64, link string, mi *mediainfo.Info) map[string]any {
	e := map[string]any{"index": index, "name": name, "path": "/" + name, "size": size, "link": link}
	if mi != nil {
		e["media_info"] = mi
	}
	return e
}

func (h *Handler) manifestMeta(ctx context.Context, infoHash string) (name string, size int64, files []jobs.File) {
	data, err := h.Store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return "", 0, nil
	}
	return manifest.Meta(data)
}

func (h *Handler) cachedFiles(ctx context.Context, infoHash string) (string, []map[string]any, bool) {
	name, _, fs := h.manifestMeta(ctx, infoHash)
	if fs == nil {
		return "", nil, false
	}
	out := make([]map[string]any, len(fs))
	for i, f := range fs {
		key := manifest.ResolveKey(infoHash, i, f.Key, f.Name)
		link := h.Store.SignURL(key, 24*time.Hour) + manifest.StreamQuery(infoHash, key, f.Enc)
		out[i] = fileEntry(i, f.Name, f.Size, link, f.MediaInfo)
	}
	return name, out, true
}

func stStatus(s jobs.Status) string {
	switch s {
	case jobs.StatusComplete:
		return "downloaded"
	case jobs.StatusDownloading, jobs.StatusProcessing, jobs.StatusPublishing:
		return "downloading"
	case jobs.StatusFailed:
		return "failed"
	default:
		return "queued"
	}
}

func extractHash(m string) string { return magnet.Hash(m) }

func bearer(h string) string {
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

func stJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func stError(w http.ResponseWriter, code int, msg string) {
	stJSON(w, code, map[string]any{"error": map[string]any{"code": code, "message": msg}})
}
