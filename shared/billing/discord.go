package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type Discord struct {
	url  string
	http *http.Client
}

func NewDiscord(webhookURL string) *Discord {
	if webhookURL == "" {
		return nil
	}
	return &Discord{url: webhookURL, http: &http.Client{Timeout: 10 * time.Second}}
}

func (d *Discord) Post(ctx context.Context, content string) {
	if d == nil {
		return
	}
	body, _ := json.Marshal(map[string]string{"content": content})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		slog.Warn("discord post failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		slog.Warn("discord post rejected", "status", resp.StatusCode)
	}
}
