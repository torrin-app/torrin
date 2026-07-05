package hdencode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/torrin-app/torrin/shared/release"
	"github.com/torrin-app/torrin/shared/safeurl"
)

const base = "https://hdencode.org"

type Result = release.Result

type Client struct {
	http     *http.Client
	mu       sync.Mutex
	cache    map[string]entry
	cacheTTL time.Duration
}

type entry struct {
	results []Result
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

func (c *Client) SearchMovie(ctx context.Context, imdbID string) ([]Result, error) {
	return c.search(ctx, imdbID, 0, 0)
}

func (c *Client) SearchTV(ctx context.Context, imdbID string, season, episode int) ([]Result, error) {
	return c.search(ctx, imdbID, season, episode)
}

func (c *Client) search(ctx context.Context, imdbID string, season, episode int) ([]Result, error) {
	imdbID = "tt" + strings.TrimPrefix(strings.TrimSpace(imdbID), "tt")
	c.mu.Lock()
	if e, ok := c.cache[imdbID]; ok && time.Since(e.at) < c.cacheTTL {
		c.mu.Unlock()
		return filterEp(e.results, season, episode), nil
	}
	c.mu.Unlock()

	doc, err := c.fetch(ctx, http.MethodGet, base+"/?s="+url.QueryEscape(imdbID), nil)
	if err != nil {
		return nil, err
	}
	var out []Result
	doc.Find("h5 a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if !isPost(href) {
			return
		}
		title, size := release.SplitTitle(strings.TrimSpace(s.Text()))
		if title != "" {
			out = append(out, Result{Title: title, Size: size, SizeBytes: release.ParseSize(size), PostURL: href})
		}
	})

	c.mu.Lock()
	c.cache[imdbID] = entry{out, time.Now()}
	c.mu.Unlock()
	return filterEp(out, season, episode), nil
}

func (c *Client) Resolve(ctx context.Context, postURL, _, want string) ([][]string, error) {
	if !isPost(postURL) {
		return nil, fmt.Errorf("not an hdencode post url")
	}
	doc, err := c.fetch(ctx, http.MethodGet, postURL, nil)
	if err != nil {
		return nil, err
	}
	form := url.Values{
		"content-protector-captcha": {"1"},
		"content-protector-submit":  {"View links"},
	}
	for _, name := range []string{"content-protector-token", "content-protector-ident", "chax-response"} {
		if v, ok := doc.Find("input[name='" + name + "']").Attr("value"); ok {
			form.Set(name, v)
		}
	}
	revealed, err := c.fetch(ctx, http.MethodPost, postURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	return release.BestArchive(revealed, want), nil
}

func (c *Client) fetch(ctx context.Context, method, u string, body io.Reader) (*goquery.Document, error) {
	return release.FetchDoc(ctx, c.http, method, u, body)
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
	case "advanced-search", "watchlist", "request", "dmca", "contact":
		return false
	}
	return !strings.HasPrefix(slug, "category") && !strings.HasPrefix(slug, "tag")
}

func filterEp(results []Result, season, episode int) []Result {
	if season <= 0 {
		return results
	}
	var out []Result
	for _, r := range results {
		if release.MatchesEpisode(r.Title, season, episode) {
			out = append(out, r)
		}
	}
	return out
}
