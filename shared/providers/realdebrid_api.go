package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/breaker"
)

const rdBase = "https://api.real-debrid.com/rest/1.0"

type rdClient struct {
	key      string
	http     *http.Client
	limiters *keyLimiter
}

func newRDClient(apiKey string) *rdClient {
	return &rdClient{
		key:      apiKey,
		http:     &http.Client{Timeout: 30 * time.Second},
		limiters: sharedLimiters("rd:"+apiKey, limitSpec{250, time.Minute}),
	}
}

type rdTorrent struct {
	ID       string   `json:"id"`
	Hash     string   `json:"hash"`
	Filename string   `json:"filename"`
	Bytes    int64    `json:"bytes"`
	Status   string   `json:"status"`
	Progress float64  `json:"progress"`
	Links    []string `json:"links"`
	Files    []rdFile `json:"files"`
}

func (c *rdClient) listTorrents(ctx context.Context, page, limit int) ([]rdTorrent, error) {
	var out []rdTorrent
	err := c.get(ctx, fmt.Sprintf("/torrents?page=%d&limit=%d", page, limit), &out)
	return out, err
}

type rdFile struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

type rdUnrestrict struct {
	Filename string `json:"filename"`
	FileSize int64  `json:"filesize"`
	Download string `json:"download"`
}

func (c *rdClient) addMagnet(ctx context.Context, magnet string) (string, error) {
	var r struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/torrents/addMagnet", url.Values{"magnet": {magnet}}, &r); err != nil {
		return "", err
	}
	return r.ID, nil
}

func (c *rdClient) torrent(ctx context.Context, id string) (*rdTorrent, error) {
	var t rdTorrent
	if err := c.get(ctx, "/torrents/info/"+id, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *rdClient) selectFiles(ctx context.Context, id, files string) error {
	return c.post(ctx, "/torrents/selectFiles/"+id, url.Values{"files": {files}}, nil)
}

func (c *rdClient) unrestrict(ctx context.Context, link string) (*rdUnrestrict, error) {
	var u rdUnrestrict
	if err := c.post(ctx, "/unrestrict/link", url.Values{"link": {link}}, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *rdClient) deleteTorrent(ctx context.Context, id string) error {
	return c.del(ctx, "/torrents/delete/"+id)
}

func (c *rdClient) get(ctx context.Context, path string, out any) error {
	waitLimits(ctx, c.limiters)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rdBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	return c.doJSON(req, out)
}

func (c *rdClient) post(ctx context.Context, path string, form url.Values, out any) error {
	waitLimits(ctx, c.limiters)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rdBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.doJSON(req, out)
}

func (c *rdClient) del(ctx context.Context, path string) error {
	waitLimits(ctx, c.limiters)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, rdBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	status, body, err := breaker.RoundTrip("realdebrid", c.http, req)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("realdebrid HTTP %d: %s", status, body)
	}
	return nil
}

func (c *rdClient) doJSON(req *http.Request, out any) error {
	status, body, err := breaker.RoundTrip("realdebrid", c.http, req)
	if err != nil {
		return err
	}
	if status >= 400 {
		return fmt.Errorf("realdebrid HTTP %d: %s", status, body)
	}
	if out != nil && len(body) > 0 {
		return json.Unmarshal(body, out)
	}
	return nil
}

type rdLimiter struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64
	last   time.Time
}

func newRDLimiter(max int, interval time.Duration) *rdLimiter {
	return &rdLimiter{
		tokens: float64(max),
		max:    float64(max),
		rate:   float64(max) / float64(interval),
		last:   time.Now(),
	}
}

func (l *rdLimiter) wait(ctx context.Context) {
	for {
		l.mu.Lock()
		now := time.Now()
		l.tokens += float64(now.Sub(l.last)) * l.rate
		if l.tokens > l.max {
			l.tokens = l.max
		}
		l.last = now
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
