package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/storage"
)

type encStorage struct{ enc []byte }

func (e *encStorage) Verify(string, int64, string, string) bool        { return true }
func (e *encStorage) GetBytes(context.Context, string) ([]byte, error) { return nil, nil }
func (e *encStorage) GetBytesNode(context.Context, string, string) ([]byte, error) {
	return nil, nil
}
func (e *encStorage) Head(context.Context, string) (*storage.Object, error) {
	return &storage.Object{Size: int64(len(e.enc)), ContentType: "video/x-matroska"}, nil
}
func (e *encStorage) HeadNode(ctx context.Context, _, key string) (*storage.Object, error) {
	return e.Head(ctx, key)
}
func (e *encStorage) GetNode(ctx context.Context, _, key, rng string) (*storage.Object, error) {
	return e.Get(ctx, key, rng)
}
func (e *encStorage) Get(_ context.Context, _, rng string) (*storage.Object, error) {
	if rng == "" {
		return &storage.Object{Body: io.NopCloser(bytes.NewReader(e.enc)), Size: int64(len(e.enc))}, nil
	}
	spec := strings.TrimPrefix(rng, "bytes=")
	dash := strings.IndexByte(spec, '-')
	a, _ := strconv.ParseInt(spec[:dash], 10, 64)
	b, _ := strconv.ParseInt(spec[dash+1:], 10, 64)
	if b >= int64(len(e.enc)) {
		b = int64(len(e.enc)) - 1
	}
	slice := e.enc[a : b+1]
	return &storage.Object{
		Body:         io.NopCloser(bytes.NewReader(slice)),
		Size:         int64(len(slice)),
		ContentRange: fmt.Sprintf("bytes %d-%d/%d", a, b, len(e.enc)),
	}, nil
}

func encServer(t *testing.T, plain []byte) *Server {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	c, err := crypto.NewStream(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, _ := c.EncryptWriter(&buf)
	w.Write(plain)
	w.Close()
	return New(&encStorage{enc: buf.Bytes()}, "*", "", c)
}

func TestServeEncryptedFull(t *testing.T) {
	plain := make([]byte, 300*1024+123)
	rand.Read(plain)
	srv := encServer(t, plain)

	w := do(srv, http.MethodGet, "/blobs/b_x?expires=9999999999&sig=ok&enc=1", nil)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), plain) {
		t.Fatal("full decrypted body mismatch")
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(plain)) {
		t.Fatalf("content-length %s want %d", got, len(plain))
	}
}

func TestServeEncryptedRange(t *testing.T) {
	plain := make([]byte, 500*1024)
	rand.Read(plain)
	srv := encServer(t, plain)

	cases := [][2]int64{{0, 1023}, {100000, 200000}, {64 * 1024, 64*1024 + 10}, {499000, 499999}}
	for _, c := range cases {
		hdr := http.Header{"Range": []string{fmt.Sprintf("bytes=%d-%d", c[0], c[1])}}
		w := do(srv, http.MethodGet, "/blobs/b_x?expires=9999999999&sig=ok&enc=1", hdr)
		if w.Code != http.StatusPartialContent {
			t.Fatalf("range %v code %d", c, w.Code)
		}
		want := plain[c[0] : c[1]+1]
		if !bytes.Equal(w.Body.Bytes(), want) {
			t.Fatalf("range %v mismatch: got %d want %d", c, w.Body.Len(), len(want))
		}
		wantCR := fmt.Sprintf("bytes %d-%d/%d", c[0], c[1], len(plain))
		if got := w.Header().Get("Content-Range"); got != wantCR {
			t.Fatalf("content-range %q want %q", got, wantCR)
		}
	}
}

func TestServeEncryptedHead(t *testing.T) {
	plain := make([]byte, 250*1024)
	srv := encServer(t, plain)
	w := do(srv, http.MethodHead, "/blobs/b_x?expires=9999999999&sig=ok&enc=1", nil)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(plain)) {
		t.Fatalf("head content-length %s want %d (plaintext)", got, len(plain))
	}
}
