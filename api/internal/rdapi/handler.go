package rdapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/storage"
)

type Deps struct {
	Users *auth.Store
	Jobs  *jobs.Postgres
	Store *storage.Client
	Qbit  *qbit.Client
	Slots *middleware.SlotTracker
	Bus   *bus.Bus
}

type Handler struct{ Deps }

func New(d Deps) *Handler { return &Handler{d} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /rest/1.0/user", h.auth(h.getUser))
	mux.HandleFunc("GET /rest/1.0/torrents", h.auth(h.listTorrents))
	mux.HandleFunc("POST /rest/1.0/torrents/addMagnet", h.auth(h.addMagnet))
	mux.HandleFunc("POST /rest/1.0/torrents/selectFiles/{id}", h.auth(h.selectFiles))
	mux.HandleFunc("GET /rest/1.0/torrents/info/{id}", h.auth(h.torrentInfo))
	mux.HandleFunc("DELETE /rest/1.0/torrents/delete/{id}", h.auth(h.deleteTorrent))
	mux.HandleFunc("GET /rest/1.0/torrents/instantAvailability/{hashes...}", h.auth(h.instantAvailability))
	mux.HandleFunc("POST /rest/1.0/unrestrict/link", h.auth(h.unrestrictLink))
}

func (h *Handler) auth(next func(http.ResponseWriter, *http.Request, *auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == r.Header.Get("Authorization") {
			token = ""
		}
		if token == "" {
			token = r.URL.Query().Get("auth_token")
		}
		if token == "" {
			writeRDError(w, 401, 8, "bad token")
			return
		}
		user, err := h.Users.GetByAPIKey(r.Context(), token)
		if err != nil || user == nil {
			writeRDError(w, 401, 8, "bad token")
			return
		}
		if user.IsPaused() {
			writeRDError(w, 403, 9, "subscription paused")
			return
		}
		if time.Now().After(user.ExpiresAt) {
			writeRDError(w, 403, 9, "subscription expired")
			return
		}
		next(w, r, user)
	}
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request, user *auth.User) {
	premium := int(time.Until(user.ExpiresAt).Seconds())
	if premium < 0 {
		premium = 0
	}
	writeJSON(w, 200, map[string]any{
		"id": user.ID, "username": strings.Split(user.Email, "@")[0],
		"email": user.Email, "type": "premium", "premium": premium,
		"expiration": user.ExpiresAt.Format(time.RFC3339), "points": 0, "locale": "en", "avatar": "",
	})
}

func (h *Handler) listTorrents(w http.ResponseWriter, r *http.Request, user *auth.User) {
	userJobs, _ := h.Jobs.ListByUser(r.Context(), user.ID, 100)
	torrents := []map[string]any{}
	for _, j := range userJobs {
		torrents = append(torrents, jobToRDTorrent(j, h.Store))
	}
	writeJSON(w, 200, torrents)
}

func (h *Handler) addMagnet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	r.ParseForm()
	magnet := r.FormValue("magnet")
	if !strings.HasPrefix(magnet, "magnet:") {
		writeRDError(w, 400, 1, "missing magnet")
		return
	}
	infoHash := extractHash(magnet)
	if infoHash == "" {
		writeRDError(w, 400, 2, "invalid magnet")
		return
	}
	cached, _ := h.Store.Has(r.Context(), manifest.Path(infoHash))
	plan, _ := plans.Get(user.PlanID)

	if existing, err := h.Jobs.GetByInfoHash(r.Context(), infoHash); err == nil && existing != nil && existing.Status != jobs.StatusFailed {
		if existing.UserID == user.ID {
			writeJSON(w, 201, map[string]any{"id": existing.ID, "uri": "/rest/1.0/torrents/info/" + existing.ID})
			return
		}
		linked := &jobs.Job{
			UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: existing.Name,
			Source: jobs.SourceTorrent, Status: existing.Status, IMDBID: existing.IMDBID,
			Files: existing.Files, FileSize: existing.FileSize,
		}
		active := existing.Status.Active()
		if active && !h.Slots.Acquire(r.Context(), user.ID, plan) {
			writeRDError(w, 503, 20, "slot limit reached")
			return
		}
		h.Jobs.Create(r.Context(), linked)
		if active {
			h.Slots.Release(user.ID)
		}
		writeJSON(w, 201, map[string]any{"id": linked.ID, "uri": "/rest/1.0/torrents/info/" + linked.ID})
		return
	}

	if !cached && !h.Slots.Acquire(r.Context(), user.ID, plan) {
		writeRDError(w, 503, 20, "slot limit reached")
		return
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Source: jobs.SourceTorrent,
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
	writeJSON(w, 201, map[string]any{"id": job.ID, "uri": "/rest/1.0/torrents/info/" + job.ID})
}

func (h *Handler) selectFiles(w http.ResponseWriter, r *http.Request, user *auth.User) {
	job, err := h.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.UserID != user.ID {
		writeRDError(w, 404, 7, "unknown resource")
		return
	}
	if job.Status != jobs.StatusComplete && h.Qbit != nil && h.Qbit.Login() == nil {
		h.Qbit.Resume(job.InfoHash)
	}
	w.WriteHeader(204)
}

func (h *Handler) torrentInfo(w http.ResponseWriter, r *http.Request, user *auth.User) {
	job, err := h.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.UserID != user.ID {
		writeRDError(w, 404, 7, "unknown resource")
		return
	}
	writeJSON(w, 200, jobToRDTorrentFull(job, h.Store))
}

func (h *Handler) deleteTorrent(w http.ResponseWriter, r *http.Request, user *auth.User) {
	job, err := h.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil || job.UserID != user.ID {
		writeRDError(w, 404, 7, "unknown resource")
		return
	}
	if job.Status.Active() && h.Qbit != nil && h.Qbit.Login() == nil {
		h.Qbit.Delete(job.InfoHash)
	}
	h.Jobs.Delete(r.Context(), job.ID)
	w.WriteHeader(204)
}

func (h *Handler) instantAvailability(w http.ResponseWriter, r *http.Request, user *auth.User) {
	result := make(map[string]any)
	for _, hash := range strings.Split(r.PathValue("hashes"), "/") {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash == "" {
			continue
		}
		if _, _, files := h.manifestMeta(r.Context(), hash); files != nil {
			variant := make(map[string]map[string]any)
			for i, f := range files {
				variant[strconv.Itoa(i+1)] = map[string]any{"filename": f.Name, "filesize": f.Size}
			}
			result[hash] = map[string]any{"rd": []any{variant}}
		} else {
			result[hash] = map[string]any{}
		}
	}
	writeJSON(w, 200, result)
}

func (h *Handler) unrestrictLink(w http.ResponseWriter, r *http.Request, user *auth.User) {
	r.ParseForm()
	link := r.FormValue("link")
	raw := strings.TrimPrefix(link, "torrin://")
	key, suffix := raw, ""
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		key = raw[:i]
		if q, err := url.ParseQuery(raw[i+1:]); err == nil {
			suffix = manifest.StreamQuery(q.Get("ih"), "blobs/", q.Get("enc") == "1")
		}
	}
	if link == "" || !isValidKey(key) {
		writeRDError(w, 400, 1, "invalid link")
		return
	}
	parts := strings.Split(key, "/")
	filename := parts[len(parts)-1]
	download := h.Store.SignURL(key, 24*time.Hour)
	if creds, err := h.Users.GetStorageCreds(r.Context(), user.ID); err == nil && creds != nil && creds.Enabled && creds.IsRclone() {
		download = h.Store.SignURLNodeUser("", key, user.ID, 24*time.Hour) + "&byos=1"
	}
	download += suffix
	writeJSON(w, 200, map[string]any{
		"id": generateShortID(), "filename": filename, "mimeType": mimeFor(filename),
		"filesize": 0, "link": link, "host": "torrin.app", "chunks": 16, "crc": 1,
		"download": download, "streamable": 1,
	})
}

func (h *Handler) assign(job *jobs.Job) {
	h.Bus.Publish(events.JobAssigned, events.Assigned{
		JobID: job.ID, InfoHash: job.InfoHash, Magnet: job.Magnet,
		Source: string(jobs.SourceTorrent), MaxBytes: job.MaxBytes,
	})
}

func (h *Handler) manifestMeta(ctx context.Context, infoHash string) (string, int64, []jobs.File) {
	data, err := h.Store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return "", 0, nil
	}
	return manifest.Meta(data)
}
