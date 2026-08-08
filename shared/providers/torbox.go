package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/breaker"
)

var tbBase = "https://api.torbox.app/v1/api"

type torbox struct {
	key      string
	http     *http.Client
	limiters *keyLimiter
}

func newTorbox(key string) *torbox {
	return &torbox{
		key:      key,
		http:     &http.Client{Timeout: 30 * time.Second},
		limiters: sharedLimiters("tb:"+key, limitSpec{300, time.Minute}),
	}
}

func NewTorBox(apiKey string) Provider { return newTorbox(apiKey) }

func (t *torbox) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	return t.do(req)
}

func (t *torbox) Name() string { return "torbox" }

func (t *torbox) Fetch(ctx context.Context, magnet, infoHash string) (*Result, error) {
	cached, err := t.cached(ctx, infoHash)
	if err != nil || cached == nil {
		return nil, err
	}

	id, err := t.createTorrent(ctx, magnet)
	if err != nil {
		return nil, err
	}

	tor, err := t.listTorrent(ctx, id)
	if err != nil {
		t.Release(context.Background(), strconv.Itoa(id))
		return nil, err
	}
	if !tor.DownloadFinished || !tor.DownloadPresent {
		t.Release(context.Background(), strconv.Itoa(id))
		return nil, nil
	}

	var files []Link
	for _, f := range tor.Files {
		if !isVideoFile(f.Name) {
			continue
		}
		dlURL, err := t.requestLink(ctx, id, f.ID)
		if err != nil {
			continue
		}
		files = append(files, Link{Name: filepath.Base(f.Name), Size: f.Size, URL: dlURL})
	}
	if len(files) == 0 {
		t.Release(context.Background(), strconv.Itoa(id))
		return nil, nil
	}

	name := cached.Name
	if name == "" {
		name = tor.Name
	}
	if name == "" {
		name = files[0].Name
	}
	return &Result{Name: name, Handle: strconv.Itoa(id), Files: files}, nil
}

type tbFile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type tbTorrent struct {
	Hash             string   `json:"hash"`
	Name             string   `json:"name"`
	Size             int64    `json:"size"`
	Progress         float64  `json:"progress"`
	DownloadFinished bool     `json:"download_finished"`
	DownloadPresent  bool     `json:"download_present"`
	Files            []tbFile `json:"files"`
}

func (t *torbox) listTorrent(ctx context.Context, id int) (*tbTorrent, error) {
	u := fmt.Sprintf("%s/torrents/mylist?id=%d&bypass_cache=true", tbBase, id)
	body, err := t.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("torbox mylist failed")
	}
	var one tbTorrent
	if err := json.Unmarshal(resp.Data, &one); err == nil && (one.Hash != "" || one.Files != nil) {
		return &one, nil
	}
	var many []tbTorrent
	if err := json.Unmarshal(resp.Data, &many); err == nil && len(many) > 0 {
		return &many[0], nil
	}
	return nil, fmt.Errorf("torbox mylist: torrent %d not found", id)
}

func (t *torbox) Release(ctx context.Context, handle string) error {
	id, err := strconv.Atoi(handle)
	if err != nil {
		return nil
	}
	payload := fmt.Sprintf(`{"torrent_id":%d,"operation":"delete"}`, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tbBase+"/torrents/controltorrent", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	req.Header.Set("Content-Type", "application/json")
	_, err = t.do(req)
	return err
}

type tbCache struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type tbListItem struct {
	Hash             string `json:"hash"`
	Name             string `json:"name"`
	Size             int64  `json:"size"`
	DownloadFinished bool   `json:"download_finished"`
	DownloadPresent  bool   `json:"download_present"`
}

func TBLibrary(ctx context.Context, tbKey string) ([]LibraryItem, error) {
	t := newTorbox(tbKey)
	var out []LibraryItem
	for offset := 0; ; offset += 1000 {
		u := fmt.Sprintf("%s/torrents/mylist?offset=%d&limit=%d", tbBase, offset, 1000)
		body, err := t.get(ctx, u)
		if err != nil {
			return out, err
		}
		var resp struct {
			Success bool         `json:"success"`
			Data    []tbListItem `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return out, err
		}
		for _, it := range resp.Data {
			if it.Hash != "" && it.DownloadFinished && it.DownloadPresent {
				out = append(out, LibraryItem{Hash: strings.ToLower(it.Hash), Filename: it.Name, Size: it.Size})
			}
		}
		if len(resp.Data) < 1000 {
			return out, nil
		}
	}
}

func TorBoxCached(ctx context.Context, tbKey string, hashes []string) map[string]bool {
	t := newTorbox(tbKey)
	u := tbBase + "/torrents/checkcached?hash=" + url.QueryEscape(strings.Join(hashes, ",")) + "&format=list"
	body, err := t.get(ctx, u)
	if err != nil {
		return nil
	}
	var resp struct {
		Success bool         `json:"success"`
		Data    []tbListItem `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return nil
	}
	out := make(map[string]bool, len(resp.Data))
	for _, d := range resp.Data {
		if d.Hash != "" {
			out[strings.ToLower(d.Hash)] = true
		}
	}
	return out
}

func (t *torbox) cached(ctx context.Context, hash string) (*tbCache, error) {
	u := tbBase + "/torrents/checkcached?hash=" + url.QueryEscape(hash) + "&format=list"
	body, err := t.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Success bool      `json:"success"`
		Data    []tbCache `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if !resp.Success || len(resp.Data) == 0 {
		return nil, nil
	}
	return &resp.Data[0], nil
}

func (t *torbox) createTorrent(ctx context.Context, magnet string) (int, error) {
	form := url.Values{"magnet": {magnet}, "add_only_if_cached": {"true"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tbBase+"/torrents/createtorrent", strings.NewReader(form.Encode()))
	req.Header.Set("Authorization", "Bearer "+t.key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := t.do(req)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			TorrentID int `json:"torrent_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("torbox create torrent failed")
	}
	return resp.Data.TorrentID, nil
}

func (t *torbox) requestLink(ctx context.Context, id, fileID int) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		u := fmt.Sprintf("%s/torrents/requestdl?torrent_id=%d&file_id=%d", tbBase, id, fileID)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("Authorization", "Bearer "+t.key)
		body, err := t.do(req)
		if err != nil {
			lastErr = err
			continue
		}
		var resp struct {
			Success bool   `json:"success"`
			Data    string `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil || !resp.Success {
			lastErr = fmt.Errorf("torbox request dl failed")
			continue
		}
		head, _ := http.NewRequestWithContext(ctx, http.MethodHead, resp.Data, nil)
		hr, err := t.http.Do(head)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		hr.Body.Close()
		if hr.StatusCode >= 400 {
			lastErr = fmt.Errorf("torbox cdn returned %d", hr.StatusCode)
			time.Sleep(2 * time.Second)
			continue
		}
		return resp.Data, nil
	}
	return "", fmt.Errorf("torbox cdn unreachable: %w", lastErr)
}

func (t *torbox) do(req *http.Request) ([]byte, error) {
	waitLimits(req.Context(), t.limiters)
	status, body, err := breaker.RoundTrip("torbox", t.http, req)
	if err != nil {
		return nil, t.redact(err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("torbox HTTP %d: %s", status, body)
	}
	return body, nil
}

func (t *torbox) redact(err error) error {
	if t.key == "" {
		return err
	}
	if msg := err.Error(); strings.Contains(msg, t.key) {
		return errors.New(strings.ReplaceAll(msg, t.key, "REDACTED"))
	}
	return err
}
