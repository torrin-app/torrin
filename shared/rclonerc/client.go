package rclonerc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

type Error struct {
	Method string
	Status int
	Msg    string
}

func (e *Error) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("rclone rc %s: HTTP %d", e.Method, e.Status)
	}
	return fmt.Sprintf("rclone rc %s: %s", e.Method, e.Msg)
}

func (e *Error) Permanent() bool { return e.Status >= 400 && e.Status < 500 }

func (e *Error) Auth() bool {
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return true
	}
	return strings.Contains(e.Msg, "401") || strings.Contains(e.Msg, "403") ||
		strings.Contains(e.Msg, "Unauthorized") || strings.Contains(e.Msg, "Forbidden")
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 0}}
}

func (c *Client) CheckAccess(ctx context.Context, fs string) error {
	_, err := c.call(ctx, "operations/list", map[string]any{"fs": fs, "remote": ""})
	return err
}

func (c *Client) call(ctx context.Context, method string, in map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(data) > 0 {
		json.Unmarshal(data, &out)
	}
	if resp.StatusCode != http.StatusOK {
		msg := ""
		if out != nil {
			if e, ok := out["error"].(string); ok {
				msg = e
			}
		}
		return out, &Error{Method: method, Status: resp.StatusCode, Msg: msg}
	}
	return out, nil
}

func (c *Client) VFSRefresh(ctx context.Context, dir string) error {
	_, err := c.call(ctx, "vfs/refresh", map[string]any{"dir": dir})
	return err
}

func (c *Client) Noop(ctx context.Context) error {
	_, err := c.call(ctx, "rc/noop", map[string]any{})
	return err
}

func (c *Client) WaitReady(ctx context.Context) error {
	for {
		if err := c.Noop(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Client) CreateRemote(ctx context.Context, name, backend string, params map[string]string, obscure bool) error {
	_, err := c.call(ctx, "config/create", map[string]any{
		"name":       name,
		"type":       backend,
		"parameters": params,
		"opt":        map[string]any{"obscure": obscure, "nonInteractive": true},
	})
	return err
}

func (c *Client) Purge(ctx context.Context, fs, remote string) error {
	_, err := c.call(ctx, "operations/purge", map[string]any{"fs": fs, "remote": remote})
	return err
}

func UserRemoteName(userID string) string {
	id := userID
	if len(id) > 16 {
		id = id[:16]
	}
	return "u_" + id
}

func (c *Client) EnsureUserRemote(ctx context.Context, userID, backend string, params map[string]string, obscure bool, cryptPassword, basePath string) (string, error) {
	remote := UserRemoteName(userID)
	if cryptPassword == "" {
		return remote, c.CreateRemote(ctx, remote, backend, params, obscure)
	}
	src := remote + "_src"
	if err := c.CreateRemote(ctx, src, backend, params, obscure); err != nil {
		return "", err
	}
	crypt := map[string]string{
		"remote":                    src + ":" + basePath,
		"password":                  cryptPassword,
		"filename_encryption":       "standard",
		"directory_name_encryption": "true",
	}
	if err := c.CreateRemote(ctx, remote, "crypt", crypt, true); err != nil {
		return "", err
	}
	return remote, nil
}
