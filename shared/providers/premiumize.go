package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/breaker"
)

const pmBase = "https://www.premiumize.me"

type premiumize struct {
	key  string
	http *http.Client
}

func newPremiumize(key string) *premiumize {
	return &premiumize{key: key, http: &http.Client{Timeout: 30 * time.Second}}
}

func NewPremiumize(apiKey string) Provider { return newPremiumize(apiKey) }

func (p *premiumize) Name() string { return "premiumize" }

func (p *premiumize) Release(context.Context, string) error { return nil }

func (p *premiumize) Fetch(ctx context.Context, magnet, infoHash string) (*Result, error) {
	cached, err := p.cached(ctx, magnet)
	if err != nil || !cached {
		return nil, err
	}

	content, err := p.directDL(ctx, magnet)
	if err != nil {
		return nil, err
	}

	var files []Link
	for _, c := range content {
		if isVideoFile(c.Path) {
			files = append(files, Link{Name: filepath.Base(c.Path), Size: c.Size, URL: c.Link})
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	return &Result{Name: files[0].Name, Files: files}, nil
}

func PremiumizeCached(ctx context.Context, pmKey string, hashes []string) map[string]bool {
	p := newPremiumize(pmKey)
	form := url.Values{}
	for _, h := range hashes {
		form.Add("items[]", "magnet:?xt=urn:btih:"+h)
	}
	body, err := p.post(ctx, "/api/cache/check", form)
	if err != nil {
		return nil
	}
	var resp struct {
		Status   string `json:"status"`
		Response []bool `json:"response"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Status != "success" {
		return nil
	}
	out := make(map[string]bool, len(hashes))
	for i, h := range hashes {
		if i < len(resp.Response) {
			out[strings.ToLower(h)] = resp.Response[i]
		}
	}
	return out
}

func (p *premiumize) cached(ctx context.Context, magnet string) (bool, error) {
	form := url.Values{}
	form.Add("items[]", magnet)
	body, err := p.post(ctx, "/api/cache/check", form)
	if err != nil {
		return false, err
	}
	var resp struct {
		Status   string `json:"status"`
		Response []bool `json:"response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, err
	}
	return resp.Status == "success" && len(resp.Response) > 0 && resp.Response[0], nil
}

type pmContent struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Link string `json:"link"`
}

func (p *premiumize) directDL(ctx context.Context, magnet string) ([]pmContent, error) {
	body, err := p.post(ctx, "/api/transfer/directdl", url.Values{"src": {magnet}})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status  string      `json:"status"`
		Content []pmContent `json:"content"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("premiumize directdl: %s", resp.Status)
	}
	return resp.Content, nil
}

func (p *premiumize) post(ctx context.Context, path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pmBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = form.Encode()
	req.Header.Set("Authorization", "Bearer "+p.key)

	status, body, err := breaker.RoundTrip("premiumize", p.http, req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("premiumize HTTP %d: %s", status, body)
	}
	return body, nil
}
