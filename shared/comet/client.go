package comet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Title    string    `json:"title"`
	Size     int64     `json:"size"`
	Seeders  int       `json:"seeders"`
	Peers    int       `json:"peers"`
	InfoHash string    `json:"infohash"`
	Tracker  string    `json:"tracker"`
	Cached   bool      `json:"cached"`
	PubDate  time.Time `json:"pubdate"`
}

type Params struct {
	Type    string
	IMDB    string
	Query   string
	Season  int
	Episode int
	Limit   int
}

type Client struct {
	baseURL string
	key     string
	http    *http.Client
}

func New(baseURL, key string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Search(ctx context.Context, p Params) ([]Result, error) {
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("type", p.Type)
	set("imdb", p.IMDB)
	set("q", p.Query)
	if p.Season > 0 {
		q.Set("season", strconv.Itoa(p.Season))
	}
	if p.Episode > 0 {
		q.Set("ep", strconv.Itoa(p.Episode))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/torznab/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set("X-Api-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("comet search: status %d", resp.StatusCode)
	}
	var body struct {
		Results []Result `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Results, nil
}
