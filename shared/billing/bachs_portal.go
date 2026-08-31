package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func (b *BachsHandler) CreatePortalSession(ctx context.Context, customerID string) (string, error) {
	if customerID == "" {
		return "", fmt.Errorf("no bachs customer for this account")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.apiURL+"/v1/customers/"+customerID+"/portal-sessions", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+b.secretKey)

	resp, err := b.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("bachs portal session: %d %s", resp.StatusCode, msg)
	}

	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("bachs portal session: empty url")
	}
	return out.URL, nil
}
