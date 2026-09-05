package handlers

import (
	"github.com/torrin-app/torrin/shared/mediainfo"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestNormalizeIndexerURL(t *testing.T) {
	cases := map[string]string{
		"https://idx.com/":     "https://idx.com",
		"https://idx.com/api":  "https://idx.com",
		"https://idx.com/api/": "https://idx.com",
		"  https://idx.com  ":  "https://idx.com",
	}
	for in, want := range cases {
		if got := normalizeIndexerURL(in); got != want {
			t.Errorf("normalizeIndexerURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseCachedTarget(t *testing.T) {
	cases := []struct {
		in              string
		imdb            string
		season, episode int
	}{
		{"tt20329502", "20329502", 0, 0},
		{"tt0903747:4:5", "0903747", 4, 5},
		{"123", "123", 0, 0},
		{"", "", 0, 0},
		{"tt123:2", "123", 0, 0},
	}
	for _, c := range cases {
		imdb, season, episode := parseCachedTarget(c.in)
		if imdb != c.imdb || season != c.season || episode != c.episode {
			t.Errorf("parseCachedTarget(%q) = (%q,%d,%d), want (%q,%d,%d)", c.in, imdb, season, episode, c.imdb, c.season, c.episode)
		}
	}
}

func TestCachedStreamResultIncludesPlaybackMetadata(t *testing.T) {
	job := &jobs.Job{Name: "Example.Release", InfoHash: "content-hash"}
	stream := jobs.Stream{
		FileName:  "Example.S02E05.1080p.mkv",
		Size:      123456,
		SignedURL: "https://example.com/blob",
	}

	got := cachedStreamResult(job, stream)
	if got["name"] != job.Name || got["file_name"] != stream.FileName {
		t.Fatalf("unexpected cached stream identity: %#v", got)
	}
	if got["size"] != stream.Size || got["info_hash"] != job.InfoHash {
		t.Fatalf("missing cached stream playback metadata: %#v", got)
	}
	if got["signed_url"] != stream.SignedURL {
		t.Fatalf("unexpected cached stream URL: %#v", got)
	}
}

func TestSignSelectedFileKeepsIndexAndMediaInfo(t *testing.T) {
	s := &Server{Deps: Deps{Store: &fakeStore{}}}
	info := &mediainfo.Info{Resolution: "1080p"}
	j := &jobs.Job{InfoHash: strings.Repeat("a", 40), Files: []jobs.File{{Index: 7, Name: "Show.S02E08.mkv", Size: 123, MediaInfo: info}}}
	streams := s.signStreams(j, httptest.NewRequest("GET", "/", nil))
	if len(streams) != 1 || streams[0].Index != 7 || streams[0].MediaInfo != info {
		t.Fatalf("streams %+v", streams)
	}
	if !strings.Contains(streams[0].SignedURL, "/file_7/") {
		t.Fatalf("lost storage index: %s", streams[0].SignedURL)
	}
}
