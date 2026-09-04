package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

const usenetMaxPage = 100

var usenetMeta = cinemeta.NewClient()

func (s *Server) registerUsenetSearchRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("GET /api/usenet/search", authMW(http.HandlerFunc(s.usenetSearch)))
	mux.Handle("GET /api/usenet/cached", authMW(http.HandlerFunc(s.usenetCached)))
	mux.Handle("POST /api/usenet/grab", authMW(http.HandlerFunc(s.usenetGrab)))
	mux.Handle("POST /api/usenet/indexer/test", authMW(http.HandlerFunc(s.testIndexer)))
	mux.Handle("POST /api/usenet/indexer", authMW(http.HandlerFunc(s.setIndexer)))
	mux.Handle("GET /api/usenet/indexer", authMW(http.HandlerFunc(s.getIndexer)))
	mux.Handle("DELETE /api/usenet/indexer", authMW(http.HandlerFunc(s.delIndexer)))
	mux.Handle("GET /api/usenet/indexers", authMW(http.HandlerFunc(s.listIndexers)))
	mux.Handle("POST /api/usenet/indexers", authMW(http.HandlerFunc(s.addIndexer)))
	mux.Handle("POST /api/usenet/indexers/test", authMW(http.HandlerFunc(s.testIndexer)))
	mux.Handle("PUT /api/usenet/indexers/{id}", authMW(http.HandlerFunc(s.editIndexer)))
	mux.Handle("DELETE /api/usenet/indexers/{id}", authMW(http.HandlerFunc(s.deleteIndexerH)))
	mux.Handle("POST /api/usenet/indexers/{id}/toggle", authMW(http.HandlerFunc(s.toggleIndexerH)))
	mux.Handle("GET /api/usenet/providers", authMW(http.HandlerFunc(s.listProviders)))
	mux.Handle("POST /api/usenet/providers", authMW(http.HandlerFunc(s.addProvider)))
	mux.Handle("POST /api/usenet/providers/test", authMW(http.HandlerFunc(s.testProvider)))
	mux.Handle("PUT /api/usenet/providers/{id}", authMW(http.HandlerFunc(s.editProvider)))
	mux.Handle("DELETE /api/usenet/providers/{id}", authMW(http.HandlerFunc(s.deleteProviderH)))
	mux.Handle("POST /api/usenet/providers/{id}/toggle", authMW(http.HandlerFunc(s.toggleProviderH)))
	mux.Handle("GET /api/usenet/system-indexer", authMW(http.HandlerFunc(s.getSystemIndexer)))
	mux.Handle("POST /api/usenet/system-indexer/toggle", authMW(http.HandlerFunc(s.toggleSystemIndexer)))
}

func (s *Server) resolveSources(ctx context.Context, userID string, plan plans.Plan) []indexer.Source {
	return s.Users.IndexerSources(ctx, userID, plan, s.IndexerURL, s.IndexerKey)
}

func (s *Server) usenetSearch(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	sources := s.resolveSources(r.Context(), user.ID, plan)
	if len(sources) == 0 {
		web.WriteError(w, 400, "configure a usenet indexer first")
		return
	}
	q := r.URL.Query()
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("ep"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit > usenetMaxPage {
		limit = usenetMaxPage
	}
	p := indexer.Params{IMDB: q.Get("imdb"), Query: q.Get("q"), Title: q.Get("title"), Cat: q.Get("cat"), Season: season, Episode: episode, Offset: offset, Limit: limit}
	if p.IMDB == "" && p.Query == "" {
		web.WriteError(w, 400, "provide imdb, q, or imdb+season+ep")
		return
	}
	if p.Title == "" && p.IMDB != "" {
		ct := "movie"
		if p.Season > 0 && p.Episode > 0 {
			ct = "series"
		}
		if t, err := usenetMeta.Title(r.Context(), p.IMDB, ct); err == nil && t != "" {
			p.Title = t
		}
	}
	web.WriteJSON(w, 200, s.usenetResults(r.Context(), user.ID, plan.ID, sources, p))
}

type busRequester interface {
	RequestJSON(subject string, payload any, timeout time.Duration) ([]byte, error)
}

func (s *Server) usenetResults(ctx context.Context, userID, planID string, sources []indexer.Source, p indexer.Params) []indexer.Result {
	if rq, ok := s.Bus.(busRequester); ok {
		req := indexer.SearchRequest{UserID: userID, PlanID: planID, Params: p}
		if data, err := rq.RequestJSON(indexer.SearchSubject, req, 22*time.Second); err == nil {
			var resp indexer.SearchResponse
			if json.Unmarshal(data, &resp) == nil && resp.Error == "" {
				if resp.Results == nil {
					return []indexer.Result{}
				}
				return resp.Results
			}
		}
	}
	return indexer.Search(ctx, sources, p)
}

func parseCachedTarget(raw string) (imdb string, season, episode int) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(raw), "tt"), ":")
	imdb = parts[0]
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	return
}

func cachedStreamResult(job *jobs.Job, stream jobs.Stream) map[string]any {
	return map[string]any{
		"name":       job.Name,
		"file_name":  stream.FileName,
		"size":       stream.Size,
		"info_hash":  job.InfoHash,
		"signed_url": stream.SignedURL,
	}
}

func (s *Server) usenetCached(w http.ResponseWriter, r *http.Request) {
	imdb, season, episode := parseCachedTarget(r.URL.Query().Get("imdb"))
	if imdb == "" {
		web.WriteJSON(w, 200, []any{})
		return
	}
	list, err := s.JobsPG.ListByIMDB(r.Context(), imdb)
	if err != nil {
		web.WriteError(w, 500, "cached lookup failed")
		return
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, j := range list {
		if j.Source != jobs.SourceUsenet || seen[j.InfoHash] {
			continue
		}
		seen[j.InfoHash] = true
		for _, st := range s.signStreams(j, r) {
			if season > 0 && episode > 0 && !episodeMatch(j, st.FileName, season, episode) {
				continue
			}
			out = append(out, cachedStreamResult(j, st))
		}
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) usenetGrab(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	sources := s.resolveSources(r.Context(), user.ID, plan)
	if len(sources) == 0 {
		web.WriteError(w, 400, "configure a usenet indexer first")
		return
	}
	var req struct {
		ID       string `json:"id"`
		Source   string `json:"source"`
		NZBURL   string `json:"nzb_url"`
		Title    string `json:"title"`
		IMDBID   string `json:"imdb_id"`
		Explicit bool   `json:"explicit"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.ID == "" {
		web.WriteError(w, 400, "id required")
		return
	}
	client := pickSource(sources, req.Source, req.NZBURL)
	if client == nil {
		web.WriteError(w, 404, "indexer not found")
		return
	}
	nzbData, err := client.DownloadNZB(&indexer.Result{ID: req.ID, NZBURL: req.NZBURL})
	if err != nil {
		web.WriteError(w, 502, "failed to download NZB")
		return
	}
	imdb, _, _ := parseCachedTarget(req.IMDBID)
	s.ingestNZB(w, r, user, plan, nzbData, req.Title, imdb, req.Explicit)
}

func pickSource(sources []indexer.Source, sourceID, nzbURL string) *indexer.Client {
	if c := indexer.Find(sources, sourceID); c != nil {
		return c
	}
	if host := urlHost(nzbURL); host != "" {
		for i := range sources {
			if urlHost(sources[i].Client.BaseURL()) == host {
				return sources[i].Client
			}
		}
	}
	if len(sources) == 1 {
		return sources[0].Client
	}
	return nil
}

func urlHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func normalizeIndexerURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	u = strings.TrimSuffix(u, "/api")
	return strings.TrimRight(u, "/")
}
