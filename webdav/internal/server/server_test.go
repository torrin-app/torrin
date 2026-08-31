package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
)

func TestBuildTreeOverrides(t *testing.T) {
	overrides := map[string]auth.WebdavOverride{
		auth.WebdavKey("bbbbbbbb2222", 0):  {InfoHash: "bbbbbbbb2222", Alias: "Beta.S01E01.mkv"},
		auth.WebdavKey("bbbbbbbb2222", 1):  {InfoHash: "bbbbbbbb2222", FileIndex: 1, Excluded: true},
		auth.WebdavKey("aaaaaaaa1111", -1): {InfoHash: "aaaaaaaa1111", FileIndex: -1, Excluded: true},
	}
	root := buildTree(sample(), overrides)

	byHash := func(h string) *node {
		for _, c := range root.children {
			if c.hash == h {
				return c
			}
		}
		return nil
	}

	if a := byHash("aaaaaaaa1111"); a == nil || !a.hidden {
		t.Fatalf("excluded release should be present and hidden, got %+v", a)
	}
	beta := byHash("bbbbbbbb2222")
	if beta == nil {
		t.Fatal("beta folder missing")
	}
	var renamed, hidden bool
	for _, f := range beta.children {
		if f.idx == 0 && f.name == "Beta.S01E01.mkv" && !f.hidden {
			renamed = true
		}
		if f.idx == 1 && f.hidden {
			hidden = true
		}
	}
	if !renamed || !hidden {
		t.Fatalf("beta: renamed=%v hidden=%v (want both), children=%v", renamed, hidden, names(beta))
	}
}

func TestDavReaddirHidesExcluded(t *testing.T) {
	overrides := map[string]auth.WebdavOverride{
		auth.WebdavKey("bbbbbbbb2222", 1): {InfoHash: "bbbbbbbb2222", FileIndex: 1, Excluded: true},
	}
	fs := davFS{root: buildTree(sample(), overrides)}
	f, err := fs.OpenFile(context.Background(), "/Beta S01", 0, 0)
	if err != nil {
		t.Fatalf("open beta folder: %v", err)
	}
	infos, _ := f.Readdir(0)
	if len(infos) != 1 {
		t.Fatalf("excluded file must be filtered from the DAV listing, got %d entries", len(infos))
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestServeLogsErrors(t *testing.T) {
	buf := captureLogs(t)
	w := httptest.NewRecorder()
	(&Server{}).serve(w, httptest.NewRequest(http.MethodGet, "/private.mkv", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code %d, want 401", w.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "status=401") || !strings.Contains(out, "/private.mkv") {
		t.Errorf("expected error log with status+path, got: %q", out)
	}
	if !strings.Contains(out, `reason="no credentials"`) {
		t.Errorf("expected auth-failure reason in log, got: %q", out)
	}
}

func TestServeDoesNotLogSuccess(t *testing.T) {
	buf := captureLogs(t)
	w := httptest.NewRecorder()
	(&Server{}).serve(w, httptest.NewRequest(http.MethodOptions, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("code %d, want 200", w.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("success must not log, got: %q", buf.String())
	}
}

type fakeRepo struct {
	jobs.Repository
	list   []*jobs.Job
	viewed []string
}

func (f *fakeRepo) ListByUser(context.Context, string, int) ([]*jobs.Job, error) { return f.list, nil }
func (f *fakeRepo) RecordView(_ context.Context, hash, _ string) (bool, error) {
	f.viewed = append(f.viewed, hash)
	return true, nil
}

type fakeSigner struct {
	last     string
	lastNode string
}

func (s *fakeSigner) SignURLNode(node, p string, _ time.Duration) string {
	s.last, s.lastNode = p, node
	return "https://beam/" + p
}

func file(idx int, name string, size int64) jobs.File {
	return jobs.File{Index: idx, Name: name, Size: size}
}

func job(name, hash string, files ...jobs.File) *jobs.Job {
	return &jobs.Job{Name: name, InfoHash: hash, Status: jobs.StatusComplete, UpdatedAt: time.Unix(1700000000, 0), Files: files}
}

func sample() []*jobs.Job {
	return []*jobs.Job{
		job("Alpha 2020", "aaaaaaaa1111", file(0, "alpha.mkv", 100)),
		job("Beta S01", "bbbbbbbb2222", file(0, "pack/S01E01.mkv", 200), file(1, "pack/S01E01.mkv", 210)),
		job("Alpha 2020", "cccccccc3333", file(0, "alpha.mkv", 50)),
		{Name: "Pending", InfoHash: "dddd", Status: jobs.StatusDownloading, Files: []jobs.File{file(0, "x.mkv", 1)}},
	}
}

func names(n *node) []string {
	var out []string
	for _, c := range n.children {
		out = append(out, c.name)
	}
	return out
}

func TestBuildTreeHierarchyAndExclusion(t *testing.T) {
	root := buildTree(sample(), nil)
	if len(root.children) != 3 {
		t.Fatalf("root folders = %d, want 3 (pending excluded)", len(root.children))
	}
	leaf := root.find([]string{"Alpha 2020", "alpha.mkv"})
	if leaf == nil || leaf.dir || leaf.hash != "aaaaaaaa1111" || leaf.size != 100 {
		t.Errorf("alpha leaf resolved wrong: %+v", leaf)
	}
	if root.find([]string{"nope"}) != nil {
		t.Error("missing path should resolve nil")
	}
}

func TestBuildTreeCollisions(t *testing.T) {
	root := buildTree(sample(), nil)
	if root.index["Alpha 2020"] == nil || root.index["Alpha 2020 [cccccccc]"] == nil {
		t.Errorf("folder-name collision not disambiguated: %v", names(root))
	}
	beta := root.index["Beta S01"]
	if beta == nil || len(beta.children) != 2 {
		t.Fatalf("beta children = %v", names(beta))
	}
	if beta.index["S01E01.mkv"] == nil || beta.index["S01E01.mkv [1]"] == nil {
		t.Errorf("basename collision within folder not disambiguated: %v", names(beta))
	}
}

func propfind(t *testing.T, path, depth string) string {
	t.Helper()
	h := &webdav.Handler{FileSystem: davFS{root: buildTree(sample(), nil)}, LockSystem: webdav.NewMemLS()}
	r := httptest.NewRequest("PROPFIND", path, nil)
	r.Header.Set("Depth", depth)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND %s depth %s: code %d", path, depth, w.Code)
	}
	return w.Body.String()
}

func TestPropfindListsFolders(t *testing.T) {
	body := propfind(t, "/", "1")
	for _, want := range []string{"Alpha 2020", "Beta S01", "collection"} {
		if !strings.Contains(body, want) {
			t.Errorf("root PROPFIND missing %q", want)
		}
	}
}

func TestPropfindFileProps(t *testing.T) {
	body := propfind(t, "/Beta%20S01", "1")
	for _, want := range []string{"S01E01.mkv", "getcontentlength", "200", "getetag", "video"} {
		if !strings.Contains(body, want) {
			t.Errorf("folder PROPFIND missing %q", want)
		}
	}
}

func TestGetFileRedirects(t *testing.T) {
	sig := &fakeSigner{}
	repo := &fakeRepo{}
	s := &Server{jobs: repo, store: sig}
	r := httptest.NewRequest(http.MethodGet, "/Alpha%202020/alpha.mkv", nil)
	w := httptest.NewRecorder()
	s.get(w, r, "u1", buildTree(sample(), nil))
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("code %d, want 307", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "aaaaaaaa1111") {
		t.Errorf("Location = %q", loc)
	}
	if len(repo.viewed) != 1 || repo.viewed[0] != "aaaaaaaa1111" {
		t.Errorf("view not recorded: %v", repo.viewed)
	}
}

func TestGetFileSignsWithNode(t *testing.T) {
	hash := strings.Repeat("b", 40)
	j := &jobs.Job{Name: "Box2 Movie", InfoHash: hash, Node: "box2", Status: jobs.StatusComplete, UpdatedAt: time.Unix(1700000000, 0),
		Files: []jobs.File{{Index: 0, Name: "movie.mkv", Size: 100, Key: "blobs/cafe"}}}
	sig := &fakeSigner{}
	s := &Server{jobs: &fakeRepo{}, store: sig}
	r := httptest.NewRequest(http.MethodGet, "/Box2%20Movie/movie.mkv", nil)
	w := httptest.NewRecorder()
	s.get(w, r, "u1", buildTree([]*jobs.Job{j}, nil))
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("code %d, want 307", w.Code)
	}
	if sig.lastNode != "box2" {
		t.Fatalf("redirect must be signed for the content's node, got node=%q", sig.lastNode)
	}
}

func TestGetEncryptedBlobRedirect(t *testing.T) {
	hash := strings.Repeat("a", 40)
	j := &jobs.Job{Name: "Enc Movie", InfoHash: hash, Status: jobs.StatusComplete, UpdatedAt: time.Unix(1700000000, 0),
		Files: []jobs.File{{Index: 0, Name: "movie.mkv", Size: 100, Key: "blobs/deadbeef", Enc: true}}}
	s := &Server{jobs: &fakeRepo{}, store: &fakeSigner{}}
	r := httptest.NewRequest(http.MethodGet, "/Enc%20Movie/movie.mkv", nil)
	w := httptest.NewRecorder()
	s.get(w, r, "u1", buildTree([]*jobs.Job{j}, nil))
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "blobs/deadbeef") {
		t.Errorf("Location missing blob key: %q", loc)
	}
	if !strings.Contains(loc, "ih="+hash) || !strings.Contains(loc, "enc=1") {
		t.Errorf("Location missing stream query: %q", loc)
	}
}

func TestGetDirRendersHTML(t *testing.T) {
	s := &Server{jobs: &fakeRepo{}, store: &fakeSigner{}}
	r := httptest.NewRequest(http.MethodGet, "/Beta%20S01", nil)
	w := httptest.NewRecorder()
	s.get(w, r, "u1", buildTree(sample(), nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("code=%d ct=%q", w.Code, w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "S01E01.mkv") {
		t.Error("HTML listing missing file")
	}
}

func TestGetNotFound(t *testing.T) {
	s := &Server{jobs: &fakeRepo{}, store: &fakeSigner{}}
	w := httptest.NewRecorder()
	s.get(w, httptest.NewRequest(http.MethodGet, "/ghost/x.mkv", nil), "u1", buildTree(sample(), nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("code %d, want 404", w.Code)
	}
}

func TestOptions(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).serve(w, httptest.NewRequest(http.MethodOptions, "/", nil))
	if w.Code != 200 || w.Header().Get("DAV") != "1, 2" {
		t.Errorf("OPTIONS: code=%d dav=%q", w.Code, w.Header().Get("DAV"))
	}
}

func TestUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).serve(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("expected 401 challenge, got %d", w.Code)
	}
}

func TestBuildTreeSanitizesSlashNames(t *testing.T) {
	j := &jobs.Job{Name: "Zadok - 08/08/2026", InfoHash: strings.Repeat("c", 40), Status: jobs.StatusComplete,
		UpdatedAt: time.Unix(1700000000, 0),
		Files:     []jobs.File{{Index: 0, Name: "clip.mkv", Size: 10, Key: "blobs/x"}}}
	root := buildTree([]*jobs.Job{j}, nil)
	if len(root.children) != 1 {
		t.Fatalf("want 1 folder, got %d", len(root.children))
	}
	folder := root.children[0]
	if strings.Contains(folder.name, "/") {
		t.Fatalf("folder name must not contain a path separator: %q", folder.name)
	}
	// x/net/webdav's PROPFIND walk re-Stats each child by path; a slash in the
	// name used to break that round-trip and 500 the whole listing.
	fs := davFS{root: root}
	if _, err := fs.Stat(context.Background(), "/"+folder.name); err != nil {
		t.Fatalf("sanitized folder must resolve by path: %v", err)
	}
	if _, err := fs.Stat(context.Background(), "/"+folder.name+"/"+folder.children[0].name); err != nil {
		t.Fatalf("child file must resolve by path: %v", err)
	}
}
