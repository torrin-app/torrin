package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type Client struct {
	s3         *s3.Client
	bucket     string
	publicURL  string
	signingKey []byte
	nodeBases  map[string]string
}

func (c *Client) SetNodeBases(m map[string]string) { c.nodeBases = m }

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
	return io.ReadAll(o.Body)
}

func (c *Client) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	o, err := c.Get(ctx, key, "")
	if err != nil {
		return nil, err
	}
	return o.Body, nil
}

func (c *Client) Head(ctx context.Context, key string) (*Object, error) {
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
