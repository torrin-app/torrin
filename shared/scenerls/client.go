package scenerls

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/moistari/rls"
	"github.com/torrin-app/torrin/shared/release"
	"github.com/torrin-app/torrin/shared/safeurl"
)

const base = "https://scene-rls.net"

const nfoMarker = "nfo.scene-rls.net/view/"

const imdbMarker = "imdb.com/title/tt"

type Client struct {
	http     *http.Client
	mu       sync.Mutex
	cache    map[string]entry
	cacheTTL time.Duration
}

type entry struct {
	results []release.Result
	at      time.Time
}

func NewClient() *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = safeurl.Dialer(false)
	jar, _ := cookiejar.New(nil)
	return &Client{
		http:     &http.Client{Timeout: 20 * time.Second, Transport: tr, Jar: jar},
		cache:    map[string]entry{},
		cacheTTL: 2 * time.Hour,
	}
}

func (c *Client) SearchMovie(ctx context.Context, title string, year int) ([]release.Result, error) {
	yearTag := ""
	if year > 0 {
		yearTag = fmt.Sprintf("%d", year)
	}
	return c.search(ctx, title, yearTag, "")
}

func (c *Client) SearchTV(ctx context.Context, title string, season, episode int) ([]release.Result, error) {
	return c.search(ctx, title, "", fmt.Sprintf("s%02de%02d", season, episode))
}

func (c *Client) search(ctx context.Context, title, yearTag, epTag string) ([]release.Result, error) {
	query := dotted(title)
	if query == "" {
		return nil, nil
	}
	if epTag != "" {
		query += "." + strings.ToUpper(epTag)
	}
	c.mu.Lock()
	if e, ok := c.cache[query]; ok && time.Since(e.at) < c.cacheTTL {
		c.mu.Unlock()
		return filter(e.results, yearTag, epTag), nil
	}
	c.mu.Unlock()

	doc, err := release.FetchDoc(ctx, c.http, http.MethodGet, base+"/?s="+url.QueryEscape(query), nil)
	if err != nil {
		return nil, err
	}
	var out []release.Result
	doc.Find(".post").Each(func(_ int, s *goquery.Selection) {
		a := s.Find(".postTitle a[href]").First()
		href, _ := a.Attr("href")
		if !isPost(href) {
			return
		}
		title := strings.TrimSpace(a.Text())
		if title == "" {
			return
		}
		size := largestSize(s.Text())
		out = append(out, release.Result{Title: title, Size: size, SizeBytes: release.ParseSize(size), PostURL: href})
	})

	c.mu.Lock()
	c.cache[query] = entry{out, time.Now()}
	c.mu.Unlock()
	return filter(out, yearTag, epTag), nil
}

func imdbFromDoc(doc *goquery.Document) string {
	href, ok := doc.Find("a[href*='" + imdbMarker + "']").First().Attr("href")
	if !ok {
		return ""
	}
	i := strings.Index(href, imdbMarker)
	if i < 0 {
		return ""
	}
	id := "tt"
	for _, ch := range href[i+len(imdbMarker):] {
		if ch < '0' || ch > '9' {
			break
		}
		id += string(ch)
	}
	if len(id) <= 2 {
		return ""
	}
	return id
}

func (c *Client) Resolve(ctx context.Context, postURL, imdbID, title string) ([][]string, error) {
	if !isPost(postURL) {
		return nil, fmt.Errorf("not a scene-rls post url")
	}
	doc, err := release.FetchDoc(ctx, c.http, http.MethodGet, postURL, nil)
	if err != nil {
		return nil, err
	}
	if want := strings.TrimSpace(imdbID); want != "" {
		want = "tt" + strings.TrimPrefix(want, "tt")
		if got := imdbFromDoc(doc); got != "" && got != want {
			return nil, fmt.Errorf("imdb mismatch: post is %s, want %s", got, want)
		}
	}
	if parts := release.BestArchive(doc, title); len(parts) > 0 {
		return parts, nil
	}
	for _, v := range orderedViews(doc) {
		revealed, err := release.FetchDoc(ctx, c.http, http.MethodPost, v, strings.NewReader("submit=submit"))
		if err != nil {
			continue
		}
		if parts := release.BestArchive(revealed, title); len(parts) > 0 {
			return parts, nil
		}
	}
	return nil, fmt.Errorf("no usable links found")
}

func orderedViews(doc *goquery.Document) []string {
	byHost := map[string]string{}
	var others []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if !strings.Contains(href, nfoMarker) {
			return
		}
		label := strings.ToLower(strings.TrimSpace(s.Text()))
		for _, h := range release.Hosts {
			if strings.Contains(label, h) {
				if _, ok := byHost[h]; !ok {
					byHost[h] = href
				}
				return
			}
		}
		others = append(others, href)
	})
	var out []string
	for _, h := range release.Hosts {
		if v, ok := byHost[h]; ok {
			out = append(out, v)
		}
	}
	return append(out, others...)
}

func largestSize(text string) string {
	fields := strings.Fields(text)
	var best string
	var bestBytes int64
	for i := 0; i+1 < len(fields); i++ {
		num := strings.Trim(fields[i], ".,():")
		unit := strings.ToUpper(strings.Trim(fields[i+1], ".,():"))
		s := num + " " + unit
		if b := release.ParseSize(s); b > bestBytes {
			best, bestBytes = s, b
		}
	}
	return best
}

func dotted(title string) string {
	return strings.Join(strings.Fields(rls.MustNormalize(title)), ".")
}

func isPost(href string) bool {
	if !strings.HasPrefix(href, base+"/") {
		return false
	}
	slug := strings.Trim(strings.TrimPrefix(href, base+"/"), "/")
	if len(slug) < 8 || strings.Contains(slug, "/") {
		return false
	}
	switch slug {
	case "releases", "contact", "dmca", "request", "advanced-search", "watchlist":
		return false
	}
	for _, p := range []string{"category", "tag", "author", "page"} {
		if strings.HasPrefix(slug, p) {
			return false
		}
	}
	return true
}

func filter(results []release.Result, yearTag, epTag string) []release.Result {
	if yearTag == "" && epTag == "" {
		return results
	}
	var out []release.Result
	for _, r := range results {
		lower := strings.ToLower(r.Title)
		if yearTag != "" && !strings.Contains(lower, yearTag) {
			continue
		}
		if epTag != "" && !strings.Contains(lower, epTag) {
			continue
		}
		out = append(out, r)
	}
	return out
}
