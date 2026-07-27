package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type backend interface {
	get(ctx context.Context, key, rng string) (*Object, error)
	head(ctx context.Context, key string) (*Object, error)
	has(ctx context.Context, key string) (bool, error)
	put(ctx context.Context, key string, body io.Reader, contentType string) error
	delete(ctx context.Context, key string) error
	deletePrefix(ctx context.Context, prefix string) error
}

type fsBackend struct{ baseDir string }

func (b *fsBackend) objPath(key string) string {
	clean := filepath.Clean("/" + strings.TrimPrefix(key, "/"))
	return filepath.Join(b.baseDir, clean)
}

func (b *fsBackend) get(_ context.Context, key, rng string) (*Object, error) {
	p := b.objPath(key)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	size := st.Size()
	ct := ctypeFor(key, p)
	if rng == "" {
		return &Object{Body: f, Size: size, ContentType: ct}, nil
	}
	start, end, ok := parseRange(rng, size)
	if !ok {
		return &Object{Body: f, Size: size, ContentType: ct}, nil
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return &Object{
		Body:         &limitedFile{f: f, remaining: end - start + 1},
		Size:         end - start + 1,
		ContentType:  ct,
		ContentRange: fmt.Sprintf("bytes %d-%d/%d", start, end, size),
	}, nil
}

func (b *fsBackend) head(_ context.Context, key string) (*Object, error) {
	p := b.objPath(key)
	st, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Object{Size: st.Size(), ContentType: ctypeFor(key, p)}, nil
}

func (b *fsBackend) has(_ context.Context, key string) (bool, error) {
	if _, err := os.Stat(b.objPath(key)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *fsBackend) put(_ context.Context, key string, body io.Reader, contentType string) error {
	p := b.objPath(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return err
	}
	if strings.HasPrefix(key, "blobs/") && contentType != "" {
		os.WriteFile(p+ctypeSidecarSuffix, []byte(contentType), 0o644)
	}
	return nil
}

func (b *fsBackend) delete(_ context.Context, key string) error {
	p := b.objPath(key)
	os.Remove(p + ctypeSidecarSuffix)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *fsBackend) deletePrefix(_ context.Context, prefix string) error {
	return os.RemoveAll(b.objPath(strings.TrimSuffix(prefix, "/")))
}

type s3Backend struct {
	s3     *s3.Client
	bucket string
}

func newS3Backend(endpoint, region, accessKey, secretKey, bucket string) *s3Backend {
	if region == "" {
		region = "garage"
	}
	ep := endpoint
	return &s3Backend{
		s3: s3.New(s3.Options{
			Region:       region,
			BaseEndpoint: &ep,
			Credentials:  credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
			UsePathStyle: true,
		}),
		bucket: bucket,
	}
}

func (b *s3Backend) get(ctx context.Context, key, rng string) (*Object, error) {
	in := &s3.GetObjectInput{Bucket: &b.bucket, Key: &key}
	if rng != "" {
		in.Range = &rng
	}
	out, err := b.s3.GetObject(ctx, in)
	if err != nil {
		return nil, s3err(err)
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

func (b *s3Backend) head(ctx context.Context, key string) (*Object, error) {
	out, err := b.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &b.bucket, Key: &key})
	if err != nil {
		return nil, s3err(err)
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

func (b *s3Backend) has(ctx context.Context, key string) (bool, error) {
	if _, err := b.head(ctx, key); err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *s3Backend) put(ctx context.Context, key string, body io.Reader, contentType string) error {
	up := manager.NewUploader(b.s3, func(u *manager.Uploader) {
		u.PartSize = 32 * 1024 * 1024
		u.Concurrency = 8
		u.LeavePartsOnError = false
	})
	_, err := up.Upload(ctx, &s3.PutObjectInput{
		Bucket: &b.bucket, Key: &key, Body: body, ContentType: &contentType,
	})
	return err
}

func (b *s3Backend) delete(ctx context.Context, key string) error {
	_, err := b.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.bucket, Key: &key})
	return err
}

func (b *s3Backend) deletePrefix(ctx context.Context, prefix string) error {
	var token *string
	for {
		list, err := b.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &b.bucket, Prefix: &prefix, ContinuationToken: token,
		})
		if err != nil {
			return err
		}
		for _, obj := range list.Contents {
			b.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.bucket, Key: obj.Key})
		}
		if list.IsTruncated == nil || !*list.IsTruncated {
			return nil
		}
		token = list.NextContinuationToken
	}
}

func s3err(err error) error {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return ErrNotFound
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return ErrNotFound
		}
	}
	return err
}
