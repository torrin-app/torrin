package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

type Host struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Domains []string `json:"domains"`
	Up      *bool    `json:"up"`
}

func FetchHosts(ctx context.Context) ([]Host, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adBase+"/v4/hosts?agent=torrin", nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var r struct {
		Status string `json:"status"`
		Data   struct {
			Hosts map[string]struct {
				Name    string   `json:"name"`
				Type    string   `json:"type"`
				Domains []string `json:"domains"`
				Status  *bool    `json:"status"`
			} `json:"hosts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("alldebrid hosts: %s", body)
	}
	out := make([]Host, 0, len(r.Data.Hosts))
	for _, h := range r.Data.Hosts {
		out = append(out, Host{Name: h.Name, Type: h.Type, Domains: h.Domains, Up: h.Status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
