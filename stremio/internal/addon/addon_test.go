package addon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/stremioid"
)

type fakeJobRepository struct {
	warm   map[string]*jobs.Job
	byHash map[string][]*jobs.Job
	byIMDB []*jobs.Job
	byBYOS []*jobs.Job
}

func (f *fakeJobRepository) CachedByHashes(_ context.Context, hashes []string) (map[string]*jobs.Job, error) {
	out := map[string]*jobs.Job{}
	for _, hash := range hashes {
		if job := f.warm[hash]; job != nil {
			out[hash] = job
		}
	}
	return out, nil
}

func (*fakeJobRepository) RecordView(context.Context, string, string) (bool, error) {
	return true, nil
}
func (f *fakeJobRepository) ListByInfoHash(_ context.Context, hash string) ([]*jobs.Job, error) {
	return f.byHash[hash], nil
}
func (f *fakeJobRepository) ListByIMDB(context.Context, string) ([]*jobs.Job, error) {
	return f.byIMDB, nil
}
func (f *fakeJobRepository) ListUserByosByIMDB(context.Context, string, string) ([]*jobs.Job, error) {
	return f.byBYOS, nil
}
func (*fakeJobRepository) ListByTitleNorm(context.Context, string) ([]*jobs.Job, error) {
	return nil, nil
}

type fakeStreamStore struct {
	manifest []byte
	err      error
}

func (f fakeStreamStore) GetBytes(context.Context, string) ([]byte, error) {
	return f.manifest, f.err
}
func (fakeStreamStore) SignURLNode(node, path string, _ time.Duration) string {
	return "node://" + node + "/" + path + "?signed=1"
}
func (fakeStreamStore) SignURLNodeUser(node, path, userID string, _ time.Duration) string {
	return "user://" + node + "/" + path + "?u=" + userID + "&signed=1"
}

func streamLink(t *testing.T, streams []map[string]any) string {
	t.Helper()
	if len(streams) != 1 {
		t.Fatalf("streams = %+v, want one", streams)
	}
	link, _ := streams[0]["url"].(string)
	return link
}

func TestLibraryFilesFiltersRequestedEpisode(t *testing.T) {
	j := &jobs.Job{Season: 5, Files: []jobs.File{
		{Name: "Example.Show.S05E01.mkv"},
		{Name: "Example.Show.S05E02.mkv"},
		{Name: "Example.Show.S12E01.mkv"},
	}}
	got := libraryFiles(j, stremioid.Parse("tt3121722:5:1"))
	if len(got) != 1 {
		t.Fatalf("files = %d, want one S05E01 file", len(got))
	}
	if got[0].Name != "Example.Show.S05E01.mkv" || got[0].Index != 0 {
		t.Fatalf("file = %+v, want the original S05E01 file", got[0])
	}
}

func TestLibraryFilesDoesNotReturnWrongSeasonPack(t *testing.T) {
	j := &jobs.Job{Season: 12, Files: []jobs.File{{Name: "Example.Show.S12E01.mkv"}}}
	if got := libraryFiles(j, stremioid.Parse("tt3121722:5:1")); len(got) != 0 {
		t.Fatalf("wrong-season files = %v, want none", got)
	}
}

func TestLibraryFilesDoesNotFilterMovies(t *testing.T) {
	j := &jobs.Job{Files: []jobs.File{
		{Name: "Movie.2024.mkv"},
		{Name: "Movie.behind-the-scenes.mkv"},
	}}
	got := libraryFiles(j, stremioid.Parse("tt0816692"))
	if len(got) != len(j.Files) {
		t.Fatalf("movie files = %d, want %d", len(got), len(j.Files))
	}
}

func TestStreamTargetMatchesType(t *testing.T) {
	tests := []struct {
		contentType string
		contentID   string
		want        bool
	}{
		{contentType: "movie", contentID: "tt0816692", want: true},
		{contentType: "series", contentID: "tt3121722:5:1", want: true},
		{contentType: "series", contentID: "tt3121722:0:2", want: true},
		{contentType: "series", contentID: "tt3121722"},
		{contentType: "movie", contentID: "tt3121722:5:1"},
		{contentType: "series", contentID: "tt3121722:bad:1"},
		{contentType: "other", contentID: "tt0816692"},
	}
	for _, tt := range tests {
		t.Run(tt.contentType+"/"+tt.contentID, func(t *testing.T) {
			if got := streamTargetMatchesType(tt.contentType, stremioid.Parse(tt.contentID)); got != tt.want {
				t.Fatalf("streamTargetMatchesType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEntryIncludesPlaybackHints(t *testing.T) {
	hash := "C12FE1C06BBA254A9DC9F519B335AA7C1367A88A"
	got := entry(`Show\Season 05\Show.S05E01.MKV`, "https://stream.example/file", hash, 1234)
	if got["title"] != "Show.S05E01.MKV" {
		t.Fatalf("title = %v, want base filename", got["title"])
	}
	if got["description"] != "Show.S05E01.MKV" {
		t.Fatalf("description = %v, want base filename", got["description"])
	}
	hints := got["behaviorHints"].(map[string]any)
	if hints["filename"] != "Show.S05E01.MKV" {
		t.Errorf("filename = %v", hints["filename"])
	}
	if hints["notWebReady"] != true {
		t.Errorf("notWebReady = %v, want true for MKV", hints["notWebReady"])
	}
	if hints["bingeGroup"] != "torrin:c12fe1c06bba254a9dc9f519b335aa7c1367a88a" {
		t.Errorf("bingeGroup = %v", hints["bingeGroup"])
	}
	if hints["videoSize"] != int64(1234) {
		t.Errorf("videoSize = %v", hints["videoSize"])
	}

	mp4 := entry("movie.MP4", "https://stream.example/file", hash, 0)
	mp4Hints := mp4["behaviorHints"].(map[string]any)
	if mp4Hints["notWebReady"] != false {
		t.Errorf("HTTPS MP4 notWebReady = %v, want false", mp4Hints["notWebReady"])
	}
	if _, ok := mp4Hints["videoSize"]; ok {
		t.Error("unknown video size should be omitted")
	}
}

func TestByLibraryPrefersCrossNodeWarmOverCairn(t *testing.T) {
	hash := strings.Repeat("a", 40)
	direct := &jobs.Job{InfoHash: hash, Season: 5, Episode: 2, Files: []jobs.File{{
		Index: 1, Name: "Show.S05E02.mkv", Size: 200, Key: cairn.StreamPath(hash, 1, "Show.S05E02.mkv"),
	}}}
	warm := &jobs.Job{InfoHash: hash, Node: "box2", Files: []jobs.File{{
		Index: 1, Name: "Show.S05E02.mkv", Size: 200, Key: "blobs/warm-episode",
	}}}
	s := &Server{
		jobs:  &fakeJobRepository{warm: map[string]*jobs.Job{hash: warm}, byIMDB: []*jobs.Job{direct}, byBYOS: []*jobs.Job{direct}},
		store: fakeStreamStore{},
	}
	r := httptest.NewRequest(http.MethodGet, "/stream/series/tt1234567:5:2", nil)
	link := streamLink(t, s.byLibrary(r, "series", stremioid.Parse("tt1234567:5:2"), "user-1", true))
	if !strings.Contains(link, "node://box2/blobs/warm-episode") || strings.Contains(link, "/cairn/") || strings.Contains(link, "byos=1") {
		t.Fatalf("warm link = %q", link)
	}
}

func TestByLibraryCairnURLIsUserBound(t *testing.T) {
	hash := strings.Repeat("b", 40)
	direct := &jobs.Job{InfoHash: hash, Files: []jobs.File{{
		Index: 2, Name: "Show.S05E02.mkv", Size: 200, Key: cairn.StreamPath(hash, 2, "Show.S05E02.mkv"),
	}}}
	s := &Server{jobs: &fakeJobRepository{byIMDB: []*jobs.Job{direct}}, store: fakeStreamStore{}}
	r := httptest.NewRequest(http.MethodGet, "/stream/series/tt1234567:5:2", nil)
	link := streamLink(t, s.byLibrary(r, "series", stremioid.Parse("tt1234567:5:2"), "user-2", false))
	if !strings.Contains(link, hash+"/cairn/2/Show.S05E02.mkv") || !strings.Contains(link, "?u=user-2") {
		t.Fatalf("direct Cairn link = %q", link)
	}
}

func TestByLibraryPrefersUserBYOSOverCairnWithoutWarmCopy(t *testing.T) {
	hash := strings.Repeat("e", 40)
	direct := &jobs.Job{InfoHash: hash, Files: []jobs.File{{
		Name: "Show.S05E02.mkv", Key: cairn.StreamPath(hash, 0, "Show.S05E02.mkv"),
	}}}
	byos := &jobs.Job{InfoHash: hash, Node: "box2", Files: []jobs.File{{
		Name: "Show.S05E02.mkv", Key: "blobs/byos-episode",
	}}}
	s := &Server{
		jobs:  &fakeJobRepository{byIMDB: []*jobs.Job{direct}, byBYOS: []*jobs.Job{byos}},
		store: fakeStreamStore{},
	}
	r := httptest.NewRequest(http.MethodGet, "/stream/series/tt1234567:5:2", nil)
	link := streamLink(t, s.byLibrary(r, "series", stremioid.Parse("tt1234567:5:2"), "user-5", true))
	if !strings.Contains(link, "user://box2/"+hash+"/file_0/Show.S05E02.mkv?u=user-5") || !strings.Contains(link, "byos=1") {
		t.Fatalf("BYOS link = %q", link)
	}
}

func TestByHashFallsBackToCrossNodeWarm(t *testing.T) {
	hash := strings.Repeat("c", 40)
	warm := &jobs.Job{InfoHash: hash, Node: "box3", Files: []jobs.File{{Name: "movie.mkv", Key: "blobs/movie", Size: 300}}}
	s := &Server{
		jobs:  &fakeJobRepository{warm: map[string]*jobs.Job{hash: warm}},
		store: fakeStreamStore{err: errors.New("local manifest missing")},
	}
	r := httptest.NewRequest(http.MethodGet, "/stream/movie/"+hash, nil)
	link := streamLink(t, s.byHash(r, hash, "user-3"))
	if !strings.Contains(link, "node://box3/blobs/movie") {
		t.Fatalf("cross-node hash link = %q", link)
	}
}

func TestByHashFallsBackToUserBoundCairn(t *testing.T) {
	hash := strings.Repeat("d", 40)
	direct := &jobs.Job{InfoHash: hash, Status: jobs.StatusComplete, Files: []jobs.File{{
		Name: "movie.mkv", Key: cairn.StreamPath(hash, 0, "movie.mkv"), Size: 300,
	}}}
	s := &Server{
		jobs:  &fakeJobRepository{byHash: map[string][]*jobs.Job{hash: {direct}}},
		store: fakeStreamStore{err: errors.New("local manifest missing")},
	}
	r := httptest.NewRequest(http.MethodGet, "/stream/movie/"+hash, nil)
	link := streamLink(t, s.byHash(r, hash, "user-4"))
	if !strings.Contains(link, hash+"/cairn/0/movie.mkv") || !strings.Contains(link, "?u=user-4") {
		t.Fatalf("direct hash Cairn link = %q", link)
	}
}

func TestManifest(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).manifest(w, httptest.NewRequest(http.MethodGet, "/key/manifest.json", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var m map[string]any
	json.Unmarshal(w.Body.Bytes(), &m)
	if m["id"] != "app.torrin.stremio" {
		t.Errorf("manifest id = %v", m["id"])
	}
}
