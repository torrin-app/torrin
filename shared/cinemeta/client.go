package cinemeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/useragent"
)

const base = "https://v3-cinemeta.strem.io"

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = safeurl.Dialer(false)
	return &Client{http: &http.Client{Timeout: 10 * time.Second, Transport: tr}}
}

func (c *Client) SeasonEpisodes(ctx context.Context, imdbID string, season int) (int, error) {
	id := "tt" + strings.TrimPrefix(imdbID, "tt")
	url := fmt.Sprintf("%s/meta/series/%s.json", base, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", useragent.Default)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("cinemeta status %d", resp.StatusCode)
	}
	var out struct {
		Meta struct {
			Videos []struct {
				Season  int `json:"season"`
				Episode int `json:"episode"`
			} `json:"videos"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	last := 0
	for _, v := range out.Meta.Videos {
		if v.Season == season && v.Episode > last {
			last = v.Episode
		}
	}
	return last, nil
}
