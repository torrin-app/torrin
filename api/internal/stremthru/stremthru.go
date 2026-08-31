package stremthru

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/cluster"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/torrentclaw"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type store interface {
	Has(ctx context.Context, key string) (bool, error)
	GetBytes(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, body io.Reader, contentType string) error
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
	Users       *auth.Store
	Jobs        *jobs.Postgres
	Store       store
	Cairns      cairnRepository
	CairnStore  cairnStore
	CairnCipher *crypto.Stream
	CairnDirect bool
	Slots       *middleware.SlotTracker
	Bus         *bus.Bus
	TC          *torrentclaw.Client
	Qbit        *qbit.Client
	SysADKey    string
	SysRDKey    string
}

type Handler struct {
	Deps
}

func New(d Deps) *Handler {
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
	m := map[string]any{
		"id": j.ID, "hash": j.InfoHash, "magnet": magnet.Build(j.InfoHash, j.Name), "name": j.Name, "status": stStatus(j.Status),
		"size": j.FileSize, "added_at": j.CreatedAt.Format(time.RFC3339), "private": false,
		"files": []map[string]any{},
	}
	if name, size, files, ok := h.cachedJobFiles(ctx, j.InfoHash); ok {
		m["status"] = "downloaded"
		m["size"] = size
		m["files"] = h.buildFileEntries(j.UserID, j.InfoHash, h.Jobs.NodeForInfoHash(ctx, j.InfoHash), files)
		if name != "" {
			m["name"] = name
		}
	} else if j.Status == jobs.StatusComplete || j.Status == jobs.StatusSeeding {
		files := j.Files
		if _, _, mf := h.manifestMeta(ctx, j.InfoHash); mf != nil {
			files = mf
		}
		m["files"] = h.buildFileEntries(j.UserID, j.InfoHash, j.Node, files)
	}
	return m
}

func (h *Handler) buildFileEntries(userID, hash, node string, files []jobs.File) []map[string]any {
	out := make([]map[string]any, len(files))
	for i, f := range files {
		key := manifest.ResolveKey(hash, i, f.Key, f.Name)
		link := ""
		if _, _, _, ok := cairn.ParseStreamPath(key); ok {
			link = h.Store.SignURLNodeUser("", key, userID, 24*time.Hour)
		} else {
			link = h.Store.SignURLNode(node, key, 24*time.Hour)
		}
		link += manifest.StreamQuery(hash, f.Enc)
		out[i] = fileEntry(i, f.Name, f.Size, link, f.MediaInfo)
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

func (h *Handler) warmJobFiles(ctx context.Context, infoHash string) (string, int64, []jobs.File, bool) {
	if manifest.Playable(ctx, h.Store, infoHash) {
		name, size, files := h.manifestMeta(ctx, infoHash)
		if len(files) > 0 {
			return name, size, files, true
		}
	}
	return "", 0, nil, false
}

func (h *Handler) cairnJobFiles(ctx context.Context, infoHash string) (string, int64, []jobs.File, bool) {
	if !h.CairnDirect || h.Cairns == nil || h.CairnStore == nil {
		return "", 0, nil, false
	}
	_, name, archived := h.Cairns.GetCairnArchive(ctx, infoHash)
	if !archived {
		return "", 0, nil, false
	}
	data, err := h.CairnStore.GetBytes(ctx, nzb.StorageKey(infoHash))
	if err != nil {
		if fallback, ok := h.Cairns.GetCairnNZB(ctx, infoHash); ok {
			data, err = fallback, nil
		}
	}
	if err != nil {
		return "", 0, nil, false
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil || len(parsed.Files) == 0 {
		return "", 0, nil, false
	}
	enc := h.CairnCipher != nil
	files := make([]jobs.File, len(parsed.Files))
	var total int64
	for i, file := range parsed.Files {
		fileName := file.Filename
		if fileName == "" {
			fileName = file.Subject
		}
		fileName = filepath.Base(fileName)
		if fileName == "." || fileName == "" {
			return "", 0, nil, false
		}
		size := file.Size()
		if enc {
			size, err = h.CairnCipher.PlainSize(size)
			if err != nil {
				return "", 0, nil, false
			}
		}
		if size < 0 {
			return "", 0, nil, false
		}
		if total > math.MaxInt64-size {
			return "", 0, nil, false
		}
		total += size
		files[i] = jobs.File{Index: i, Name: fileName, Size: size, Key: cairn.StreamPath(infoHash, i, fileName), Enc: enc}
	}
	if name == "" {
		name = parsed.Name()
	}
	return name, total, files, true
}

func (h *Handler) cachedJobFiles(ctx context.Context, infoHash string) (string, int64, []jobs.File, bool) {
	if name, size, files, ok := h.warmJobFiles(ctx, infoHash); ok {
		return name, size, files, true
	}
	return h.cairnJobFiles(ctx, infoHash)
}

func (h *Handler) cachedFileEntries(ctx context.Context, userID, infoHash string, filesFn func(context.Context, string) (string, int64, []jobs.File, bool)) (string, []map[string]any, bool) {
	name, _, files, ok := filesFn(ctx, infoHash)
	if !ok {
		return "", nil, false
	}
	return name, h.buildFileEntries(userID, infoHash, h.Jobs.NodeForInfoHash(ctx, infoHash), files), true
}

func (h *Handler) warmCachedFiles(ctx context.Context, userID, infoHash string) (string, []map[string]any, bool) {
	return h.cachedFileEntries(ctx, userID, infoHash, h.warmJobFiles)
}

func (h *Handler) cairnCachedFiles(ctx context.Context, userID, infoHash string) (string, []map[string]any, bool) {
	return h.cachedFileEntries(ctx, userID, infoHash, h.cairnJobFiles)
}

func (h *Handler) cachedFiles(ctx context.Context, userID, infoHash string) (string, []map[string]any, bool) {
	if name, files, ok := h.warmCachedFiles(ctx, userID, infoHash); ok {
		return name, files, true
	}
	return h.cairnCachedFiles(ctx, userID, infoHash)
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
