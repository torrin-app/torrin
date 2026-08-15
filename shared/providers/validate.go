package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var errBadKey = fmt.Errorf("that key is invalid or expired")

func validateProvider(ctx context.Context, url, key string, client *http.Client, check func(status int, body []byte) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the provider, try again")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return check(resp.StatusCode, body)
}

func ValidateRD(ctx context.Context, key string) error {
	return validateProvider(ctx, rdBase+"/user", key, http.DefaultClient, func(status int, body []byte) error {
		if status < 400 {
			return nil
		}
		var r struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &r) == nil && r.Error != "" {
			return fmt.Errorf("%s: %s", "Real-Debrid", r.Error)
		}
		return errBadKey
	})
}

func ValidateTB(ctx context.Context, key string) error {
	return validateProvider(ctx, tbBase+"/user/me", key, http.DefaultClient, func(status int, body []byte) error {
		var r struct {
			Success bool   `json:"success"`
			Detail  string `json:"detail"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(body, &r) != nil {
			if status < 400 {
				return nil
			}
			return errBadKey
		}
		if r.Success {
			return nil
		}
		if r.Detail != "" {
			return fmt.Errorf("%s: %s", "TorBox", r.Detail)
		}
		if r.Error != "" {
			return fmt.Errorf("%s: %s", "TorBox", r.Error)
		}
		return errBadKey
	})
}

func ValidatePM(ctx context.Context, key string) error {
	return validateProvider(ctx, pmBase+"/api/account/info", key, http.DefaultClient, func(status int, body []byte) error {
		var r struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &r) != nil {
			return errBadKey
		}
		if r.Status == "success" {
			return nil
		}
		if r.Message != "" {
			return fmt.Errorf("%s: %s", "Premiumize", r.Message)
		}
		return errBadKey
	})
}

func ValidateAD(ctx context.Context, key string) error {
	return validateProvider(ctx, adBase+"/v4/user?agent=torrin", key, adHTTPClient(10*time.Second), func(status int, body []byte) error {
		var r struct {
			Status string `json:"status"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &r) != nil {
			return errBadKey
		}
		if r.Status == "success" {
			return nil
		}
		if r.Error != nil && r.Error.Message != "" {
			return fmt.Errorf("%s: %s", "AllDebrid", r.Error.Message)
		}
		return errBadKey
	})
}

func ValidateOC(ctx context.Context, key string) error {
	return validateProvider(ctx, ocBase+"/api/account/info", key, http.DefaultClient, func(status int, body []byte) error {
		var r struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &r) == nil && r.Error != "" {
			return fmt.Errorf("%s: %s", "Offcloud", r.Error)
		}
		if status < 400 {
			return nil
		}
		return errBadKey
	})
}
