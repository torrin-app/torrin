package episodes

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/jobs"
)

type testCatalog []cinemeta.Episode

func (c testCatalog) Episodes(context.Context, string) ([]cinemeta.Episode, error) { return c, nil }

func TestSelectFileCoverage(t *testing.T) {
	tests := []struct {
		name, pack, file string
		season, episode  int
		want             bool
	}{
		{"single", "Show S02", "Show.S02E03.mkv", 2, 3, true},
		{"range", "Show S02", "Show.S02E03-E05.mkv", 2, 4, true},
		{"adjacent", "Show S02", "Show.S02E03E04.mkv", 2, 4, true},
		{"range miss", "Show S02", "Show.S02E03-E05.mkv", 2, 6, false},
		{"multi season basename", "Show S01-S03", "Show.S01-S03/Show.S02E03.mkv", 2, 3, true},
		{"season folder", "Show S01-S03", "Show/Season 2/E03.mkv", 2, 3, true},
		{"windows folder", "Show S01-S03", `Show\Season 2\E03.mkv`, 2, 3, true},
		{"pack season", "Show S02", "E03.mkv", 2, 3, true},
		{"wrong season", "Show S01-S03", "Show.S01E03.mkv", 2, 3, false},
		{"opaque pack", "Show S02", "video.mkv", 2, 3, false},
		{"subtitle", "Show S02", "Show.S02E03.srt", 2, 3, false},
		{"sample", "Show S02", "Show.S02E03.sample.mkv", 2, 3, false},
		{"movie", "Movie", "Movie.2024.MKV", 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &jobs.Job{Name: tt.pack}
			r := New(nil)
			got := r.Select(context.Background(), "", j, []jobs.File{{Index: 7, Name: tt.file}}, tt.season, tt.episode)
			if (len(got) > 0) != tt.want {
				t.Fatalf("files=%+v, want match %v", got, tt.want)
			}
			if len(got) > 0 && got[0].Index != 7 {
				t.Fatal("lost stable index")
			}
		})
	}
}

func TestCatalogCombinedStoriesOverrideLiteralNumber(t *testing.T) {
	r := New(testCatalog{{Season: 2, Number: 7, Name: "The Lost Little Duck"}, {Season: 2, Number: 8, Name: "The Amazing Brass Band"}})
	files := []jobs.File{{Index: 3, Name: "Show.S02E04.The.Lost.Little.Duck.-.The.Amazing.Brass.Band.mkv"}}
	for _, e := range []int{7, 8} {
		got := r.Select(context.Background(), "tt123", nil, files, 2, e)
		if len(got) != 1 || got[0].Index != 3 {
			t.Fatalf("E%d got %+v", e, got)
		}
	}
	if got := r.Select(context.Background(), "tt123", nil, files, 2, 4); len(got) != 0 {
		t.Fatalf("wrong story matched literal E4: %+v", got)
	}
	if len(files[0].Episodes) != 0 {
		t.Fatal("mutated input")
	}
}

func TestAmbiguousCatalogDoesNotOverride(t *testing.T) {
	r := New(testCatalog{{Season: 2, Number: 7, Name: "A Repeated Story"}, {Season: 2, Number: 8, Name: "A Repeated Story"}, {Season: 2, Number: 9, Name: "Pilot"}})
	files := []jobs.File{{Name: "Show.S02E04.A.Repeated.Story.Pilot.mkv"}}
	if got := r.Select(context.Background(), "tt123", nil, files, 2, 4); len(got) != 1 {
		t.Fatalf("lost literal match %+v", got)
	}
	for _, e := range []int{7, 8, 9} {
		if got := r.Select(context.Background(), "tt123", nil, files, 2, e); len(got) != 0 {
			t.Fatalf("ambiguous E%d matched", e)
		}
	}
}

type countingCatalog struct{ calls atomic.Int32 }

func (c *countingCatalog) Episodes(context.Context, string) ([]cinemeta.Episode, error) {
	c.calls.Add(1)
	return []cinemeta.Episode{{Season: 2, Number: 8, Name: "The Amazing Brass Band"}}, nil
}
func TestConcurrentRequestsShareCatalog(t *testing.T) {
	catalog := &countingCatalog{}
	resolver := New(catalog)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			files := resolver.Select(context.Background(), "tt123", nil, []jobs.File{{Name: "Show.S02E04.The.Amazing.Brass.Band.mkv"}}, 2, 8)
			if len(files) != 1 {
				t.Errorf("files=%+v", files)
			}
		}()
	}
	wg.Wait()
	if got := catalog.calls.Load(); got != 1 {
		t.Fatalf("catalog fetched %d times", got)
	}
}

func TestAssessDoesNotConfuseUnmappedFilesWithMissingEpisodes(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []jobs.File
		want  string
	}{
		{"combined match", []jobs.File{{Name: "Show.S02E07E08.mkv"}}, "match"},
		{"known copy miss", []jobs.File{{Name: "Show.S02E07.mkv"}}, "no_match"},
		{"ambiguous copy", []jobs.File{{Name: "Show.S02E07.mkv"}, {Name: "video.mkv"}}, "unknown"},
		{"no file list", nil, "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, got := New(nil).Assess(context.Background(), "", nil, tt.files, 2, 8)
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
