package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fsStore(t *testing.T) *Client {
	t.Helper()
	return NewFSClient(t.TempDir(), "https://x", "sign")
}

func put(t *testing.T, c *Client, key string, data []byte, ct string) {
	t.Helper()
	if err := c.Put(context.Background(), key, bytes.NewReader(data), ct); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
}

func TestFSRoundTrip(t *testing.T) {
	c := fsStore(t)
	data := []byte("hello world contents")
	put(t, c, "blobs/b_abc", data, "video/mp4")

	o, err := c.Get(context.Background(), "blobs/b_abc", "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(o.Body)
	o.Body.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("roundtrip mismatch: %q", got)
	}
	if o.Size != int64(len(data)) {
		t.Fatalf("size %d want %d", o.Size, len(data))
	}
	if o.ContentType != "video/mp4" {
		t.Fatalf("content-type %q want video/mp4 (sidecar)", o.ContentType)
	}
	fi, err := os.Stat(filepath.Join(c.b.(*fsBackend).baseDir, "blobs/b_abc"))
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("blob perms = %v (want 0644 so nonroot readers can open)", fi.Mode().Perm())
	}
}

func TestFSListSkipsSidecars(t *testing.T) {
	c := fsStore(t)
	put(t, c, "blobs/b_one", []byte("aaaa"), "video/mp4")
	put(t, c, "blobs/b_two", []byte("bbbbbb"), "video/x-matroska")

	objs, err := c.List(context.Background(), "blobs/")
	if err != nil {
		t.Fatal(err)
	}
	sizes := map[string]int64{}
	for _, o := range objs {
		sizes[o.Key] = o.Size
		if strings.HasSuffix(o.Key, ctypeSidecarSuffix) {
			t.Fatalf("list returned a ctype sidecar: %s", o.Key)
		}
	}
	if len(objs) != 2 {
		t.Fatalf("listed %d objects, want 2 (%v)", len(objs), sizes)
	}
	if sizes["blobs/b_one"] != 4 || sizes["blobs/b_two"] != 6 {
		t.Fatalf("wrong sizes: %v", sizes)
	}
}

func TestFSRange(t *testing.T) {
	c := fsStore(t)
	data := make([]byte, 100_000)
	rand.Read(data)
	put(t, c, "blobs/b_r", data, "video/x-matroska")

	cases := []struct{ start, end int64 }{{0, 0}, {0, 99999}, {500, 1500}, {99990, 99999}}
	for _, cs := range cases {
		rng := "bytes=" + itoa(cs.start) + "-" + itoa(cs.end)
		o, err := c.Get(context.Background(), "blobs/b_r", rng)
		if err != nil {
			t.Fatalf("range %v: %v", cs, err)
		}
		got, _ := io.ReadAll(o.Body)
		o.Body.Close()
		want := data[cs.start : cs.end+1]
		if !bytes.Equal(got, want) {
			t.Fatalf("range %v mismatch: got %d want %d", cs, len(got), len(want))
		}
		if o.ContentRange == "" {
			t.Fatalf("range %v: missing Content-Range", cs)
		}
	}
}

func TestFSManifestEncryptDualRead(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	enc := fsStore(t)
	if err := enc.SetStorageKey(hex.EncodeToString(key)); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"info_hash":"abc","files":[]}`)
	put(t, enc, "abc/manifest.json", manifest, "application/json")

	// on-disk bytes must be ciphertext (enc:v1: marker), not plaintext
	raw, _ := os.ReadFile(filepath.Join(enc.b.(*fsBackend).baseDir, "abc/manifest.json"))
	if !strings.HasPrefix(string(raw), "enc:v1:") {
		t.Fatalf("manifest not encrypted at rest: %q", raw[:min(20, len(raw))])
	}
	// GetBytes decrypts back
	got, err := enc.GetBytes(context.Background(), "abc/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, manifest) {
		t.Fatalf("decrypt mismatch: %q", got)
	}

	// legacy plaintext manifest (no key) reads fine (dual-read)
	plain := fsStore(t)
	put(t, plain, "def/manifest.json", manifest, "application/json")
	got2, _ := plain.GetBytes(context.Background(), "def/manifest.json")
	if !bytes.Equal(got2, manifest) {
		t.Fatal("plaintext manifest read mismatch")
	}
}

func TestFSHeadHasDelete(t *testing.T) {
	c := fsStore(t)
	put(t, c, "x/manifest.json", []byte("{}"), "application/json")

	h, err := c.Head(context.Background(), "x/manifest.json")
	if err != nil || h.ContentType != "application/json" {
		t.Fatalf("head: %v ct=%v", err, h)
	}
	if ok, _ := c.Has(context.Background(), "x/manifest.json"); !ok {
		t.Fatal("Has should be true")
	}
	if ok, _ := c.Has(context.Background(), "x/missing"); ok {
		t.Fatal("Has should be false for missing")
	}
	if err := c.Delete(context.Background(), "x/manifest.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(context.Background(), "x/manifest.json", ""); !IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestFSDeletePrefix(t *testing.T) {
	c := fsStore(t)
	put(t, c, "hash1/file_0/a.mkv", []byte("a"), "")
	put(t, c, "hash1/manifest.json", []byte("{}"), "")
	put(t, c, "hash2/file_0/b.mkv", []byte("b"), "")

	if err := c.DeletePrefix(context.Background(), "hash1/"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := c.Has(context.Background(), "hash1/file_0/a.mkv"); ok {
		t.Fatal("hash1 should be gone")
	}
	if ok, _ := c.Has(context.Background(), "hash2/file_0/b.mkv"); !ok {
		t.Fatal("hash2 must survive")
	}
}

func TestFSPathSafety(t *testing.T) {
	c := fsStore(t)
	if err := c.Put(context.Background(), "../escape", bytes.NewReader([]byte("x")), ""); err != nil {
		t.Fatalf("put escaping key should be contained, got %v", err)
	}
	outside := filepath.Join(filepath.Dir(c.b.(*fsBackend).baseDir), "escape")
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("path traversal escaped the base dir")
	}
}

func TestFSContentTypeByExtension(t *testing.T) {
	c := fsStore(t)
	put(t, c, "h/file_0/movie.mkv", []byte("data"), "")
	o, _ := c.Head(context.Background(), "h/file_0/movie.mkv")
	if o.ContentType != "video/x-matroska" {
		t.Fatalf("mkv content-type %q", o.ContentType)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
