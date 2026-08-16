package rclonerc

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) ReadFile(ctx context.Context, remote, path string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/["+remote+":]/"+escapePath(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, &Error{Method: "read", Status: resp.StatusCode}
	}
	return resp.Body, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
