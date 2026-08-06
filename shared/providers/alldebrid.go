package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

var adBase = "https://api.alldebrid.com"

type alldebrid struct {
	key      string
	http     *http.Client
	limiters *keyLimiter
}

func newAlldebrid(key string) *alldebrid {
	return &alldebrid{
		key:      key,
		http:     adHTTPClient(30 * time.Second),
		limiters: sharedLimiters("ad:"+key, limitSpec{12, time.Second}, limitSpec{600, time.Minute}),
	}
}

func adHTTPClient(timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if p := os.Getenv("AD_PROXY_URL"); p != "" {
		if u, err := url.Parse(p); err == nil {
			c.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	return c
}

func NewAllDebrid(apiKey string) Provider { return newAlldebrid(apiKey) }

func (a *alldebrid) Name() string { return "alldebrid" }

func (a *alldebrid) Fetch(ctx context.Context, magnet, infoHash string) (*Result, error) {
	added, err := a.addMagnet(ctx, magnet)
	if err != nil {
		return nil, err
	}
	handle := strconv.Itoa(added.ID)

	if !added.Ready {
		a.Release(context.Background(), handle)
		return nil, nil
	}

	files, err := a.files(ctx, added.ID)
	if err != nil {
		a.Release(context.Background(), handle)
		return nil, err
	}

	var links []Link
	for _, f := range files {
		if !isVideoFile(f.Name) {
			continue
		}
		u, err := a.unlock(ctx, f.Link)
		if err != nil || u.Link == "" {
			continue
		}
		links = append(links, Link{Name: u.Filename, Size: u.FileSize, URL: u.Link})
	}
	if len(links) == 0 {
		a.Release(context.Background(), handle)
		return nil, nil
	}

	name := added.Name
	if name == "" {
		name = links[0].Name
	}
	return &Result{Name: name, Handle: handle, Files: links}, nil
}

func (a *alldebrid) Release(ctx context.Context, handle string) error {
	if handle == "" {
		return nil
	}
	_, err := a.do(ctx, "/v4/magnet/delete", url.Values{"id": {handle}})
	return err
}

type adMagnet struct {
	ID    int    `json:"id"`
	Ready bool   `json:"ready"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Hash  string `json:"hash"`
}

type ADMagnetItem struct {
	ID         int    `json:"id"`
	Hash       string `json:"hash"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	Status     string `json:"status"`
	StatusCode int    `json:"statusCode"`
	UploadDate int64  `json:"uploadDate"`
}

func ADListMagnets(ctx context.Context, adKey, status string) ([]ADMagnetItem, error) {
	a := newAlldebrid(adKey)
	form := url.Values{}
	if status != "" {
		form.Set("status", status)
	}
	body, err := a.do(ctx, "/v4.1/magnet/status", form)
	if err != nil {
		return nil, err
	}
	var data struct {
		Magnets []ADMagnetItem `json:"magnets"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data.Magnets, nil
}

func ADLibrary(ctx context.Context, adKey string) ([]LibraryItem, error) {
	magnets, err := ADListMagnets(ctx, adKey, "ready")
	if err != nil {
		return nil, err
	}
	out := make([]LibraryItem, 0, len(magnets))
	for _, m := range magnets {
		if m.Hash == "" {
			continue
		}
		out = append(out, LibraryItem{Hash: m.Hash, Filename: m.Filename, Size: m.Size})
	}
	return out, nil
}

func ADReapOrphans(ctx context.Context, adKey string) (int, error) {
	mags, err := ADListMagnets(ctx, adKey, "")
	if err != nil {
		return 0, err
	}
	a := newAlldebrid(adKey)
	now := time.Now().Unix()
	deleted := 0
	for _, m := range mags {
		dead := m.StatusCode >= 5
		readyStale := m.StatusCode == 4 && m.UploadDate > 0 && now-m.UploadDate > 20*60
		if dead || readyStale {
			if _, err := a.do(ctx, "/v4/magnet/delete", url.Values{"id": {strconv.Itoa(m.ID)}}); err == nil {
				deleted++
			}
		}
	}
	return deleted, nil
}

func (a *alldebrid) addMagnet(ctx context.Context, magnet string) (*adMagnet, error) {
	body, err := a.do(ctx, "/v4/magnet/upload", url.Values{"magnets[]": {magnet}})
	if err != nil {
		return nil, err
	}
	var data struct {
		Magnets []adMagnet `json:"magnets"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if len(data.Magnets) == 0 {
		return nil, fmt.Errorf("alldebrid: no magnet result")
	}
	return &data.Magnets[0], nil
}

type adFile struct {
	Name string   `json:"n"`
	Size int64    `json:"s"`
	Link string   `json:"l"`
	Sub  []adFile `json:"e"`
}

func (a *alldebrid) files(ctx context.Context, id int) ([]adFile, error) {
	body, err := a.do(ctx, "/v4/magnet/files", url.Values{"id[]": {strconv.Itoa(id)}})
	if err != nil {
		return nil, err
	}
	var data struct {
		Magnets []struct {
			Files []adFile `json:"files"`
		} `json:"magnets"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if len(data.Magnets) == 0 {
		return nil, fmt.Errorf("alldebrid: no files result")
	}
	return flattenAD(data.Magnets[0].Files), nil
}

func flattenAD(files []adFile) []adFile {
	var out []adFile
	for _, f := range files {
		if f.Link != "" {
			out = append(out, f)
		}
		out = append(out, flattenAD(f.Sub)...)
	}
	return out
}

type adUnlock struct {
	Link     string `json:"link"`
	Filename string `json:"filename"`
	FileSize int64  `json:"filesize"`
	Delayed  int    `json:"delayed"`
}

const (
	delayedPollInterval = 5 * time.Second
	delayedMaxPolls     = 24
)

func HosterUnlock(ctx context.Context, adKey, link string) (name, dl string, size int64, err error) {
	u, err := newAlldebrid(adKey).unlock(ctx, link)
	if err != nil {
		return "", "", 0, hosterFailure(err)
	}
	return u.Filename, u.Link, u.FileSize, nil
}

func (a *alldebrid) unlock(ctx context.Context, link string) (*adUnlock, error) {
	body, err := a.do(ctx, "/v4/link/unlock", url.Values{"link": {link}})
	if err != nil {
		return nil, err
	}
	var u adUnlock
	if err := json.Unmarshal(body, &u); err != nil {
		return nil, err
	}
	if u.Link == "" && u.Delayed > 0 {
		return a.pollDelayed(ctx, u.Delayed, &u)
	}
	return &u, nil
}

func (a *alldebrid) pollDelayed(ctx context.Context, id int, u *adUnlock) (*adUnlock, error) {
	for i := 0; i < delayedMaxPolls; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delayedPollInterval):
		}
		body, err := a.do(ctx, "/v4/link/delayed", url.Values{"id": {strconv.Itoa(id)}})
		if err != nil {
			return nil, err
		}
		var d struct {
			Status   int    `json:"status"`
			Link     string `json:"link"`
			Filename string `json:"filename"`
			Filesize int64  `json:"filesize"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			return nil, err
		}
		switch d.Status {
		case 2:
			if d.Link != "" {
				u.Link = d.Link
			}
			if d.Filename != "" {
				u.Filename = d.Filename
			}
			if d.Filesize > 0 {
				u.FileSize = d.Filesize
			}
			return u, nil
		case 3:
			return nil, fmt.Errorf("alldebrid delayed link failed")
		}
	}
	return nil, fmt.Errorf("alldebrid delayed link timed out")
}

func (a *alldebrid) do(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	waitLimits(ctx, a.limiters)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adBase+path, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = form.Encode()
	req.Header.Set("Authorization", "Bearer "+a.key)

	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		if r.Error != nil {
			return nil, &adError{code: r.Error.Code, message: r.Error.Message}
		}
		return nil, fmt.Errorf("alldebrid: %s", body)
	}
	return r.Data, nil
}
