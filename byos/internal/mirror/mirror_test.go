package mirror

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/storage"
)

type fakeSource struct{ data []byte }

func (f fakeSource) Get(_ context.Context, _, _ string) (*storage.Object, error) {
	return &storage.Object{Body: io.NopCloser(bytes.NewReader(f.data)), Size: int64(len(f.data))}, nil
}

func TestOpenDecryptedPlain(t *testing.T) {
	want := []byte("plain media bytes")
	r, err := openDecrypted(context.Background(), fakeSource{want}, nil, "k", false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, want) {
		t.Fatalf("plain passthrough: got %q", got)
	}
}

func TestOpenDecryptedEncrypted(t *testing.T) {
	cipher, err := crypto.NewStream("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("media-"), 5000)
	encR, err := cipher.EncryptReader(bytes.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := io.ReadAll(encR)
	if bytes.Equal(enc, want) {
		t.Fatal("test data not actually encrypted")
	}
	r, err := openDecrypted(context.Background(), fakeSource{enc}, cipher, "blobs/x", true)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	if !bytes.Equal(got, want) {
		t.Fatalf("decrypt mismatch: got %d bytes want %d", len(got), len(want))
	}
}
