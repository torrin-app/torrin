package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrTorBoxUsenetUnavailable = errors.New("torbox: usenet unavailable for this account")

const usenetPollInterval = 5 * time.Second

type tbUsenetStatus struct {
	Name            string   `json:"name"`
	DownloadState   string   `json:"download_state"`
	Progress        float64  `json:"progress"`
	Size            int64    `json:"size"`
	DownloadPresent bool     `json:"download_present"`
	Files           []tbFile `json:"files"`
}

// TorBoxUsenetByID builds download links for an EXISTING completed usenet
// download in the user's TorBox (library import) — no NZB, no create/await.
func TorBoxUsenetByID(ctx context.Context, key string, id int) (*Result, error) {
	t := newTorbox(key)
	st, err := t.usenetStatus(ctx, id)
	if err != nil {
		return nil, err
	}
	if !st.DownloadPresent {
		return nil, fmt.Errorf("%w: download %d not ready", ErrTorBoxUsenetUnavailable, id)
	}
	var files []Link
	for _, f := range st.Files {
		if !isVideoFile(f.Name) {
			continue
		}
		dlURL, err := t.requestUsenetLink(ctx, id, f.ID)
		if err != nil {
			continue
		}
		files = append(files, Link{Name: filepath.Base(f.Name), Size: f.Size, URL: dlURL})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("torbox usenet: no video files in release")
	}
	name := st.Name
	if name == "" {
		name = strconv.Itoa(id)
	}
	return &Result{Name: name, Handle: strconv.Itoa(id), Files: files}, nil
}

func TorBoxUsenet(ctx context.Context, key, name string, nzbData []byte, onProgress func(done, total int64)) (*Result, error) {
	t := newTorbox(key)
	id, err := t.createUsenet(ctx, name, nzbData)
	if err != nil {
		return nil, err
	}
	st, err := t.awaitUsenet(ctx, id, onProgress)
	if err != nil {
		return nil, err
	}
	var files []Link
	for _, f := range st.Files {
		if !isVideoFile(f.Name) {
			continue
		}
		dlURL, err := t.requestUsenetLink(ctx, id, f.ID)
		if err != nil {
			continue
		}
		files = append(files, Link{Name: filepath.Base(f.Name), Size: f.Size, URL: dlURL})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("torbox usenet: no video files in release")
	}
	return &Result{Name: name, Handle: strconv.Itoa(id), Files: files}, nil
}

func ReleaseTorBoxUsenet(ctx context.Context, key, handle string) {
	id, err := strconv.Atoi(handle)
	if err != nil {
		return
	}
	t := newTorbox(key)
	payload := fmt.Sprintf(`{"usenet_id":%d,"operation":"delete"}`, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tbBase+"/usenet/controlusenetdownload", strings.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	req.Header.Set("Content-Type", "application/json")
	t.do(req)
}

func (t *torbox) createUsenet(ctx context.Context, name string, nzbData []byte) (int, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", name+".nzb")
	if err != nil {
		return 0, err
	}
	if _, err := fw.Write(nzbData); err != nil {
		return 0, err
	}
	w.WriteField("name", name)
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tbBase+"/usenet/createusenetdownload", &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	body, err := t.do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrTorBoxUsenetUnavailable, err)
	}
	var resp struct {
		Success bool   `json:"success"`
		Detail  string `json:"detail"`
		Data    struct {
			UsenetDownloadID int `json:"usenetdownload_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if !resp.Success || resp.Data.UsenetDownloadID == 0 {
		return 0, fmt.Errorf("%w: %s", ErrTorBoxUsenetUnavailable, resp.Detail)
	}
	return resp.Data.UsenetDownloadID, nil
}

func (t *torbox) awaitUsenet(ctx context.Context, id int, onProgress func(done, total int64)) (*tbUsenetStatus, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(usenetPollInterval):
		}
		st, err := t.usenetStatus(ctx, id)
		if err != nil {
			return nil, err
		}
		if s := strings.ToLower(st.DownloadState); strings.Contains(s, "fail") || strings.Contains(s, "error") {
			return nil, fmt.Errorf("torbox usenet download %s", st.DownloadState)
		}
		if onProgress != nil && st.Size > 0 {
			onProgress(int64(st.Progress*float64(st.Size)), st.Size)
		}
		if st.DownloadPresent {
			return st, nil
		}
	}
}

func (t *torbox) usenetStatus(ctx context.Context, id int) (*tbUsenetStatus, error) {
	body, err := t.get(ctx, fmt.Sprintf("%s/usenet/mylist?id=%d&bypass_cache=true", tbBase, id))
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
	var one tbUsenetStatus
	if err := json.Unmarshal(resp.Data, &one); err == nil && (one.DownloadState != "" || one.Files != nil) {
		return &one, nil
	}
	return nil, fmt.Errorf("torbox usenet status: download %d not found", id)
}

func (t *torbox) requestUsenetLink(ctx context.Context, id, fileID int) (string, error) {
	u := fmt.Sprintf("%s/usenet/requestdl?token=%s&usenet_id=%d&file_id=%d", tbBase, url.QueryEscape(t.key), id, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+t.key)
	body, err := t.do(req)
	if err != nil {
		return "", err
	}
	var resp struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || !resp.Success || resp.Data == "" {
		return "", fmt.Errorf("torbox usenet requestdl failed")
	}
	return resp.Data, nil
}
