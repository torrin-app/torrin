package stremthru

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/stremioid"
)

func TestTorzRoutesRegistered(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{}).Register(mux)
	cases := []struct{ method, path string }{
		{"POST", "/v0/store/torz"},
		{"GET", "/v0/store/torz"},
		{"GET", "/v0/store/torz/check"},
		{"GET", "/v0/store/torz/abc"},
		{"DELETE", "/v0/store/torz/abc"},
		{"POST", "/v0/store/torz/link/generate"},
	}
	for _, c := range cases {
		if _, pat := mux.Handler(httptest.NewRequest(c.method, c.path, nil)); pat == "" {
			t.Errorf("%s %s not registered", c.method, c.path)
		}
	}
}

func TestMagnetDataIncludesPrivate(t *testing.T) {
	h := &Handler{Deps: Deps{Store: fakeStore{err: context.DeadlineExceeded}}}
	if d := h.magnetData(context.Background(), packJob()); d["private"] != false {
		t.Errorf("private = %v, want false", d["private"])
	}
}

func TestReleaseLinkPaidGate(t *testing.T) {
	if plans.CanBYOK("free") {
		t.Error("free must not cache hoster/hdencode release-link content")
	}
	for _, p := range []string{"starter", "standard", "pro"} {
		if !plans.CanBYOK(p) {
			t.Errorf("%s should be allowed to cache release-link content", p)
		}
	}
}

type fakeStore struct {
	manifest []byte
	err      error
	missing  bool
}

func (f fakeStore) Has(context.Context, string) (bool, error) { return !f.missing, nil }
func (f fakeStore) GetBytes(context.Context, string) ([]byte, error) {
	return f.manifest, f.err
}
func (f fakeStore) Put(context.Context, string, io.Reader, string) error { return nil }
func (f fakeStore) Delete(context.Context, string) error                 { return nil }
func (f fakeStore) SignURL(path string, _ time.Duration) string          { return "sign://" + path }
func (f fakeStore) SignURLNode(node, path string, _ time.Duration) string {
	return "sign://" + node + "/" + path
}
func (f fakeStore) SignURLNodeUser(_, path, userID string, _ time.Duration) string {
	return "sign://" + path + "?u=" + userID
}

func packJob() *jobs.Job {
	return &jobs.Job{
		InfoHash: "abc", Status: jobs.StatusSeeding,
		Files: []jobs.File{
			{Name: "Reborn.Rookie.S01.1080p/Reborn.Rookie.S01E01.mkv", Size: 100},
			{Name: "Reborn.Rookie.S01.1080p/Reborn.Rookie.S01E07.mkv", Size: 200},
		},
	}
}

func TestBuildFileEntries(t *testing.T) {
	h := &Handler{Deps: Deps{Store: fakeStore{}}}
	out := h.buildFileEntries("user-1", "abc", "box2", packJob().Files)
	if len(out) != 2 {
		t.Fatalf("got %d entries, want 2", len(out))
	}
	if out[0]["size"] != int64(100) || out[1]["size"] != int64(200) {
		t.Errorf("sizes wrong: %v %v", out[0]["size"], out[1]["size"])
	}
	if link, _ := out[1]["link"].(string); !strings.HasPrefix(link, "sign://") {
		t.Errorf("entry missing node-signed link: %v", out[1])
	}
}

func TestExtractHash(t *testing.T) {
	h := "0123456789abcdef0123456789abcdef01234567"
	if got := extractHash("magnet:?xt=urn:btih:" + h + "&dn=x"); got != h {
		t.Errorf("magnet = %q", got)
	}
	if got := extractHash("xt=urn:btih:" + h); got != h {
		t.Errorf("bare = %q", got)
	}
	if got := extractHash("magnet:?xt=urn:btih:tooshort"); got != "" {
		t.Errorf("invalid should be empty, got %q", got)
	}
	if got := extractHash("https://example.com/x"); got != "" {
		t.Errorf("non-magnet should be empty, got %q", got)
	}
}

func TestStStatus(t *testing.T) {
	cases := map[jobs.Status]string{
		jobs.StatusComplete:    "downloaded",
		jobs.StatusDownloading: "downloading",
		jobs.StatusProcessing:  "downloading",
		jobs.StatusPublishing:  "downloading",
		jobs.StatusSeeding:     "downloaded",
		jobs.StatusFailed:      "failed",
		jobs.StatusPending:     "queued",
		jobs.StatusQueued:      "queued",
	}
	for in, want := range cases {
		if got := stStatus(in); got != want {
			t.Errorf("stStatus(%s)=%q want %q", in, got, want)
		}
	}
}

type fakeColdPull struct {
	allowed bool
	err     error
}

func (f fakeColdPull) ColdPullAllowed(context.Context, string, int) (bool, error) {
	return f.allowed, f.err
}

func TestColdPullBlocked(t *testing.T) {
	if !coldPullBlocked(context.Background(), fakeColdPull{allowed: false}, "u", 15) {
		t.Error("over-limit user must be blocked")
	}
	if coldPullBlocked(context.Background(), fakeColdPull{allowed: true}, "u", 15) {
		t.Error("under-limit user must not be blocked")
	}
	if coldPullBlocked(context.Background(), fakeColdPull{allowed: false, err: context.DeadlineExceeded}, "u", 15) {
		t.Error("must fail open: a checker error must not block the add")
	}
}

func TestFileEntry(t *testing.T) {
	e := fileEntry(2, "the.100.s02e01.mkv", 3517219191, "https://beam/link", nil)
	if e["path"] != "/the.100.s02e01.mkv" {
		t.Errorf("path = %v, want /the.100.s02e01.mkv", e["path"])
	}
	for _, k := range []string{"index", "name", "path", "size", "link"} {
		if _, ok := e[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

func TestDisplayName(t *testing.T) {
	m := "magnet:?xt=urn:btih:abcdef&dn=Big+Buck+Bunny+2008+1080p&tr=udp://x"
	if got := displayName(m); got != "Big Buck Bunny 2008 1080p" {
		t.Errorf("dn = %q", got)
	}
	if got := displayName("magnet:?xt=urn:btih:abcdef"); got != "" {
		t.Errorf("no dn should be empty, got %q", got)
	}
}

func TestMagnetDataUsesManifestBasenames(t *testing.T) {
	m := manifest.Manifest{
		InfoHash: "abc", Name: "Reborn.Rookie.S01.1080p",
		Files: []manifest.File{
			{FileName: "Reborn.Rookie.S01E01.mkv", FileSize: 100},
			{FileName: "Reborn.Rookie.S01E07.mkv", FileSize: 200},
		},
	}
	data, _ := m.Marshal()
	h := &Handler{Deps: Deps{Store: fakeStore{manifest: data}}}

	files, _ := h.magnetData(context.Background(), packJob())["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	// name must be the manifest basename, not the folder-prefixed DB name
	if files[1]["name"] != "Reborn.Rookie.S01E07.mkv" {
		t.Errorf("name = %q, want basename", files[1]["name"])
	}
	// the R2 key in the link must use the basename too
	if link, _ := files[1]["link"].(string); !strings.Contains(link, "abc/file_1/Reborn.Rookie.S01E07.mkv") {
		t.Errorf("link = %q, want basename key", link)
	}
}

func TestMagnetDataFallsBackToJobFilesWithoutManifest(t *testing.T) {
	h := &Handler{Deps: Deps{Store: fakeStore{err: context.DeadlineExceeded}}}
	files, _ := h.magnetData(context.Background(), packJob())["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("files = %d, want 2", len(files))
	}
	if files[1]["name"] != "Reborn.Rookie.S01.1080p/Reborn.Rookie.S01E07.mkv" {
		t.Errorf("fallback name = %q, want job file name", files[1]["name"])
	}
}

func TestMagnetDataFiltersSeasonPackUsingCurrentTarget(t *testing.T) {
	m := manifest.Manifest{
		InfoHash: "abc", Name: "Example.Show.S05.1080p",
		Files: []manifest.File{
			{FileName: "Example.Show.S05E01.mkv", FileSize: 100},
			{FileName: "Example.Show.S05E02.mkv", FileSize: 200},
			{FileName: "Example.Show.S12E01.mkv", FileSize: 300},
		},
	}
	data, _ := m.Marshal()
	h := &Handler{Deps: Deps{Store: fakeStore{manifest: data}}}
	stale := &jobs.Job{InfoHash: "abc", UserID: "u", Status: jobs.StatusComplete, Season: 12, Episode: 1}

	files := h.magnetDataForTarget(context.Background(), stale, stremioid.Parse("tt3121722:5:2"))["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("files = %d, want one S05E02 file", len(files))
	}
	if files[0]["name"] != "Example.Show.S05E02.mkv" {
		t.Fatalf("name = %v, want S05E02", files[0]["name"])
	}
	if files[0]["index"] != 1 {
		t.Fatalf("index = %v, want original manifest index 1", files[0]["index"])
	}
	if link, _ := files[0]["link"].(string); !strings.Contains(link, "abc/file_1/Example.Show.S05E02.mkv") {
		t.Fatalf("link = %q, want original file_1 key", link)
	}
}

func TestMagnetDataIncludesMagnet(t *testing.T) {
	h := &Handler{Deps: Deps{Store: fakeStore{err: context.DeadlineExceeded}}}
	j := packJob()
	j.Magnet = "https://scene-rls.net/some-release/"
	d := h.magnetData(context.Background(), j)
	m, _ := d["magnet"].(string)
	if !strings.HasPrefix(m, "magnet:?xt=urn:btih:abc") {
		t.Errorf("magnet = %q, want a proper magnet URI (not the stored source)", m)
	}
	if _, ok := d["name"]; !ok {
		t.Error("name key missing")
	}
}

func TestCachedFilesReturnsManifestName(t *testing.T) {
	m := manifest.Manifest{
		InfoHash: "abc", Name: "Some.Movie.2020.1080p",
		Files: []manifest.File{{FileName: "movie.mkv", FileSize: 100}},
	}
	data, _ := m.Marshal()
	h := &Handler{Deps: Deps{Store: fakeStore{manifest: data}}}

	name, files, ok := h.cachedFiles(context.Background(), "user-1", "abc")
	if !ok || len(files) != 1 {
		t.Fatalf("ok=%v files=%d", ok, len(files))
	}
	if name != "Some.Movie.2020.1080p" {
		t.Errorf("name = %q, want manifest name", name)
	}
}
