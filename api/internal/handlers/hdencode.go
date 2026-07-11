package handlers

import (
	"net/http"
	"strconv"
	"sync"

	"github.com/torrin-app/torrin/api/internal/web"
	hd "github.com/torrin-app/torrin/shared/hdencode"
	"github.com/torrin-app/torrin/shared/jobs"
)

var (
	hdOnce   sync.Once
	hdClient *hd.Client
)

func hdenc() *hd.Client {
	hdOnce.Do(func() { hdClient = hd.NewClient("") })
	return hdClient
}

func (s *Server) registerHDEncodeRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("GET /api/hdencode/search", authMW(http.HandlerFunc(s.hdencodeSearch)))
	mux.Handle("POST /api/hdencode/grab", authMW(http.HandlerFunc(s.hdencodeGrab)))
}

func (s *Server) hdencodeSearch(w http.ResponseWriter, r *http.Request) {
	imdb := r.URL.Query().Get("imdb")
	if imdb == "" {
		web.WriteError(w, 400, "imdb required")
		return
	}
	var (
		results []hd.Result
		err     error
	)
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	if season > 0 {
		episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
		results, err = hdenc().SearchTV(r.Context(), imdb, season, episode)
	} else {
		results, err = hdenc().SearchMovie(r.Context(), imdb)
	}
	if err != nil {
		web.WriteError(w, 502, "hdencode search failed")
		return
	}
	s.writeReleaseResults(w, r, matchReleaseTitle(titleWants(r), results), jobs.SourceHDEncode)
}

func (s *Server) hdencodeGrab(w http.ResponseWriter, r *http.Request) {
	s.releaseGrab(w, r, jobs.SourceHDEncode, "hdencode requires a paid plan")
}
