package addon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/episodes"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
	"github.com/torrin-app/torrin/shared/stremioid"
)

type Server struct {
	users           *auth.Store
	jobs            jobRepository
	store           streamStore
	meta            titleResolver
	episodeResolver *episodes.Resolver
}

type jobRepository interface {
	jobs.CachedLookup
	RecordView(ctx context.Context, infoHash, userID string) (bool, error)
	ListByInfoHash(ctx context.Context, infoHash string) ([]*jobs.Job, error)
	ListByIMDB(ctx context.Context, imdbID string) ([]*jobs.Job, error)
	ListUserByosByIMDB(ctx context.Context, userID, imdbID string) ([]*jobs.Job, error)
	ListByTitleNorm(ctx context.Context, norm string) ([]*jobs.Job, error)
}

type streamStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
	SignURLNode(node, path string, expiry time.Duration) string
	SignURLNodeUser(node, path, userID string, expiry time.Duration) string
}

type titleResolver interface {
	Title(ctx context.Context, imdbID, contentType string) (string, error)
}

func New(users *auth.Store, j *jobs.Postgres, store *storage.Client) *Server {
	return &Server{users: users, jobs: j, store: store, meta: cinemeta.NewClient(), episodeResolver: episodes.New(cinemeta.NewClient())}
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
	id := stremioid.Parse(contentID)
	if !streamTargetMatchesType(r.PathValue("type"), id) {
		writeJSON(w, 200, empty)
		return
	}
	var streams []map[string]any
	if id.InfoHash != "" {
		streams = append(streams, s.byHash(r, id.InfoHash, user.ID)...)
	}
	if id.IMDBID != "" {
		streams = append(streams, s.byLibrary(r, r.PathValue("type"), id, user.ID, byos)...)
	}

	if len(streams) == 0 {
		writeJSON(w, 200, empty)
		return
	}
	if id.InfoHash != "" {
		s.jobs.RecordView(r.Context(), id.InfoHash, user.ID)
	}
	slog.Info("stremio: served", "user", user.ID, "id", contentID, "streams", len(streams))
	writeJSON(w, 200, map[string]any{"streams": streams})
}

func (s *Server) byHash(r *http.Request, infoHash, userID string) []map[string]any {
	data, err := s.store.GetBytes(r.Context(), manifest.Path(infoHash))
	if err == nil {
		man, parseErr := manifest.Parse(data)
		if parseErr == nil && len(man.Files) > 0 {
			return s.entries(r, infoHash, userID, "", false, manifestFiles(man), man.Name)
		}
		if parseErr != nil {
			slog.Warn("stremio: bad manifest", "hash", infoHash, "err", parseErr)
		}
	}

	if warm, ok := s.cachedJobs(r.Context(), []string{infoHash})[infoHash]; ok {
		if files := jobs.FilesForEpisode(warm, warm.Files, 0, 0); len(files) > 0 {
			return s.entries(r, infoHash, userID, warm.Node, false, files, warm.Name)
		}
	}
	candidates, _ := s.jobs.ListByInfoHash(r.Context(), infoHash)
	for _, candidate := range candidates {
		if candidate.Status != jobs.StatusComplete && candidate.Status != jobs.StatusSeeding {
			continue
		}
		files := jobs.FilesForEpisode(candidate, candidate.Files, 0, 0)
		if hasCairnFile(infoHash, files) {
			return s.entries(r, infoHash, userID, "", false, files, candidate.Name)
		}
	}
	return nil
}

func (s *Server) userHasBYOS(ctx context.Context, userID string) bool {
	creds, err := s.users.GetStorageCreds(ctx, userID)
	return err == nil && creds != nil && creds.Enabled && creds.IsRclone()
}

func (s *Server) streamURL(r *http.Request, infoHash, key, userID, node string, byos, enc bool) string {
	var u string
	if _, _, _, direct := cairn.ParseStreamPath(key); direct {
		u = s.store.SignURLNodeUser("", key, userID, 24*time.Hour)
	} else if byos {
		u = s.store.SignURLNodeUser(node, key, userID, 24*time.Hour) + "&byos=1"
	} else {
		u = s.store.SignURLNode(node, key, 24*time.Hour)
	}
	u += manifest.StreamQuery(infoHash, enc)
	return georoute.URL(r, u)
}

func (s *Server) byLibrary(r *http.Request, contentType string, id stremioid.ID, userID string, byos bool) []map[string]any {
	ctx := r.Context()
	order := []string{}
	grouped := map[string][]libraryCandidate{}
	add := func(list []*jobs.Job, fromBYOS bool) {
		for _, j := range list {
			if j == nil || j.InfoHash == "" {
				continue
			}
			if _, exists := grouped[j.InfoHash]; !exists {
				order = append(order, j.InfoHash)
			}
			copy := *j
			copy.Files = s.episodeResolver.Select(ctx, id.IMDBID, j, j.Files, id.Season, id.Episode)
			grouped[j.InfoHash] = append(grouped[j.InfoHash], libraryCandidate{job: &copy, byos: fromBYOS})
		}
	}

	byImdb, _ := s.jobs.ListByIMDB(ctx, id.IMDBID)
	add(byImdb, false)

	if byos {
		byosOwn, _ := s.jobs.ListUserByosByIMDB(ctx, userID, id.IMDBID)
		add(byosOwn, true)
	}

	if s.meta != nil {
		if title, err := s.meta.Title(ctx, id.IMDBID, contentType); err == nil {
			if norm := jobs.NormTitle(title); norm != "" {
				byTitle, _ := s.jobs.ListByTitleNorm(ctx, norm)
				add(byTitle, false)
			}
		}
	}

	warmByHash := s.cachedJobs(ctx, order)
	var out []map[string]any
	for _, hash := range order {
		if warm := warmByHash[hash]; warm != nil {
			if files := s.episodeResolver.Select(ctx, id.IMDBID, warm, warm.Files, id.Season, id.Episode); len(files) > 0 {
				out = append(out, s.entries(r, hash, userID, warm.Node, false, files, warm.Name)...)
				continue
			}
		}
		selected, files, ok := selectLibraryCandidate(grouped[hash], id, byos)
		if !ok {
			continue
		}
		out = append(out, s.entries(r, hash, userID, selected.job.Node, selected.byos, files, selected.job.Name)...)
	}
	return out
}

type libraryCandidate struct {
	job  *jobs.Job
	byos bool
}

func selectLibraryCandidate(candidates []libraryCandidate, id stremioid.ID, preferBYOS bool) (libraryCandidate, []jobs.File, bool) {
	for _, wantBYOS := range []bool{true, false} {
		if wantBYOS && !preferBYOS {
			continue
		}
		for _, candidate := range candidates {
			if candidate.byos != wantBYOS {
				continue
			}
			if files := libraryFiles(candidate.job, id); len(files) > 0 {
				return candidate, files, true
			}
		}
	}
	return libraryCandidate{}, nil, false
}

func manifestFiles(man *manifest.Manifest) []jobs.File {
	files := make([]jobs.File, len(man.Files))
	for i, file := range man.Files {
		files[i] = jobs.File{Index: i, Name: file.FileName, Size: file.FileSize, Key: file.DirectURL, Enc: file.Enc, MediaInfo: file.MediaInfo}
	}
	return files
}

func hasCairnFile(infoHash string, files []jobs.File) bool {
	for _, file := range files {
		key := manifest.ResolveKey(infoHash, file.Index, file.Key, file.Name)
		if _, _, _, ok := cairn.ParseStreamPath(key); ok {
			return true
		}
	}
	return false
}

func (s *Server) cachedJobs(ctx context.Context, hashes []string) map[string]*jobs.Job {
	if s.jobs == nil || len(hashes) == 0 {
		return nil
	}
	byHash, err := s.jobs.CachedByHashes(ctx, hashes)
	if err != nil {
		return nil
	}
	return byHash
}

func libraryFiles(j *jobs.Job, id stremioid.ID) []jobs.File {
	return jobs.FilesForEpisode(j, j.Files, id.Season, id.Episode)
}

func streamTargetMatchesType(contentType string, id stremioid.ID) bool {
	if id.InfoHash != "" {
		return contentType == "movie" || contentType == "series"
	}
	switch contentType {
	case "movie":
		return id.IMDBID != "" && !id.IsEpisode()
	case "series":
		return id.IsEpisode()
	default:
		return false
	}
}

func entry(title, streamURL, infoHash string, size int64) map[string]any {
	filename := path.Base(strings.ReplaceAll(title, `\`, "/"))
	parsedURL, _ := url.Parse(streamURL)
	hints := map[string]any{
		"filename":    filename,
		"notWebReady": parsedURL.Scheme != "https" || !strings.EqualFold(path.Ext(filename), ".mp4"),
	}
	if infoHash != "" {
		hints["bingeGroup"] = "torrin:" + strings.ToLower(infoHash)
	}
	if size > 0 {
		hints["videoSize"] = size
	}
	return map[string]any{
		"name":          "Torrin",
		"title":         filename,
		"description":   filename,
		"url":           streamURL,
		"behaviorHints": hints,
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
