package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/crypto"
)

var ErrNotFound = errors.New("storage: not found")

type Client struct {
	b              backend
	publicURL      string
	signingKey     []byte
	nodeBases      map[string]string
	rcloneURL      string
	rcloneNodeDirs map[string]string
	hc             *http.Client
	manifestCipher *crypto.Cipher
}

func NewClient(endpoint, region, accessKey, secretKey, bucket, publicURL, signingKey string) *Client {
	return &Client{
		b:          newS3Backend(endpoint, region, accessKey, secretKey, bucket),
		publicURL:  strings.TrimRight(publicURL, "/"),
		signingKey: []byte(signingKey),
	}
}

func NewFSClient(baseDir, publicURL, signingKey string) *Client {
	return &Client{
		b:          &fsBackend{baseDir: baseDir},
		publicURL:  strings.TrimRight(publicURL, "/"),
		signingKey: []byte(signingKey),
	}
}

func (c *Client) SetNodeBases(m map[string]string) { c.nodeBases = m }

func (c *Client) SetStorageKey(hexKey string) error {
	cipher, err := crypto.New(hexKey)
	if err != nil {
		return err
	}
	c.manifestCipher = cipher
	return nil
}

func isManifestKey(key string) bool { return strings.HasSuffix(key, "/manifest.json") }

func StreamHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
	}}
}

func (c *Client) SetRcloneCache(rcloneURL string) {
	c.rcloneURL = strings.TrimRight(rcloneURL, "/")
	c.hc = StreamHTTPClient()
}

func (c *Client) SetRcloneCacheNodeDirs(m map[string]string) { c.rcloneNodeDirs = m }

func (c *Client) rclonePath(node, key string) string {
	if dir := c.rcloneNodeDirs[node]; dir != "" {
		return dir + "/" + key
	}
	return key
}

func (c *Client) baseFor(node string) string {
	if node != "" {
		if b := c.nodeBases[node]; b != "" {
			return b
		}
	}
	return c.publicURL
}

type Object struct {
	Body         io.ReadCloser
	Size         int64
	ContentType  string
	ContentRange string
}

type ObjMeta struct {
	Key     string
	Size    int64
	ModTime time.Time
}

func (c *Client) Get(ctx context.Context, key, rng string) (*Object, error) {
	return c.GetNode(ctx, "", key, rng)
}

func (c *Client) GetNode(ctx context.Context, node, key, rng string) (*Object, error) {
	if c.rcloneURL != "" {
		o, err := c.rcloneGet(ctx, c.rclonePath(node, key), rng)
		if err == nil || c.b == nil {
			return o, err
		}
	}
	return c.b.get(ctx, key, rng)
}

func (c *Client) GetBytes(ctx context.Context, key string) ([]byte, error) {
	return c.GetBytesNode(ctx, "", key)
}

func (c *Client) GetBytesNode(ctx context.Context, node, key string) ([]byte, error) {
	o, err := c.GetNode(ctx, node, key, "")
	if err != nil {
		return nil, err
	}
	defer o.Body.Close()
	data, err := io.ReadAll(o.Body)
	if err != nil {
		return nil, err
	}
	if c.manifestCipher != nil && isManifestKey(key) {
		return []byte(c.manifestCipher.Decrypt(string(data))), nil
	}
	return data, nil
}

func (c *Client) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	o, err := c.Get(ctx, key, "")
	if err != nil {
		return nil, err
	}
	return o.Body, nil
}

func (c *Client) Head(ctx context.Context, key string) (*Object, error) {
	return c.HeadNode(ctx, "", key)
}

func (c *Client) HeadNode(ctx context.Context, node, key string) (*Object, error) {
	if c.rcloneURL != "" {
		o, err := c.rcloneHead(ctx, c.rclonePath(node, key))
		if err == nil || c.b == nil {
			return o, err
		}
	}
	return c.b.head(ctx, key)
}

func (c *Client) Has(ctx context.Context, key string) (bool, error) {
	return c.b.has(ctx, key)
}

func (c *Client) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	if c.manifestCipher != nil && isManifestKey(key) {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		body = strings.NewReader(c.manifestCipher.Encrypt(string(data)))
	}
	return c.b.put(ctx, key, body, contentType)
}

func (c *Client) StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error {
	return c.b.put(ctx, key, body, contentType)
}

func (c *Client) PutSized(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	return c.b.putSized(ctx, key, body, contentType, size)
}

func (c *Client) Delete(ctx context.Context, key string) error {
	return c.b.delete(ctx, key)
}

func (c *Client) DeletePrefix(ctx context.Context, prefix string) error {
	return c.b.deletePrefix(ctx, prefix)
}

func (c *Client) List(ctx context.Context, prefix string) ([]ObjMeta, error) {
	return c.b.list(ctx, prefix)
}

func (c *Client) TestWrite(ctx context.Context) error {
	key := ".torrin-write-test"
	if err := c.Put(ctx, key, strings.NewReader("ok"), "text/plain"); err != nil {
		return err
	}
	return c.Delete(ctx, key)
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

func (c *Client) rcloneGet(ctx context.Context, key, rng string) (*Object, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rcloneURL+"/"+escapePath(key), nil)
	if err != nil {
		return nil, err
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("rclone get %s: %d", key, resp.StatusCode)
	}
	return &Object{
		Body:         resp.Body,
		Size:         resp.ContentLength,
		ContentType:  resp.Header.Get("Content-Type"),
		ContentRange: resp.Header.Get("Content-Range"),
	}, nil
}

func (c *Client) rcloneHead(ctx context.Context, key string) (*Object, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.rcloneURL+"/"+escapePath(key), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rclone head %s: %d", key, resp.StatusCode)
	}
	return &Object{
		Size:        resp.ContentLength,
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
}
