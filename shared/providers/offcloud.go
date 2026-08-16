package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/breaker"
	"github.com/torrin-app/torrin/shared/magnet"
)

var (
	ocBase         = "https://offcloud.com"
	ocPollInterval = 5 * time.Second
)

const ocPollAttempts = 6

type offcloud struct {
	key  string
	http *http.Client
}

func newOffcloud(key string) *offcloud {
	return &offcloud{key: key, http: &http.Client{Timeout: 30 * time.Second}}
}

func NewOffcloud(apiKey string) Provider { return newOffcloud(apiKey) }

func (o *offcloud) Name() string { return "offcloud" }

func (o *offcloud) Release(ctx context.Context, handle string) error {
	if handle == "" {
		return nil
	}
	_, err := o.post(ctx, "/api/cloud/remove", map[string]any{"requests": []string{handle}})
	return err
}

func (o *offcloud) Fetch(ctx context.Context, magnet, infoHash string) (*Result, error) {
	id, name, err := o.add(ctx, magnet)
	if err != nil || id == "" {
		return nil, err
	}
	if !o.poll(ctx, id) {
		o.Release(context.Background(), id)
		return nil, ctx.Err()
	}
	files, err := o.explore(ctx, id, name)
	if err != nil || len(files) == 0 {
		o.Release(context.Background(), id)
		return nil, err
	}
	if name == "" {
		name = files[0].Name
	}
	return &Result{Name: name, Handle: id, Files: files}, nil
}

func OffcloudCached(ctx context.Context, key string, hashes []string) map[string]bool {
	return cachedCheck(ctx, "offcloud", key, hashes, func(ctx context.Context, hs []string) map[string]bool {
		o := newOffcloud(key)
		urls := make([]string, len(hs))
		for i, h := range hs {
			urls[i] = "magnet:?xt=urn:btih:" + h
		}
		body, err := o.post(ctx, "/api/cache/info", map[string]any{"urls": urls})
		if err != nil {
			return nil
		}
		var resp []struct {
			Cached bool `json:"cached"`
		}
		if json.Unmarshal(body, &resp) != nil {
			return nil
		}
		out := make(map[string]bool, len(hs))
		for i, h := range hs {
			if i < len(resp) {
				out[strings.ToLower(h)] = resp[i].Cached
			}
		}
		return out
	})
}

func OffcloudLibrary(ctx context.Context, key string) ([]LibraryItem, error) {
	o := newOffcloud(key)
	body, err := o.get(ctx, "/api/cloud/history")
	if err != nil {
		return nil, err
	}
	var items []struct {
		FileName     string `json:"fileName"`
		Status       string `json:"status"`
		OriginalLink string `json:"originalLink"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	var out []LibraryItem
	for _, it := range items {
		if it.Status != "downloaded" {
			continue
		}
		hash := strings.ToLower(strings.TrimSuffix(filepath.Base(it.OriginalLink), ".torrent"))
		if !magnet.Valid(hash) {
			continue
		}
		out = append(out, LibraryItem{Hash: hash, Filename: it.FileName})
	}
	return out, nil
}

func (o *offcloud) add(ctx context.Context, mag string) (string, string, error) {
	body, err := o.post(ctx, "/api/cloud", map[string]any{"url": mag})
	if err != nil {
		return "", "", err
	}
	var r struct {
		RequestID string `json:"requestId"`
		FileName  string `json:"fileName"`
		Error     string `json:"error"`
	}
	if json.Unmarshal(body, &r) != nil || r.Error != "" {
		return "", "", fmt.Errorf("offcloud add: %s", r.Error)
	}
	return r.RequestID, r.FileName, nil
}

func (o *offcloud) poll(ctx context.Context, id string) bool {
	for i := 0; i < ocPollAttempts; i++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(ocPollInterval):
		}
		body, err := o.post(ctx, "/api/cloud/status", map[string]any{"requestId": id})
		if err != nil {
			return false
		}
		var r struct {
			Status struct {
				Status string `json:"status"`
			} `json:"status"`
		}
		if json.Unmarshal(body, &r) != nil {
			return false
		}
		switch r.Status.Status {
		case "downloaded":
			return true
		case "error":
			return false
		}
	}
	return false
}

func (o *offcloud) explore(ctx context.Context, id, name string) ([]Link, error) {
	body, err := o.get(ctx, "/api/cloud/explore/"+id+"?format=detailed")
	if err != nil {
		return nil, err
	}
	var r struct {
		Files []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"url"`
		} `json:"files"`
	}
	if json.Unmarshal(body, &r) == nil && len(r.Files) > 0 {
		var files []Link
		for _, f := range r.Files {
			if isVideoFile(f.Name) {
				files = append(files, Link{Name: f.Name, Size: f.Size, URL: f.URL})
			}
		}
		return files, nil
	}
	return o.single(ctx, id, name)
}

func (o *offcloud) single(ctx context.Context, id, name string) ([]Link, error) {
	body, err := o.get(ctx, "/api/cloud/explore/"+id)
	if err == nil {
		var urls []string
		if json.Unmarshal(body, &urls) == nil {
			for _, u := range urls {
				if n := filepath.Base(u); isVideoFile(n) {
					return []Link{{Name: n, URL: u}}, nil
				}
			}
		}
	}
	if isVideoFile(name) {
		return []Link{{Name: name, URL: ocBase + "/cloud/download/" + id + "/" + url.PathEscape(name)}}, nil
	}
	return nil, nil
}

func (o *offcloud) post(ctx context.Context, path string, payload any) ([]byte, error) {
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ocBase+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	req.Header.Set("Content-Type", "application/json")
	return o.do(req)
}

func (o *offcloud) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ocBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.key)
	return o.do(req)
}

func (o *offcloud) do(req *http.Request) ([]byte, error) {
	status, body, err := breaker.RoundTrip("offcloud", o.http, req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("offcloud HTTP %d: %s", status, body)
	}
	return body, nil
}
