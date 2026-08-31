package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

const usenetMaxPage = 100

func fallbackQuery(title string, season, episode int) string {
	if season > 0 && episode > 0 {
		return fmt.Sprintf("%s S%02dE%02d", title, season, episode)
	}
	return title
}

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
	sources := s.resolveSources(r.Context(), middleware.GetUser(r).ID, middleware.GetPlan(r))
	if len(sources) == 0 {
		web.WriteError(w, 400, "configure a usenet indexer first")
		return
	}
	q := r.URL.Query()
	imdb, query, title := q.Get("imdb"), q.Get("q"), q.Get("title")
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
	if imdb == "" && query == "" {
		web.WriteError(w, 400, "provide imdb, q, or imdb+season+ep")
		return
	}
	merged := indexer.FanOut(r.Context(), sources, 18*time.Second, func(c *indexer.Client) ([]indexer.Result, error) {
		return s.searchOne(c, imdb, query, title, q.Get("cat"), q.Get("season"), q.Get("ep"), season, episode, offset, limit)
	})
	results := indexer.Verify(indexer.Dedup(merged), imdb, title, season, episode)
	if results == nil {
		results = []indexer.Result{}
	}
	web.WriteJSON(w, 200, results)
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
			out = append(out, map[string]any{"name": j.Name, "file_name": st.FileName, "signed_url": st.SignedURL})
		}
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) searchOne(c *indexer.Client, imdb, query, title, cat, seasonStr, epStr string, season, episode, offset, limit int) ([]indexer.Result, error) {
	key := strings.Join([]string{c.BaseURL(), imdb, query, title, cat, seasonStr, epStr, strconv.Itoa(offset), strconv.Itoa(limit)}, "|")
	if cached, hit := usenetCacheGet(key); hit {
		return cached, nil
	}
	var results []indexer.Result
	var err error
	switch {
	case imdb != "" && season > 0 && episode > 0:
		results, err = c.SearchTV(imdb, season, episode, offset, limit)
	case imdb != "":
		results, err = c.SearchMovie(imdb, offset, limit)
	default:
		results, err = c.SearchQuery(query, cat, offset, limit)
	}
	if err != nil {
		return nil, err
	}
	if imdb != "" && title != "" && len(indexer.Verify(results, imdb, title, season, episode)) == 0 {
		if fb, e := c.SearchQuery(fallbackQuery(title, season, episode), "", offset, limit); e == nil {
			results = append(results, fb...)
		}
	}
	usenetCacheSet(key, results)
	return results, nil
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
