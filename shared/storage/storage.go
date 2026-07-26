package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/torrin-app/torrin/shared/crypto"
)

type Client struct {
	s3             *s3.Client
	bucket         string
	publicURL      string
	signingKey     []byte
	nodeBases      map[string]string
	rcloneURL      string
	hc             *http.Client
	manifestCipher *crypto.Cipher
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

func (c *Client) SetRcloneCache(rcloneURL string) {
	c.rcloneURL = strings.TrimRight(rcloneURL, "/")
	c.hc = &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
	}}
}

func (c *Client) baseFor(node string) string {
	if node != "" {
		if b := c.nodeBases[node]; b != "" {
			return b
		}
	}
	return c.publicURL
}

func NewClient(endpoint, region, accessKey, secretKey, bucket, publicURL, signingKey string) *Client {
	if region == "" {
		region = "garage"
	}
	ep := endpoint
	return &Client{
		s3: s3.New(s3.Options{
			Region:       region,
			BaseEndpoint: &ep,
			Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
			UsePathStyle: true,
		}),
		bucket:     bucket,
		publicURL:  strings.TrimRight(publicURL, "/"),
		signingKey: []byte(signingKey),
	}
}

type Object struct {
	Body         io.ReadCloser
	Size         int64
	ContentType  string
	ContentRange string
}

func (c *Client) Get(ctx context.Context, key, rng string) (*Object, error) {
	if c.rcloneURL != "" {
		o, err := c.rcloneGet(ctx, key, rng)
		if err == nil || c.s3 == nil {
			return o, err
		}
		slog.Warn("rclone cache read failed, serving direct from origin", "key", key, "err", err)
	}
	return c.s3Get(ctx, key, rng)
}

func (c *Client) s3Get(ctx context.Context, key, rng string) (*Object, error) {
	in := &s3.GetObjectInput{Bucket: &c.bucket, Key: &key}
	if rng != "" {
		in.Range = &rng
	}
	out, err := c.s3.GetObject(ctx, in)
	if err != nil {
		return nil, err
	}
	o := &Object{Body: out.Body}
	if out.ContentLength != nil {
		o.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		o.ContentType = *out.ContentType
	}
	if out.ContentRange != nil {
		o.ContentRange = *out.ContentRange
	}
	return o, nil
}

func (c *Client) GetBytes(ctx context.Context, key string) ([]byte, error) {
	o, err := c.Get(ctx, key, "")
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

func (c *Client) Head(ctx context.Context, key string) (*Object, error) {
	if c.rcloneURL != "" {
		o, err := c.rcloneHead(ctx, key)
		if err == nil || c.s3 == nil {
			return o, err
		}
		slog.Warn("rclone cache head failed, serving direct from origin", "key", key, "err", err)
	}
	return c.s3Head(ctx, key)
}

func (c *Client) s3Head(ctx context.Context, key string) (*Object, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &c.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	o := &Object{}
	if out.ContentLength != nil {
		o.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		o.ContentType = *out.ContentType
	}
	return o, nil
}

func (c *Client) Has(ctx context.Context, key string) (bool, error) {
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &c.bucket, Key: &key})
	if err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	if c.manifestCipher != nil && isManifestKey(key) {
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		body = strings.NewReader(c.manifestCipher.Encrypt(string(data)))
	}
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &c.bucket, Key: &key, Body: body, ContentType: &contentType,
	})
	return err
}

func (c *Client) TestWrite(ctx context.Context) error {
	key := ".torrin-byos-test"
	if err := c.Put(ctx, key, strings.NewReader("ok"), "text/plain"); err != nil {
		return err
	}
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &c.bucket, Key: &key})
	return err
}

func (c *Client) StreamUpload(ctx context.Context, key string, body io.Reader, contentType string) error {
	up := manager.NewUploader(c.s3, func(u *manager.Uploader) {
		u.PartSize = 32 * 1024 * 1024
		u.Concurrency = 8
		u.LeavePartsOnError = false
	})
	_, err := up.Upload(ctx, &s3.PutObjectInput{
		Bucket: &c.bucket, Key: &key, Body: body, ContentType: &contentType,
	})
	return err
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &c.bucket, Key: &key})
	return err
}

func (c *Client) DeletePrefix(ctx context.Context, prefix string) error {
	var token *string
	for {
		list, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &c.bucket, Prefix: &prefix, ContinuationToken: token,
		})
		if err != nil {
			return err
		}
		var lastErr error
		for _, obj := range list.Contents {
			if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &c.bucket, Key: obj.Key}); err != nil {
				lastErr = err
				slog.Warn("delete object failed", "key", *obj.Key, "err", err)
			}
		}
		if list.IsTruncated == nil || !*list.IsTruncated {
			return lastErr
		}
		token = list.NextContinuationToken
	}
}

func (c *Client) Presign(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := s3.NewPresignClient(c.s3).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket, Key: &key,
	}, func(o *s3.PresignOptions) { o.Expires = expiry })
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	return false
}
