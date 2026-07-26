package addon

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

type Server struct {
	users *auth.Store
	jobs  *jobs.Postgres
	store *storage.Client
	meta  *cinemeta.Client
}

func New(users *auth.Store, j *jobs.Postgres, store *storage.Client) *Server {
	return &Server{users: users, jobs: j, store: store, meta: cinemeta.NewClient()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{apiKey}/manifest.json", s.manifest)
	mux.HandleFunc("GET /{apiKey}/stream/{type}/{id}", s.stream)
	return cors(mux)
}

func (s *Server) manifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"id":          "app.torrin.stremio",
		"version":     "1.0.0",
		"name":        "Torrin",
		"description": "Stream your cached media via Torrin",
		"types":       []string{"movie", "series"},
		"catalogs":    []any{},
		"resources":   []string{"stream"},
		"idPrefixes":  []string{"tt"},
		"behaviorHints": map[string]any{
			"configurable":          false,
			"configurationRequired": false,
		},
	})
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	empty := map[string]any{"streams": []any{}}
	apiKey := r.PathValue("apiKey")
	contentID := strings.TrimSuffix(r.PathValue("id"), ".json")
	if apiKey == "" || contentID == "" {
		writeJSON(w, 200, empty)
		return
	}

	user, err := s.users.GetByAPIKey(r.Context(), apiKey)
	if err != nil || user == nil || user.Banned || user.IsPaused() || time.Now().After(user.ExpiresAt) {
		writeJSON(w, 200, empty)
		return
	}

	byos := s.userHasBYOS(r.Context(), user.ID)
	imdbID, infoHash := parseID(contentID)
	var streams []map[string]any
	if infoHash != "" {
		streams = append(streams, s.byHash(r, infoHash, user.ID, byos)...)
	}
	if imdbID != "" {
		streams = append(streams, s.byLibrary(r, r.PathValue("type"), imdbID, user.ID, byos)...)
	}

	if len(streams) > 0 && infoHash != "" {
		s.jobs.RecordView(r.Context(), infoHash, user.ID)
	}
	if len(streams) == 0 {
		writeJSON(w, 200, empty)
		return
	}
	writeJSON(w, 200, map[string]any{"streams": streams})
}

func parseID(contentID string) (imdbID, infoHash string) {
	candidate := strings.Split(contentID, ":")[0]
	if strings.HasPrefix(candidate, "tt") {
		return strings.TrimPrefix(candidate, "tt"), ""
	}
	if len(candidate) == 40 {
		return "", strings.ToLower(candidate)
	}
	return "", ""
}

func (s *Server) byHash(r *http.Request, infoHash, userID string, byos bool) []map[string]any {
	data, err := s.store.GetBytes(r.Context(), manifest.Path(infoHash))
	if err != nil {
		return nil
	}
	man, err := manifest.Parse(data)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, f := range man.Files {
		out = append(out, entry(f.FileName, s.streamURL(r, f.DirectURL, userID, byos)))
	}
	return out
}

func (s *Server) userHasBYOS(ctx context.Context, userID string) bool {
	creds, err := s.users.GetStorageCreds(ctx, userID)
	return err == nil && creds != nil && creds.Enabled && creds.IsRclone()
}

func (s *Server) streamURL(r *http.Request, key, userID string, byos bool) string {
	u := s.store.SignURL(key, 24*time.Hour)
	if byos {
		u = s.store.SignURLNodeUser("", key, userID, 24*time.Hour) + "&byos=1"
	}
	return georoute.URL(r, u)
}

func (s *Server) byLibrary(r *http.Request, contentType, imdbID, userID string, byos bool) []map[string]any {
	ctx := r.Context()
	seen := map[string]bool{}
	var out []map[string]any
	add := func(list []*jobs.Job) {
		for _, j := range list {
			if seen[j.InfoHash] {
				continue
			}
			seen[j.InfoHash] = true
			for i, f := range j.Files {
				key := manifest.Key(j.InfoHash, i, f.Name)
				out = append(out, entry(f.Name, s.streamURL(r, key, userID, byos)))
			}
		}
	}

	byImdb, _ := s.jobs.ListByIMDB(ctx, imdbID)
	add(byImdb)

	if byos {
		byosOwn, _ := s.jobs.ListUserByosByIMDB(ctx, userID, imdbID)
		add(byosOwn)
	}

	if title, err := s.meta.Title(ctx, imdbID, contentType); err == nil {
		if norm := jobs.NormTitle(title); norm != "" {
			byTitle, _ := s.jobs.ListByTitleNorm(ctx, norm)
			add(byTitle)
		}
	}
	return out
}

func entry(title, url string) map[string]any {
	return map[string]any{
		"name":  "Torrin",
		"title": title,
		"url":   url,
		"behaviorHints": map[string]any{
			"notWebReady": strings.HasSuffix(title, ".mkv"),
		},
	}
}

func cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
