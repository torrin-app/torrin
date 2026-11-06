package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func TestApplyUsenetStreamLink(t *testing.T) {
	hash := strings.Repeat("a", 40)
	nzbData := nzb.Generate([]nzb.OutFile{{Name: "movie.mkv", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "m1", Number: 1, Bytes: 1000}}}})
	store := &fakeStore{bytesByKey: map[string][]byte{nzb.StorageKey(hash): nzbData}}
	s := &Server{Deps: Deps{Store: store}}
	streaming := &auth.User{ID: "u1", UsenetStreaming: true}

	t.Run("streaming usenet download gets a ?usenet=1 link", func(t *testing.T) {
		job := &jobs.Job{InfoHash: hash, Source: jobs.SourceUsenet, Status: jobs.StatusDownloading}
		s.applyUsenetStreamLink(context.Background(), streaming, job)
		if len(job.StreamURLs) != 1 || !strings.Contains(job.StreamURLs[0].SignedURL, "usenet=1") {
			t.Fatalf("want a ?usenet=1 link, got %+v", job.StreamURLs)
		}
		if job.StreamURLs[0].FileName != "movie.mkv" {
			t.Errorf("filename = %q, want movie.mkv", job.StreamURLs[0].FileName)
		}
	})

	t.Run("toggle off -> no link", func(t *testing.T) {
		job := &jobs.Job{InfoHash: hash, Source: jobs.SourceUsenet, Status: jobs.StatusDownloading}
		s.applyUsenetStreamLink(context.Background(), &auth.User{ID: "u1"}, job)
		if len(job.StreamURLs) != 0 {
			t.Errorf("streaming-off user should get no link, got %+v", job.StreamURLs)
		}
	})

	t.Run("non-usenet -> no link", func(t *testing.T) {
		job := &jobs.Job{InfoHash: hash, Source: jobs.SourceTorrent, Status: jobs.StatusDownloading}
		s.applyUsenetStreamLink(context.Background(), streaming, job)
		if len(job.StreamURLs) != 0 {
			t.Errorf("non-usenet job should get no link, got %+v", job.StreamURLs)
		}
	})
}

func TestMintUsenetStreamLink(t *testing.T) {
	s := &Server{Deps: Deps{Store: &fakeStore{bytesByKey: map[string][]byte{}}}}

	t.Run("streamable mkv -> usenet link", func(t *testing.T) {
		data := nzb.Generate([]nzb.OutFile{{Name: "movie.mkv", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "m1", Number: 1, Bytes: 1000}}}})
		parsed, err := nzb.ParseBytes(data)
		if err != nil {
			t.Fatal(err)
		}
		stream, ok := s.mintUsenetStreamLink(context.Background(), "u1", data, parsed)
		if !ok {
			t.Fatal("want ok for a streamable release")
		}
		if !strings.Contains(stream.SignedURL, "usenet=1") {
			t.Errorf("link = %q, want a ?usenet=1 link", stream.SignedURL)
		}
		if stream.FileName != "movie.mkv" {
			t.Errorf("file = %q, want movie.mkv", stream.FileName)
		}
	})

	t.Run("non-video -> not ok", func(t *testing.T) {
		data := nzb.Generate([]nzb.OutFile{{Name: "readme.nfo", Group: "alt.test", Segments: []nzb.Segment{{MessageID: "m1", Number: 1, Bytes: 10}}}})
		parsed, err := nzb.ParseBytes(data)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s.mintUsenetStreamLink(context.Background(), "u1", data, parsed); ok {
			t.Error("non-video release should not be streamable")
		}
	})
}
