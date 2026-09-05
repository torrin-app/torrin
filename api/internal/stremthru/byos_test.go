package stremthru

import (
	"context"
	"encoding/json"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/stremioid"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBYOS struct {
	object *jobs.BYOSObject
	user   string
}

func (f *fakeBYOS) BYOSByHashes(_ context.Context, user string, _ []string) (map[string]*jobs.BYOSObject, error) {
	f.user = user
	return map[string]*jobs.BYOSObject{f.object.InfoHash: f.object}, nil
}

func TestBYOSCheckAndPlaybackSelectSamePrivateEpisode(t *testing.T) {
	hash := strings.Repeat("a", 40)
	object := &jobs.BYOSObject{UserID: "owner", InfoHash: hash, Name: "Show S02", Files: []jobs.File{{Index: 3, Name: "Show.S02E07E08.mkv", Size: 100, Enc: true}}}
	lookup := &fakeBYOS{object: object}
	h := New(Deps{Store: fakeStore{missing: true, err: context.DeadlineExceeded}, BYOS: lookup})
	for _, sid := range []string{"tt123:2:8", "tt123:2:9"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v0/store/magnets/check?magnet="+hash+"&sid="+sid, nil)
		h.checkMagnets(w, r, &auth.User{ID: "owner"})
		var body struct {
			Data struct {
				Items []struct {
					Status  string
					Private bool
					Files   []map[string]any
				}
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		item := body.Data.Items[0]
		playback := h.magnetDataForTarget(context.Background(), &jobs.Job{UserID: "owner", InfoHash: hash, Status: jobs.StatusEvicted}, stremioid.Parse(sid))
		files := playback["files"].([]map[string]any)
		if strings.HasSuffix(sid, ":8") {
			if item.Status != "cached" || !item.Private || len(item.Files) != 1 || len(files) != 1 || playback["status"] != "downloaded" {
				t.Fatalf("check %+v playback %+v", item, playback)
			}
			link := files[0]["link"].(string)
			for _, part := range []string{"u=owner", "byos=1", "bk=", "file_3", "enc=1"} {
				if !strings.Contains(link, part) {
					t.Fatalf("missing %s in %s", part, link)
				}
			}
			if files[0]["stream_source"] != "cache" || files[0]["episode_match"] != sid || files[0]["release_name"] != "Show S02" {
				t.Fatalf("missing scope/source %+v", files[0])
			}
		} else if item.Status != "unknown" || len(item.Files) != 0 || len(files) != 0 || playback["status"] == "downloaded" {
			t.Fatalf("wrong episode ready %+v %+v", item, playback)
		}
	}
	if lookup.user != "owner" {
		t.Fatalf("lookup user %s", lookup.user)
	}
}

func TestBYOSNeverUsesAnotherUsersObject(t *testing.T) {
	hash := strings.Repeat("b", 40)
	h := New(Deps{BYOS: &fakeBYOS{object: &jobs.BYOSObject{UserID: "someone-else", InfoHash: hash}}})
	if got := h.privateCopies(context.Background(), "owner", []string{hash}); len(got) != 0 {
		t.Fatalf("cross-account object %+v", got)
	}
}

func TestBYOSMoviePreservesSelectedVideo(t *testing.T) {
	hash := strings.Repeat("c", 40)
	h := New(Deps{Store: fakeStore{missing: true, err: context.DeadlineExceeded}, BYOS: &fakeBYOS{object: &jobs.BYOSObject{UserID: "owner", InfoHash: hash, Name: "Movie", Files: []jobs.File{{Index: 2, Name: "movie.MKV", Size: 100}, {Index: 4, Name: "movie.srt", Size: 10}}}}})
	data := h.magnetDataForTarget(context.Background(), &jobs.Job{UserID: "owner", InfoHash: hash, Status: jobs.StatusEvicted}, stremioid.Parse("tt123"))
	files := data["files"].([]map[string]any)
	if data["status"] != "downloaded" || len(files) != 1 || files[0]["index"] != 2 || files[0]["name"] != "movie.MKV" {
		t.Fatalf("movie data %+v", data)
	}
}
