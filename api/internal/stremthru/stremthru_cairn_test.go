package stremthru

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/stremioid"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type fakeCairnRepository struct {
	hash string
	name string
	nzb  []byte
}

type fakeCachedLookup map[string]*jobs.Job

func (f fakeCachedLookup) CachedByHashes(_ context.Context, hashes []string) (map[string]*jobs.Job, error) {
	out := make(map[string]*jobs.Job, len(hashes))
	for _, hash := range hashes {
		if job := f[hash]; job != nil {
			out[hash] = job
		}
	}
	return out, nil
}

func (f fakeCairnRepository) GetCairnArchive(_ context.Context, hash string) (string, string, bool) {
	return "nzb/" + hash + ".nzb", f.name, hash == f.hash
}

func (f fakeCairnRepository) GetCairnNZB(_ context.Context, hash string) ([]byte, bool) {
	return f.nzb, hash == f.hash && len(f.nzb) > 0
}

func directCairnHandler(t *testing.T, hash string) *Handler {
	t.Helper()
	cipher, err := crypto.NewStream(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	plainSize := int64(100000)
	encSize, err := cipher.EncryptedSize(plainSize)
	if err != nil {
		t.Fatal(err)
	}
	nzbData := nzb.Generate([]nzb.OutFile{{Name: "movie.mkv", Group: "alt.test", Segments: []nzb.Segment{
		{MessageID: "part-1", Number: 1, Bytes: encSize},
	}}})
	return New(Deps{
		Store:      fakeStore{err: context.DeadlineExceeded, missing: true},
		Cairns:     fakeCairnRepository{hash: hash, name: "Cold Movie", nzb: nzbData},
		CairnStore: fakeStore{manifest: nzbData}, CairnCipher: cipher, CairnDirect: true,
	})
}

func TestCheckMagnetsReportsDirectCairnAsCached(t *testing.T) {
	hash := strings.Repeat("a", 40)
	h := directCairnHandler(t, hash)
	r := httptest.NewRequest("GET", "/v0/store/magnets/check?magnet="+hash, nil)
	w := httptest.NewRecorder()
	h.checkMagnets(w, r, &auth.User{ID: "user-1", PlanID: "standard"})
	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Items []struct {
				Status string           `json:"status"`
				Files  []map[string]any `json:"files"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != 1 || response.Data.Items[0].Status != "cached" || len(response.Data.Items[0].Files) != 1 {
		t.Fatalf("unexpected check response: %s", w.Body.String())
	}
	file := response.Data.Items[0].Files[0]
	link, _ := file["link"].(string)
	if !strings.Contains(link, hash+"/cairn/0/movie.mkv") || !strings.Contains(link, "u=user-1") || !strings.Contains(link, "enc=1") {
		t.Errorf("direct cairn link = %q", link)
	}
	if file["size"] != float64(100000) {
		t.Errorf("plaintext size = %v, want 100000", file["size"])
	}
}

func TestMagnetDataResolvesEvictedCairnDirectly(t *testing.T) {
	hash := strings.Repeat("b", 40)
	h := directCairnHandler(t, hash)
	data := h.magnetData(context.Background(), &jobs.Job{
		ID: "job-1", UserID: "user-2", InfoHash: hash, Name: "Evicted", Status: jobs.StatusEvicted,
	})
	if data["status"] != "downloaded" || data["name"] != "Cold Movie" {
		t.Fatalf("resolved item = %+v", data)
	}
	files, _ := data["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("resolved files = %+v", data["files"])
	}
	link, _ := files[0]["link"].(string)
	if !strings.Contains(link, hash+"/cairn/0/movie.mkv") || !strings.Contains(link, "u=user-2") {
		t.Errorf("resolved link = %q", link)
	}
}

func TestMagnetDataFiltersCairnSeasonPackAndPreservesNZBIndex(t *testing.T) {
	hash := strings.Repeat("d", 40)
	nzbData := nzb.Generate([]nzb.OutFile{
		{Name: "Example.Show.S05E01.mkv", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "part-1", Number: 1, Bytes: 100}}},
		{Name: "Example.Show.S05E02.mkv", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "part-2", Number: 1, Bytes: 200}}},
		{Name: "Example.Show.S12E01.mkv", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "part-3", Number: 1, Bytes: 300}}},
	})
	h := New(Deps{
		Store:      fakeStore{err: context.DeadlineExceeded, missing: true},
		Cairns:     fakeCairnRepository{hash: hash, name: "Example Show", nzb: nzbData},
		CairnStore: fakeStore{manifest: nzbData}, CairnDirect: true,
	})
	data := h.magnetDataForTarget(context.Background(), &jobs.Job{
		ID: "job-1", UserID: "user-1", InfoHash: hash, Name: "Example Show",
		Status: jobs.StatusEvicted, Season: 12, Episode: 1,
	}, stremioid.Parse("tt3121722:5:2"))
	files, _ := data["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("files = %+v, want one S05E02 Cairn file", files)
	}
	if files[0]["name"] != "Example.Show.S05E02.mkv" || files[0]["index"] != 1 {
		t.Fatalf("file = %+v, want original NZB index 1", files[0])
	}
	link, _ := files[0]["link"].(string)
	if !strings.Contains(link, hash+"/cairn/1/Example.Show.S05E02.mkv") || !strings.Contains(link, "u=user-1") {
		t.Fatalf("link = %q, want user-bound Cairn index 1", link)
	}
}

func TestWarmCacheWinsOverDirectCairn(t *testing.T) {
	hash := strings.Repeat("c", 40)
	h := directCairnHandler(t, hash)
	warmManifest, err := (manifest.Manifest{InfoHash: hash, Name: "Warm Movie", Files: []manifest.File{
		{FileName: "warm.mkv", DirectURL: "warm/object", FileSize: 1234},
	}}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	h.Store = fakeStore{manifest: warmManifest}
	name, files, ok := h.cachedFiles(context.Background(), "user-3", hash)
	if !ok || name != "Warm Movie" || len(files) != 1 {
		t.Fatalf("warm result: ok=%v name=%q files=%+v", ok, name, files)
	}
	link, _ := files[0]["link"].(string)
	if strings.Contains(link, "/cairn/") || !strings.Contains(link, "warm/object") {
		t.Errorf("warm cache did not win: %q", link)
	}
}

func TestCrossNodeWarmCacheWinsOverDirectCairn(t *testing.T) {
	hash := strings.Repeat("d", 40)
	h := directCairnHandler(t, hash)
	h.CachedJobs = fakeCachedLookup{hash: {
		InfoHash: hash, Name: "Remote Warm Movie", Status: jobs.StatusComplete, Node: "box2", FileSize: 4321,
		Files: []jobs.File{{Index: 0, Name: "remote-warm.mkv", Size: 4321, Key: "blobs/remote-warm"}},
	}}

	data := h.magnetData(context.Background(), &jobs.Job{
		ID: "job-remote", UserID: "user-4", InfoHash: hash, Name: "Evicted", Status: jobs.StatusEvicted,
	})
	files, _ := data["files"].([]map[string]any)
	if data["status"] != "downloaded" || data["name"] != "Remote Warm Movie" || len(files) != 1 {
		t.Fatalf("resolved item = %+v", data)
	}
	link, _ := files[0]["link"].(string)
	if strings.Contains(link, "/cairn/") || !strings.Contains(link, "sign://box2/blobs/remote-warm") {
		t.Errorf("cross-node warm cache did not win: %q", link)
	}
}

func TestCairnBackedJobIsNotMistakenForWarmNode(t *testing.T) {
	hash := strings.Repeat("e", 40)
	h := directCairnHandler(t, hash)
	h.CachedJobs = fakeCachedLookup{hash: {
		InfoHash: hash, Name: "Cairn Record", Status: jobs.StatusComplete,
		Files: []jobs.File{{Index: 0, Name: "movie.mkv", Size: 100, Key: cairn.StreamPath(hash, 0, "movie.mkv")}},
	}}

	if cached, ok := h.nodeJobFiles(context.Background(), hash); ok {
		t.Fatalf("Cairn-backed job resolved as warm node: %+v", cached)
	}
}
