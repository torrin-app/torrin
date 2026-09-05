package stremthru

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/episodes"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/stremioid"
	"github.com/torrin-app/torrin/shared/torrentclaw"
)

type storyCatalog struct{}

func (storyCatalog) Episodes(context.Context, string) ([]cinemeta.Episode, error) {
	return []cinemeta.Episode{{Season: 2, Number: 7, Name: "The Lost Little Duck"}, {Season: 2, Number: 8, Name: "The Amazing Brass Band"}}, nil
}

func TestEpisodeReadinessAgreesWithPlayback(t *testing.T) {
	hash := strings.Repeat("f", 40)
	filename := "Show.S02E04.The.Lost.Little.Duck.-.The.Amazing.Brass.Band.mkv"
	man, _ := (manifest.Manifest{InfoHash: hash, Name: "Show S02", Files: []manifest.File{{FileName: "Show.S02E01.mkv", FileSize: 100}, {FileName: filename, FileSize: 200}}}).Marshal()
	j := &jobs.Job{InfoHash: hash, Name: "Show S02", Status: jobs.StatusComplete, Files: []jobs.File{{Name: "Show.S02E01.mkv"}, {Index: 1, Name: filename}}}
	h := New(Deps{Store: fakeStore{manifest: man}, CachedJobs: fakeCachedLookup{hash: j}, EpisodeResolver: episodes.New(storyCatalog{})})
	for _, sid := range []string{"tt123:2:7", "tt123:2:8", "tt123:2:4", "tt123:2:99"} {
		t.Run(sid, func(t *testing.T) {
			want := strings.HasSuffix(sid, ":7") || strings.HasSuffix(sid, ":8")
			r := httptest.NewRequest("GET", "/v0/store/magnets/check?magnet="+hash+"&sid="+sid, nil)
			w := httptest.NewRecorder()
			h.checkMagnets(w, r, &auth.User{ID: "u", PlanID: "standard"})
			var resp struct {
				Data struct {
					Items []struct {
						Status, Reason string
						Files          []map[string]any
					}
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if len(resp.Data.Items) != 1 {
				t.Fatalf("response %s", w.Body.String())
			}
			item := resp.Data.Items[0]
			playback := h.magnetDataForTarget(context.Background(), j, stremioid.Parse(sid))
			files := playback["files"].([]map[string]any)
			if want {
				if item.Status != "cached" || len(item.Files) != 1 || playback["status"] != "downloaded" || len(files) != 1 {
					t.Fatalf("check %+v playback %+v", item, playback)
				}
				if item.Files[0]["release_name"] != "Show S02" || files[0]["release_name"] != "Show S02" || files[0]["name"] != filename {
					t.Fatalf("lost original release or selected filename: check %+v playback %+v", item, playback)
				}
				if item.Files[0]["episode_match"] != sid || files[0]["episode_match"] != sid || files[0]["index"] != 1 {
					t.Fatalf("lost target/index %+v %+v", item, playback)
				}
			} else if item.Status != "unknown" || item.Reason != "episode_not_found" || len(files) != 0 || playback["status"] == "downloaded" {
				t.Fatalf("false readiness %+v %+v", item, playback)
			}
		})
	}
}

func checkEpisodeItem(t *testing.T, h *Handler, hash string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h.checkMagnets(w, httptest.NewRequest("GET", "/v0/store/magnets/check?magnet="+hash+"&sid=tt123:2:8", nil), &auth.User{ID: "owner"})
	var response struct {
		Data struct{ Items []map[string]any }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != 1 {
		t.Fatalf("response %s", w.Body.String())
	}
	return response.Data.Items[0]
}

func TestWarmSubsetDoesNotShadowBYOSEpisode(t *testing.T) {
	hash := strings.Repeat("d", 40)
	man, _ := (manifest.Manifest{InfoHash: hash, Name: "Show S02", Files: []manifest.File{{FileName: "Show.S02E07.mkv", FileSize: 100}}}).Marshal()
	j := &jobs.Job{UserID: "owner", InfoHash: hash, Name: "Show S02", Status: jobs.StatusComplete}
	h := New(Deps{Store: fakeStore{manifest: man}, CachedJobs: fakeCachedLookup{hash: j}, BYOS: &fakeBYOS{object: &jobs.BYOSObject{UserID: "owner", InfoHash: hash, Name: "Show S02", Files: []jobs.File{{Index: 3, Name: "Show.S02E08.mkv", Size: 200}}}}})
	item := checkEpisodeItem(t, h, hash)
	playback := h.magnetDataForTarget(context.Background(), j, stremioid.Parse("tt123:2:8"))
	if item["status"] != "cached" || item["episode_status"] != "match" || item["private"] != true || playback["private"] != true || playback["status"] != "downloaded" {
		t.Fatalf("check %+v playback %+v", item, playback)
	}
}

type cacheCheckTransport func(*http.Request) (*http.Response, error)

func (f cacheCheckTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStoredCopyMissRemainsScopedAndAllowsUpstream(t *testing.T) {
	hash := strings.Repeat("e", 40)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = cacheCheckTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"cached":{"` + hash + `":true}}`)), Header: make(http.Header)}, nil
	})
	for _, tt := range []struct {
		name, file, status, match string
		upstream                  bool
	}{
		{"missing from available copy", "Show.S02E07.mkv", "unknown", "no_match", false},
		{"unknown filename", "video.mkv", "unknown", "unknown", false},
		{"partial copy with upstream", "Show.S02E07.mkv", "acceleratable", "unknown", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			man, _ := (manifest.Manifest{InfoHash: hash, Name: "Show S02", Files: []manifest.File{{FileName: tt.file, FileSize: 100}}}).Marshal()
			h := New(Deps{Store: fakeStore{manifest: man}, CachedJobs: fakeCachedLookup{hash: &jobs.Job{Name: "Show S02"}}})
			if tt.upstream {
				h.TC, h.SysADKey = torrentclaw.New("test"), "test"
			}
			item := checkEpisodeItem(t, h, hash)
			if item["status"] != tt.status || item["episode_status"] != tt.match || item["episode_sid"] != "tt123:2:8" {
				t.Fatalf("item %+v", item)
			}
			if tt.match == "no_match" && item["episode_scope"] != "available_files" {
				t.Fatalf("unscoped rejection %+v", item)
			}
		})
	}
}
