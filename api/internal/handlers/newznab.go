package handlers

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

func (s *Server) registerNewznabRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /newznab/api", s.newznab)
	mux.HandleFunc("GET /newznab/getnzb", s.newznabGetNZB)
}

func (s *Server) newznab(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch q.Get("t") {
	case "", "caps":
		newznabCaps(w)
	case "search", "tvsearch", "movie":
		s.newznabSearch(w, r)
	default:
		newznabError(w, 202, "No such function")
	}
}

func (s *Server) newznabSearch(w http.ResponseWriter, r *http.Request) {
	user, err := s.Users.GetByAPIKey(r.Context(), newznabKey(r))
	if err != nil || user == nil {
		newznabError(w, 100, "Incorrect user credentials")
		return
	}
	plan, _ := plans.Get(user.PlanID)
	sources := s.resolveSources(r.Context(), user.ID, plan)
	if len(sources) == 0 {
		newznabError(w, 203, "usenet search needs a paid plan with the built-in indexer on, or your own indexer")
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
	if limit <= 0 || limit > usenetMaxPage {
		limit = usenetMaxPage
	}
	p := indexer.Params{
		IMDB: strings.TrimPrefix(strings.TrimSpace(q.Get("imdbid")), "tt"), Query: q.Get("q"),
		Cat: q.Get("cat"), Season: season, Episode: episode, Offset: offset, Limit: limit,
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
	results := s.usenetResults(r.Context(), user.ID, plan.ID, sources, p)
	writeNewznabFeed(w, reqBase(r), newznabKey(r), results)
}

func (s *Server) newznabGetNZB(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user, err := s.Users.GetByAPIKey(r.Context(), newznabKey(r))
	if err != nil || user == nil {
		newznabError(w, 100, "Incorrect user credentials")
		return
	}
	plan, _ := plans.Get(user.PlanID)
	client := pickSource(s.resolveSources(r.Context(), user.ID, plan), q.Get("s"), "")
	if client == nil {
		newznabError(w, 300, "indexer not found")
		return
	}
	data, err := client.DownloadNZB(&indexer.Result{ID: q.Get("id"), Category: q.Get("c")})
	if err != nil {
		newznabError(w, 300, "could not fetch nzb")
		return
	}
	w.Header().Set("Content-Type", "application/x-nzb")
	w.Header().Set("Content-Disposition", `attachment; filename="`+q.Get("id")+`.nzb"`)
	w.Write(data)
}

func writeNewznabFeed(w http.ResponseWriter, base, apikey string, results []indexer.Result) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/">` + "\n<channel>\n<title>Torrin</title>\n")
	for _, res := range results {
		dl := fmt.Sprintf("%s/newznab/getnzb?id=%s&s=%s&c=%s&apikey=%s", base,
			url.QueryEscape(res.ID), url.QueryEscape(res.Source), url.QueryEscape(res.Category), url.QueryEscape(apikey))
		size := strconv.FormatInt(res.Size, 10)
		b.WriteString("<item>\n")
		b.WriteString("<title>" + esc(res.Title) + "</title>\n")
		b.WriteString(`<guid isPermaLink="false">` + esc(res.ID) + "</guid>\n")
		b.WriteString("<link>" + esc(dl) + "</link>\n")
		if !res.PubDate.IsZero() {
			b.WriteString("<pubDate>" + res.PubDate.Format(time.RFC1123Z) + "</pubDate>\n")
		}
		b.WriteString(`<enclosure url="` + esc(dl) + `" length="` + size + `" type="application/x-nzb"/>` + "\n")
		b.WriteString(newznabAttr("category", res.Category))
		b.WriteString(newznabAttr("size", size))
		if res.Grabs > 0 {
			b.WriteString(newznabAttr("grabs", strconv.Itoa(res.Grabs)))
		}
		if res.IMDBID != "" {
			b.WriteString(newznabAttr("imdb", strings.TrimPrefix(res.IMDBID, "tt")))
		}
		b.WriteString("</item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(b.String()))
}

func newznabCaps(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="Torrin"/>
  <limits max="100" default="50"/>
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

func newznabError(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<error code="%d" description="%s"/>`, code, esc(desc))
}

func newznabAttr(name, value string) string {
	return `<newznab:attr name="` + name + `" value="` + esc(value) + `"/>` + "\n"
}

func esc(s string) string { return html.EscapeString(s) }

func newznabKey(r *http.Request) string {
	if k := r.URL.Query().Get("apikey"); k != "" {
		return k
	}
	if raw := r.Header.Get("Authorization"); strings.HasPrefix(raw, "Bearer ") {
		return strings.TrimPrefix(raw, "Bearer ")
	}
	return ""
}

func reqBase(r *http.Request) string {
	scheme := "https"
	if p := forwardedProto(r); p != "" {
		scheme = p
	} else if r.TLS == nil && localHost(r.Host) {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func forwardedProto(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
		return xf
	}
	if strings.Contains(r.Header.Get("CF-Visitor"), `"https"`) {
		return "https"
	}
	return ""
}

func localHost(host string) bool {
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host == "localhost" || strings.HasPrefix(host, "127.") ||
		strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "172.")
}
