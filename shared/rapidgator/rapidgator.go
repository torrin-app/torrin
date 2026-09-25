package rapidgator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

var apiBase = "https://rapidgator.net/api/v2"

var (
	ErrNotAvailable = errors.New("rapidgator: file not available")
	ErrRateLimited  = errors.New("rapidgator: traffic limit reached")
	ErrAuth         = errors.New("rapidgator: authentication failed")
)

type Client struct {
	login    string
	password string
	http     *http.Client
	mu       sync.Mutex
	token    string
}

func New(login, password string) *Client {
	if login == "" || password == "" {
		return nil
	}
	return &Client{login: login, password: password, http: &http.Client{Timeout: 45 * time.Second}}
}

func IsURL(link string) bool {
	return strings.Contains(strings.ToLower(link), "rapidgator.net/file/")
}

func fileName(fileURL string) string {
	return path.Base(strings.TrimSuffix(fileURL, ".html"))
}

func (c *Client) DirectLink(ctx context.Context, fileURL string) (string, string, int64, error) {
	dl, size, err := c.resolve(ctx, fileURL, true)
	if err != nil {
		return "", "", 0, err
	}
	return fileName(fileURL), dl, size, nil
}

func (c *Client) resolve(ctx context.Context, fileURL string, canRetry bool) (string, int64, error) {
	tok, err := c.ensureToken(ctx)
	if err != nil {
		return "", 0, err
	}
	status, body, err := c.get(ctx, "/file/download", url.Values{"token": {tok}, "url": {fileURL}})
	if err != nil {
		return "", 0, err
	}
	switch status {
	case 200:
		var r struct {
			DownloadURL string `json:"download_url"`
			File        struct {
				Size int64 `json:"size"`
			} `json:"file"`
		}
		json.Unmarshal(body, &r)
		if r.DownloadURL == "" {
			return "", 0, ErrNotAvailable
		}
		return r.DownloadURL, r.File.Size, nil
	case 401, 403:
		if canRetry {
			c.clearToken()
			return c.resolve(ctx, fileURL, false)
		}
		return "", 0, ErrAuth
	case 404, 410:
		return "", 0, ErrNotAvailable
	case 423, 429:
		return "", 0, ErrRateLimited
	default:
		return "", 0, fmt.Errorf("rapidgator status %d", status)
	}
}

func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	status, body, err := c.get(ctx, "/user/login", url.Values{"login": {c.login}, "password": {c.password}})
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", ErrAuth
	}
	var r struct {
		Token string `json:"token"`
	}
	json.Unmarshal(body, &r)
	if r.Token == "" {
		return "", ErrAuth
	}
	c.token = r.Token
	return c.token, nil
}

func (c *Client) clearToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

func (c *Client) get(ctx context.Context, endpoint string, q url.Values) (int, json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var env struct {
		Status   int             `json:"status"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return 0, nil, err
	}
	return env.Status, env.Response, nil
}
