package blob

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func key(t *testing.T, data []byte) string {
	t.Helper()
	p := write(t, data)
	k, err := ContentKey(p, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestContentKeyDeterministic(t *testing.T) {
	data := make([]byte, 5<<20)
	for i := range data {
		data[i] = byte(i)
	}
	first := key(t, data)
	second := key(t, data)
	if first != second {
		t.Fatal("same content must yield same key")
	}
}

func TestContentKeySizeSensitive(t *testing.T) {
	a := make([]byte, 3<<20)
	b := make([]byte, 3<<20+1)
	if key(t, a) == key(t, b) {
		t.Fatal("different size must differ")
	}
}

func TestContentKeyMiddleDiffersLargeFile(t *testing.T) {
	a := make([]byte, 5<<20)
	b := make([]byte, 5<<20)
	copy(a, []byte{1, 2, 3})
	copy(b, []byte{1, 2, 3})
	a[len(a)-1] = 9
	b[len(b)-1] = 8
	if key(t, a) == key(t, b) {
		t.Fatal("differing tail must change key")
	}
}

func TestContentKeySmallFileWholeHash(t *testing.T) {
	a := []byte("hello world one")
	b := []byte("hello world two")
	if key(t, a) == key(t, b) {
		t.Fatal("small files hashed whole must differ on any change")
	}
}

func TestStorageKey(t *testing.T) {
	if StorageKey("b_abc") != "blobs/b_abc" {
		t.Fatal("storage key format")
	}
}
