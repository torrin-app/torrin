package qbit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

type Torrent struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	Progress    float64 `json:"progress"`
	DlSpeed     int64   `json:"dlspeed"`
	State       string  `json:"state"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
	Category    string  `json:"category"`
	ETA         int64   `json:"eta"`
	Ratio       float64 `json:"ratio"`
	SeedingTime int64   `json:"seeding_time"`
	Uploaded    int64   `json:"uploaded"`
}

type TorrentFile struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Size     int64   `json:"size"`
	Priority int     `json:"priority"`
	Progress float64 `json:"progress"`
}

func NewClient(baseURL, username, password string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		http:     &http.Client{Jar: jar, Timeout: 30 * time.Second},
	}
}

func (c *Client) Login() error {
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/auth/login", url.Values{
		"username": {c.username},
		"password": {c.password},
	})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 204 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("login failed: %s (status %d)", body, resp.StatusCode)
}

func (c *Client) setPreferences(prefs map[string]any) error {
	data, _ := json.Marshal(prefs)
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/app/setPreferences", url.Values{"json": {string(data)}})
	if err != nil {
		return fmt.Errorf("set preferences: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set preferences (%d): %s", resp.StatusCode, body)
	}
	return nil
}

func (c *Client) SetAutoTrackers(trackers string) error {
	return c.setPreferences(map[string]any{
		"add_trackers_enabled": true,
		"add_trackers":         trackers,
	})
}

func (c *Client) ConfigureForSeeding() error {
	return c.setPreferences(map[string]any{
		"max_ratio_enabled":        false,
		"max_seeding_time_enabled": false,
		"max_active_torrents":      100,
		"max_active_uploads":       100,
		"max_uploads":              200,
		"max_uploads_per_torrent":  8,
		"dht":                      false,
		"pex":                      false,
		"lsd":                      false,
		"add_trackers_enabled":     false,
		"encryption":               0,
		"anonymous_mode":           false,
		"up_limit":                 0,
		"dl_limit":                 0,
		"async_io_threads":         32,
		"connection_speed":         100,
	})
}

func (c *Client) AddMagnet(magnet string) error {
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/add", url.Values{
		"urls":               {magnet},
		"savepath":           {"/downloads"},
		"category":           {"torrin"},
		"sequentialDownload": {"true"},
		"firstLastPiecePrio": {"true"},
		"stopCondition":      {"MetadataReceived"},
	})
	if err != nil {
		return fmt.Errorf("add magnet: %w", err)
	}
	return addResult(resp, "add magnet")
}

func (c *Client) AddTorrentFile(data []byte, category string) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("torrents", "t.torrent")
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	fields := map[string]string{
		"savepath":           "/downloads",
		"category":           category,
		"sequentialDownload": "true",
		"firstLastPiecePrio": "true",
		"ratioLimit":         "-1",
		"seedingTimeLimit":   "-1",
	}
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()
	resp, err := c.http.Post(c.baseURL+"/api/v2/torrents/add", w.FormDataContentType(), &body)
	if err != nil {
		return fmt.Errorf("add torrent file: %w", err)
	}
	return addResult(resp, "add torrent file")
}

func addResult(resp *http.Response, what string) error {
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 409 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s (%d): %s", what, resp.StatusCode, body)
}

func (c *Client) torrentsInfo(query string) ([]Torrent, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v2/torrents/info?" + query)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var torrents []Torrent
	return torrents, json.NewDecoder(resp.Body).Decode(&torrents)
}

func (c *Client) ListTorrents(category string) ([]Torrent, error) {
	return c.torrentsInfo("category=" + category)
}

func (c *Client) GetTorrent(hash string) (*Torrent, error) {
	torrents, err := c.torrentsInfo("hashes=" + strings.ToLower(hash))
	if err != nil {
		return nil, err
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("torrent %s not found", hash)
	}
	return &torrents[0], nil
}

func (c *Client) GetFiles(hash string) ([]TorrentFile, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v2/torrents/files?hash=" + strings.ToLower(hash))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var files []TorrentFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, err
	}
	for i := range files {
		files[i].Index = i
	}
	return files, nil
}

func (c *Client) Resume(hash string) error { return c.startStop(hash, "start", "resume") }
func (c *Client) Pause(hash string) error  { return c.startStop(hash, "stop", "pause") }

func (c *Client) startStop(hash, primary, fallback string) error {
	data := url.Values{"hashes": {strings.ToLower(hash)}}
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/"+primary, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		resp2, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/"+fallback, data)
		if err != nil {
			return err
		}
		resp2.Body.Close()
	}
	return nil
}

func (c *Client) Reannounce(hash string) error {
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/reannounce", url.Values{"hashes": {strings.ToLower(hash)}})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) SetFilePriority(hash string, fileIndexes []int, priority int) error {
	idx := make([]string, len(fileIndexes))
	for i, n := range fileIndexes {
		idx[i] = fmt.Sprintf("%d", n)
	}
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/filePrio", url.Values{
		"hash":     {strings.ToLower(hash)},
		"id":       {strings.Join(idx, "|")},
		"priority": {fmt.Sprintf("%d", priority)},
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) Delete(hash string) error {
	resp, err := c.http.PostForm(c.baseURL+"/api/v2/torrents/delete", url.Values{
		"hashes":      {strings.ToLower(hash)},
		"deleteFiles": {"true"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete (%d): %s", resp.StatusCode, body)
	}
	return nil
}

func IsStalled(t *Torrent) bool          { return t.State == "stalledDL" && t.DlSpeed == 0 }
func IsFetchingMetadata(t *Torrent) bool { return t.State == "metaDL" }
func IsQueued(t *Torrent) bool           { return t.State == "queuedDL" }
func IsError(t *Torrent) bool            { return t.State == "error" || t.State == "missingFiles" }

func IsComplete(t *Torrent) bool {
	switch t.State {
	case "uploading", "stalledUP", "pausedUP", "forcedUP", "checkingUP", "queuedUP", "stoppedUP":
		return true
	}
	return t.Progress >= 1.0
}
