package stremthru

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type fakeCairnRepository struct {
	hash string
	name string
	nzb  []byte
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
