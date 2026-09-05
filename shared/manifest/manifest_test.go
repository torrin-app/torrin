package manifest

import (
	"context"
	"errors"
	"github.com/torrin-app/torrin/shared/mediainfo"
	"strings"
	"testing"
)

type fakeBlobStore struct {
	manifest []byte
	blobs    map[string]bool
}

func (f *fakeBlobStore) GetBytes(_ context.Context, key string) ([]byte, error) {
	if strings.HasSuffix(key, FileName) && f.manifest != nil {
		return f.manifest, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeBlobStore) Has(_ context.Context, key string) (bool, error) {
	return f.blobs[key], nil
}

func TestPlayable(t *testing.T) {
	hash := strings.Repeat("a", 40)
	data, err := Manifest{InfoHash: hash, Name: "X", Files: []File{
		{FileName: "a.mkv", DirectURL: "blobs/b_abc", FileSize: 100},
	}}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	blobKey := ResolveKey(hash, 0, "blobs/b_abc", "a.mkv")

	cases := []struct {
		name  string
		store *fakeBlobStore
		want  bool
	}{
		{"manifest + blob present", &fakeBlobStore{manifest: data, blobs: map[string]bool{blobKey: true}}, true},
		{"blob evicted", &fakeBlobStore{manifest: data, blobs: map[string]bool{}}, false},
		{"no manifest", &fakeBlobStore{manifest: nil, blobs: map[string]bool{}}, false},
	}
	for _, c := range cases {
		if got := Playable(context.Background(), c.store, hash); got != c.want {
			t.Errorf("%s: Playable = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStreamQueryAlwaysCarriesInfoHash(t *testing.T) {
	ih := "2a0a2ab16bace3b40204a864407f955ddbca9ec9"
	if got := StreamQuery(ih, false); got != "&ih="+ih {
		t.Errorf("blob content: got %q", got)
	}
	if got := StreamQuery(ih, true); got != "&ih="+ih+"&enc=1" {
		t.Errorf("encrypted: got %q", got)
	}
	if got := StreamQuery("short", false); got != "" {
		t.Errorf("non-infohash must not add ih: got %q", got)
	}
}

func TestMetaPreservesMeasuredMediaInfo(t *testing.T) {
	data, err := (Manifest{Files: []File{{FileName: "movie.mkv", MediaInfo: &mediainfo.Info{Resolution: "2160p", Bitrate: 40000000}}}}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	_, _, files := Meta(data)
	if len(files) != 1 || files[0].MediaInfo == nil || files[0].MediaInfo.Bitrate != 40000000 {
		t.Fatalf("lost media info %+v", files)
	}
}
