package arr

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/bus"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/qbit"
	"github.com/torrin-app/torrin/shared/storage"
)

type Deps struct {
	Users    *auth.Store
	Jobs     *jobs.Postgres
	Store    *storage.Client
	Slots    *middleware.SlotTracker
	Bus      *bus.Bus
	Qbit     *qbit.Client
	SavePath string
}

type Handler struct {
	Deps
	labels *labelStore
}

func New(ctx context.Context, d Deps) *Handler {
	if strings.TrimSpace(d.SavePath) == "" {
		d.SavePath = "/downloads"
	}
	var h Handler
	h.Deps = d
	if d.Jobs != nil {
		h.labels = newLabelStore(ctx, d.Jobs.Pool())
	} else {
		h.labels = &labelStore{}
	}
	return &h
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/auth/login", h.qbitLogin)
	mux.HandleFunc("GET /api/v2/app/version", h.qbitVersion)
	mux.HandleFunc("GET /api/v2/app/webapiVersion", h.qbitWebAPIVersion)
	mux.HandleFunc("GET /api/v2/app/preferences", h.qbitAuth(h.qbitPreferences))
	mux.HandleFunc("GET /api/v2/torrents/categories", h.qbitAuth(h.qbitCategories))
	mux.HandleFunc("POST /api/v2/torrents/createCategory", h.qbitAuth(h.qbitOK))
	mux.HandleFunc("POST /api/v2/torrents/editCategory", h.qbitAuth(h.qbitOK))
	mux.HandleFunc("POST /api/v2/torrents/setCategory", h.qbitAuth(h.qbitOK))
	mux.HandleFunc("POST /api/v2/torrents/add", h.qbitAuth(h.qbitAdd))
	mux.HandleFunc("GET /api/v2/torrents/info", h.qbitAuth(h.qbitInfo))
	mux.HandleFunc("GET /api/v2/torrents/properties", h.qbitAuth(h.qbitProperties))
	mux.HandleFunc("GET /api/v2/torrents/files", h.qbitAuth(h.qbitFiles))
	mux.HandleFunc("POST /api/v2/torrents/delete", h.qbitAuth(h.qbitDelete))
	for _, p := range []string{"pause", "resume", "start", "stop", "setForceStart", "setShareLimits", "topPrio", "setAutoManagement", "recheck", "reannounce"} {
		mux.HandleFunc("POST /api/v2/torrents/"+p, h.qbitAuth(h.qbitOK))
	}

	for _, p := range []string{"/api", "/sabnzbd/api"} {
		mux.HandleFunc("GET "+p, h.sab)
		mux.HandleFunc("POST "+p, h.sab)
	}
}

func (h *Handler) qbitAuth(next func(http.ResponseWriter, *http.Request, *auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := h.resolve(r.Context(), qbitToken(r))
		if user == nil {
			writePlain(w, 403, "Forbidden")
			return
		}
		next(w, r, user)
	}
}

func qbitToken(r *http.Request) string {
	if c, err := r.Cookie("SID"); err == nil && c.Value != "" {
		return c.Value
	}
	if raw := r.Header.Get("Authorization"); strings.HasPrefix(raw, "Bearer ") {
		return strings.TrimPrefix(raw, "Bearer ")
	}
	return r.URL.Query().Get("apikey")
}

func (h *Handler) resolve(ctx context.Context, token string) *auth.User {
	if token == "" {
		return nil
	}
	user, err := h.Users.GetByAPIKey(ctx, token)
	if err != nil || user == nil || user.IsPaused() || time.Now().After(user.ExpiresAt) {
		return nil
	}
	return user
}
