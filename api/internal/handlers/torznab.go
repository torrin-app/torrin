package handlers

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/comet"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
)

const torznabMaxPage = 100

func (s *Server) registerTorznabRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /torznab/api", s.torznab)
}

func (s *Server) torznab(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("t") {
	case "", "caps":
		torznabCaps(w)
	case "search", "tvsearch", "tv-search", "movie", "movie-search":
		s.torznabSearch(w, r)
	default:
		newznabError(w, 202, "No such function")
	}
}

func (s *Server) torznabSearch(w http.ResponseWriter, r *http.Request) {
	user, err := s.Users.GetByAPIKey(r.Context(), newznabKey(r))
	if err != nil || user == nil {
		newznabError(w, 100, "Incorrect user credentials")
		return
	}
	if !plans.CanBYOK(user.PlanID) {
		newznabError(w, 203, "torrent search needs a paid plan")
		return
	}
	if s.Comet == nil {
		newznabError(w, 900, "torrent search is not available")
		return
	}
	q := r.URL.Query()
	season, _ := strconv.Atoi(q.Get("season"))
	episode, _ := strconv.Atoi(q.Get("ep"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > torznabMaxPage {
		limit = torznabMaxPage
	}
	typ := torznabType(q.Get("t"), season)
	results, err := s.Comet.Search(r.Context(), comet.Params{
		Type: typ, IMDB: normalizeIMDB(q.Get("imdbid")), Query: q.Get("q"),
		Season: season, Episode: episode, Limit: limit,
	})
	if err != nil {
		newznabError(w, 900, "torrent search failed")
		return
	}
	writeTorznabFeed(w, dedupeTorrents(s.markCached(r.Context(), results)), typ)
}

func (s *Server) markCached(ctx context.Context, in []comet.Result) []comet.Result {
	if s.CachedJobs == nil || len(in) == 0 {
		return in
	}
	hashes := make([]string, 0, len(in))
	for _, r := range in {
		if r.InfoHash != "" {
			hashes = append(hashes, strings.ToLower(r.InfoHash))
		}
	}
	cached, err := s.CachedJobs.CachedByHashes(ctx, hashes)
	if err != nil || len(cached) == 0 {
		return in
	}
	set := make(map[string]bool, len(cached))
	for h := range cached {
		set[strings.ToLower(h)] = true
	}
	for i := range in {
		if set[strings.ToLower(in[i].InfoHash)] {
			in[i].Cached = true
		}
	}
	return in
}

func torznabType(t string, season int) string {
	switch t {
	case "movie", "movie-search":
		return "movie"
	case "tvsearch", "tv-search":
		return "series"
	}
	if season > 0 {
		return "series"
	}
	return "movie"
}

func normalizeIMDB(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "tt") {
		return v
	}
	return "tt" + v
}

func dedupeTorrents(in []comet.Result) []comet.Result {
	best := map[string]comet.Result{}
	order := []string{}
	for _, r := range in {
		if r.InfoHash == "" {
			continue
		}
		h := strings.ToLower(r.InfoHash)
		if e, ok := best[h]; !ok || r.Seeders > e.Seeders {
			if !ok {
				order = append(order, h)
			}
			best[h] = r
		}
	}
	out := make([]comet.Result, 0, len(order))
	for _, h := range order {
		out = append(out, best[h])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cached != out[j].Cached {
			return out[i].Cached
		}
		return out[i].Seeders > out[j].Seeders
	})
	return out
}

func writeTorznabFeed(w http.ResponseWriter, results []comet.Result, typ string) {
	cat := "2000"
	if typ == "series" {
		cat = "5000"
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:torznab="http://torznab.com/schemas/2015/feed">` + "\n<channel>\n<title>Torrin</title>\n")
	for _, res := range results {
		mag := magnet.Build(res.InfoHash, res.Title)
		size := strconv.FormatInt(res.Size, 10)
		peers := res.Peers
		if peers < res.Seeders {
			peers = res.Seeders
		}
		title := res.Title
		if res.Cached {
			title += " [CACHED]"
		}
		b.WriteString("<item>\n")
		b.WriteString("<title>" + esc(title) + "</title>\n")
		b.WriteString(`<guid isPermaLink="false">` + esc(res.InfoHash) + "</guid>\n")
		if !res.PubDate.IsZero() {
			b.WriteString("<pubDate>" + res.PubDate.Format(time.RFC1123Z) + "</pubDate>\n")
		}
		b.WriteString(`<enclosure url="` + esc(mag) + `" length="` + size + `" type="application/x-bittorrent"/>` + "\n")
		b.WriteString(torznabAttr("category", cat))
		b.WriteString(torznabAttr("size", size))
		b.WriteString(torznabAttr("seeders", strconv.Itoa(res.Seeders)))
		b.WriteString(torznabAttr("peers", strconv.Itoa(peers)))
		b.WriteString(torznabAttr("infohash", res.InfoHash))
		b.WriteString(torznabAttr("magneturl", mag))
		if res.Cached {
			b.WriteString(torznabAttr("cached", "true"))
		}
		b.WriteString("</item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(b.String()))
}

func torznabCaps(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Torrin"/>
  <limits max="100" default="100"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,imdbid,season,ep"/>
    <movie-search available="yes" supportedParams="q,imdbid"/>
  </searching>
  <categories>
    <category id="2000" name="Movies"/>
    <category id="5000" name="TV"/>
  </categories>
</caps>`))
}

func torznabAttr(name, value string) string {
	return `<torznab:attr name="` + name + `" value="` + esc(value) + `"/>` + "\n"
}
