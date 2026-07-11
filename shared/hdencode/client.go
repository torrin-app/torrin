package hdencode

import (
	"bytes"
	"context"
	"encoding/json"
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
	http      *http.Client
	solveHTTP *http.Client
	solverURL string
	mu        sync.Mutex
	cache     map[string]entry
	cacheTTL  time.Duration
}

type entry struct {
	results []Result
	at      time.Time
}

func NewClient(solverURL string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = safeurl.Dialer(false)
	jar, _ := cookiejar.New(nil)
	return &Client{
		http:      &http.Client{Timeout: 20 * time.Second, Transport: tr, Jar: jar},
		solveHTTP: &http.Client{Timeout: 120 * time.Second},
		solverURL: strings.TrimRight(solverURL, "/"),
		cache:     map[string]entry{},
		cacheTTL:  2 * time.Hour,
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
	if c.solverURL == "" {
		return nil, fmt.Errorf("hdencode solver not configured")
	}
	doc, err := c.solve(ctx, postURL)
	if err != nil {
		return nil, err
	}
	return release.BestArchive(doc, want), nil
}

func (c *Client) solve(ctx context.Context, postURL string) (*goquery.Document, error) {
	body, _ := json.Marshal(map[string]string{"url": postURL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.solverURL+"/resolve", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.solveHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hdencode solver: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		HTML  string `json:"html"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("hdencode solver decode: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("hdencode solver: %s", out.Error)
	}
	return goquery.NewDocumentFromReader(strings.NewReader(out.HTML))
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
