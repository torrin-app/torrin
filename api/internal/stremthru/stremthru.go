package stremthru

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/episodes"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/stremioid"
	"github.com/torrin-app/torrin/shared/torrentclaw"
)

type store interface {
	Has(ctx context.Context, key string) (bool, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
	Delete(ctx context.Context, key string) error
	SignURL(path string, expiry time.Duration) string
	SignURLNode(node, path string, expiry time.Duration) string
	SignURLNodeUser(node, path, userID string, expiry time.Duration) string
}

type cairnRepository interface {
	GetCairnArchive(ctx context.Context, infoHash string) (nzbKey, name string, ok bool)
	GetCairnNZB(ctx context.Context, infoHash string) ([]byte, bool)
}

type cairnStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

type Deps struct {
	EpisodeResolver *episodes.Resolver
	Users           *auth.Store
	Jobs            *jobs.Postgres
	CachedJobs      jobs.CachedLookup
	BYOS            byosLookup
	Store           store
	Cairns          cairnRepository
	CairnStore      cairnStore
	CairnCipher     *crypto.Stream
	CairnDirect     bool
	Slots           *middleware.SlotTracker
	Bus             *bus.Bus
	TC              *torrentclaw.Client
	Qbit            *qbit.Client
	SysADKey        string
	SysRDKey        string
}

type Handler struct {
	Deps
}

func New(d Deps) *Handler {
	if d.CachedJobs == nil && d.Jobs != nil {
		d.CachedJobs = d.Jobs
	}
	if d.Cairns == nil {
		d.Cairns = d.Users
	}
	if d.CairnStore == nil {
		d.CairnStore = d.Store
	}
	return &Handler{Deps: d}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v0/store/user", h.withAuth(h.getUser))
	mux.HandleFunc("GET /v0/store/magnets/check", h.withAuth(h.checkMagnets))
	mux.HandleFunc("GET /v0/store/magnets", h.withAuth(h.listMagnets))
	mux.HandleFunc("POST /v0/store/magnets", h.withAuth(h.addMagnet))
	mux.HandleFunc("GET /v0/store/magnets/{id}", h.withAuth(h.getMagnet))
	mux.HandleFunc("DELETE /v0/store/magnets/{id}", h.withAuth(h.deleteMagnet))
	mux.HandleFunc("POST /v0/store/link/generate", h.withAuth(h.generateLink))
	mux.HandleFunc("GET /v0/store/newz/check", h.withAuth(h.checkNewz))
	mux.HandleFunc("GET /v0/store/newz", h.withAuth(h.listNewz))
	mux.HandleFunc("POST /v0/store/newz", h.withAuth(h.addNewz))
	mux.HandleFunc("GET /v0/store/newz/{id}", h.withAuth(h.getNewz))
	mux.HandleFunc("DELETE /v0/store/newz/{id}", h.withAuth(h.removeNewz))
	mux.HandleFunc("POST /v0/store/newz/link/generate", h.withAuth(h.generateLink))
	mux.HandleFunc("GET /v0/store/torz/check", h.withAuth(h.checkMagnets))
	mux.HandleFunc("GET /v0/store/torz", h.withAuth(h.listMagnets))
	mux.HandleFunc("POST /v0/store/torz", h.withAuth(h.addMagnet))
	mux.HandleFunc("GET /v0/store/torz/{id}", h.withAuth(h.getMagnet))
	mux.HandleFunc("DELETE /v0/store/torz/{id}", h.withAuth(h.deleteMagnet))
	mux.HandleFunc("POST /v0/store/torz/link/generate", h.withAuth(h.generateLink))
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
			stError(w, 403, "subscription paused, resume at https://torrin.app")
			return
		}
		if time.Now().After(user.ExpiresAt) {
			stError(w, 403, "subscription expired, renew at https://torrin.app")
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
	cluster.Assign(context.Background(), h.Bus, h.Jobs, h.Jobs, job)
}

func (h *Handler) magnetData(ctx context.Context, j *jobs.Job) map[string]any {
	return h.magnetDataForTarget(ctx, j, stremioid.ID{})
}

// magnetDataForTarget keeps the persisted job as the pack/account record and
// applies the current playback target only to the response.
func (h *Handler) magnetDataForTarget(ctx context.Context, j *jobs.Job, target stremioid.ID) map[string]any {
	m := map[string]any{
		"id": j.ID, "hash": j.InfoHash, "magnet": magnet.Build(j.InfoHash, j.Name), "name": j.Name, "status": stStatus(j.Status),
		"size": j.FileSize, "added_at": j.CreatedAt.Format(time.RFC3339), "private": false,
		"files": []map[string]any{},
	}
	// Select before committing to a storage source: a warm subset must not
	// shadow a matching Cairn or owner-scoped BYOS copy.
	tryCopy := func(cached playableJobFiles, job *jobs.Job) bool {
		if cached.name == "" {
			cached.name = job.Name
		}
		files := h.EpisodeResolver.Select(ctx, target.IMDBID, job, cached.files, target.Season, target.Episode)
		if len(files) == 0 {
			return false
		}
		m["status"], m["size"], m["private"] = "downloaded", cached.size, cached.byos
		m["files"] = h.playableEntries(j.UserID, j.InfoHash, cached, files, target)
		if cached.name != "" {
			m["name"] = cached.name
		}
		return true
	}
	for _, lookup := range []func(context.Context, string) (playableJobFiles, bool){h.warmJobFiles, h.nodeJobFiles, h.cairnJobFiles} {
		if cached, ok := lookup(ctx, j.InfoHash); ok && tryCopy(cached, j) {
			return m
		}
	}
	if o := h.privateCopies(ctx, j.UserID, []string{j.InfoHash})[j.InfoHash]; o != nil {
		var size int64
		for _, f := range o.Files {
			size += f.Size
		}
		if tryCopy(playableJobFiles{name: o.Name, files: o.Files, size: size, byos: true}, privateJob(o)) {
			return m
		}
	}
	if j.Status == jobs.StatusComplete || j.Status == jobs.StatusSeeding {
		files := h.EpisodeResolver.Select(ctx, target.IMDBID, j, j.Files, target.Season, target.Episode)
		m["files"] = h.buildFileEntries(j.UserID, j.InfoHash, j.Node, files, target)
	}
	if target.IsEpisode() && len(m["files"].([]map[string]any)) == 0 && m["status"] == "downloaded" {
		m["status"] = "unknown"
		m["reason"] = "episode_not_found"
	}
	return m
}

func (h *Handler) buildFileEntries(userID, hash, node string, files []jobs.File, targets ...stremioid.ID) []map[string]any {
	files = jobs.FilesForEpisode(nil, files, 0, 0)
	out := make([]map[string]any, len(files))
	for i, f := range files {
		index := f.Index
		key := manifest.ResolveKey(hash, index, f.Key, f.Name)
		link := ""
		if _, _, _, ok := cairn.ParseStreamPath(key); ok {
			link = h.Store.SignURLNodeUser("", key, userID, 24*time.Hour)
		} else {
			link = h.Store.SignURLNode(node, key, 24*time.Hour)
		}
		link += manifest.StreamQuery(hash, f.Enc)
		out[i] = fileEntry(index, f.Name, f.Size, link, f.MediaInfo)
		if len(f.Episodes) > 0 {
			out[i]["episodes"] = f.Episodes
		}
		out[i]["stream_source"] = "cache"
		if _, _, _, direct := cairn.ParseStreamPath(key); direct {
			out[i]["stream_source"] = "cairn"
		}
		if len(targets) > 0 && targets[0].IsEpisode() {
			t := targets[0]
			out[i]["episode_match"] = fmt.Sprintf("tt%s:%d:%d", t.IMDBID, t.Season, t.Episode)
		}
	}
	return out
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

func stStatus(s jobs.Status) string {
	switch s {
	case jobs.StatusComplete, jobs.StatusSeeding:
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

func playbackTarget(raw string) (stremioid.ID, bool) {
	target := stremioid.Parse(raw)
	// Preserve generic/movie compatibility. An IMDb ID containing episode
	// separators is a series request and must parse completely; otherwise
	// returning an unfiltered pack would silently play the wrong season.
	valid := !(strings.HasPrefix(raw, "tt") && strings.Contains(raw, ":")) || target.IsEpisode()
	return target, valid
}

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
