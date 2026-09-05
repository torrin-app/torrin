package cinemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/useragent"
)

type Meta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ReleaseInfo string `json:"releaseInfo"`
}

const base = "https://v3-cinemeta.strem.io"

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = safeurl.Dialer(false)
	return &Client{http: &http.Client{Timeout: 10 * time.Second, Transport: tr}}
}

func (c *Client) Search(ctx context.Context, query, contentType string) ([]Meta, error) {
	if contentType != "series" {
		contentType = "movie"
	}
	url := fmt.Sprintf("%s/catalog/%s/top/search=%s.json", base, contentType, neturl.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", useragent.Default)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cinemeta status %d", resp.StatusCode)
	}
	var out struct {
		Metas []Meta `json:"metas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Metas, nil
}

type metaResp struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
	Videos  []struct {
		Season  int    `json:"season"`
		Episode int    `json:"episode"`
		Name    string `json:"name"`
	} `json:"videos"`
}

type Episode struct {
	Season int
	Number int
	Name   string
}

func (c *Client) Episodes(ctx context.Context, imdbID string) ([]Episode, error) {
	m, err := c.meta(ctx, imdbID, "series")
	if err != nil {
		return nil, err
	}
	out := make([]Episode, 0, len(m.Videos))
	for _, v := range m.Videos {
		if v.Season >= 0 && v.Episode > 0 {
			out = append(out, Episode{Season: v.Season, Number: v.Episode, Name: v.Name})
		}
	}
	return out, nil
}

func (c *Client) meta(ctx context.Context, imdbID, contentType string) (*metaResp, error) {
	if contentType != "series" {
		contentType = "movie"
	}
	id := "tt" + strings.TrimPrefix(imdbID, "tt")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/meta/%s/%s.json", base, contentType, id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", useragent.Default)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cinemeta status %d", resp.StatusCode)
	}
	var out struct {
		Meta metaResp `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out.Meta, nil
}

func (c *Client) Title(ctx context.Context, imdbID, contentType string) (string, error) {
	m, err := c.meta(ctx, imdbID, contentType)
	if err != nil {
		return "", err
	}
	return m.Name, nil
}

func (c *Client) Runtime(ctx context.Context, imdbID string) (int, error) {
	for _, kind := range []string{"movie", "series"} {
		if m, err := c.meta(ctx, imdbID, kind); err == nil && m != nil {
			if rt := parseRuntimeMinutes(m.Runtime); rt > 0 {
				return rt, nil
			}
		}
	}
	return 0, nil
}

func parseRuntimeMinutes(s string) int {
	s = strings.ToLower(s)
	mins := 0
	if i := strings.Index(s, "h"); i >= 0 {
		h, _ := strconv.Atoi(strings.TrimSpace(s[:i]))
		mins += h * 60
		s = s[i+1:]
	}
	s = strings.TrimSpace(strings.ReplaceAll(s, "min", ""))
	if fields := strings.Fields(s); len(fields) > 0 {
		m, _ := strconv.Atoi(fields[0])
		mins += m
	}
	return mins
}

func (c *Client) SeasonEpisodes(ctx context.Context, imdbID string, season int) (int, error) {
	m, err := c.meta(ctx, imdbID, "series")
	if err != nil {
		return 0, err
	}
	last := 0
	for _, v := range m.Videos {
		if v.Season == season && v.Episode > last {
			last = v.Episode
		}
	}
	return last, nil
}
