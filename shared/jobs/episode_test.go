package jobs

import "testing"

func TestMatchesEpisodeFile(t *testing.T) {
	tests := []struct {
		name     string
		job      *Job
		fileName string
		season   int
		episode  int
		single   bool
		want     bool
	}{
		{name: "filename exact", job: &Job{}, fileName: "Example.Show.S05E01.mkv", season: 5, episode: 1, want: true},
		{name: "filename wrong episode", job: &Job{}, fileName: "Example.Show.S05E02.mkv", season: 5, episode: 1},
		{name: "filename wrong season", job: &Job{}, fileName: "Example.Show.S12E01.mkv", season: 5, episode: 1},
		{name: "x format", job: &Job{}, fileName: "Example Show 5x01.mkv", season: 5, episode: 1, want: true},
		{name: "multi episode range", job: &Job{}, fileName: "Example.Show.S05E01-E03.mkv", season: 5, episode: 2, want: true},
		{name: "episode range misses", job: &Job{}, fileName: "Example.Show.S05E01-E03.mkv", season: 5, episode: 4},
		{name: "special", job: &Job{}, fileName: "Example.Show.S00E02.mkv", season: 0, episode: 2, want: true},
		{name: "date is not episode", job: &Job{}, fileName: "The Ed Show 10-19-12.mp4", season: 10, episode: 19},
		{name: "season pack metadata", job: &Job{Season: 5}, fileName: "Example.Show.S05E01.mkv", season: 5, episode: 1, want: true},
		{name: "wrong pack metadata", job: &Job{Season: 12}, fileName: "Example.Show.S12E01.mkv", season: 5, episode: 1},
		{name: "exact metadata fallback single file", job: &Job{Season: 5, Episode: 1}, fileName: "opaque-video.mkv", season: 5, episode: 1, single: true, want: true},
		{name: "special metadata fallback single file", job: &Job{Season: 0, Episode: 2}, fileName: "opaque-special.mkv", season: 0, episode: 2, single: true, want: true},
		{name: "metadata fallback rejects wrong season", job: &Job{Season: 0, Episode: 2}, fileName: "opaque-special.mkv", season: 1, episode: 2, single: true},
		{name: "opaque file in a pack is not trusted", job: &Job{Season: 5, Episode: 1}, fileName: "opaque-video.mkv", season: 5, episode: 1, single: false},
		{name: "metadata does not override filename", job: &Job{Season: 5, Episode: 1}, fileName: "Example.Show.S05E02.mkv", season: 5, episode: 1},
		{name: "filename overrides stale metadata", job: &Job{Season: 12, Episode: 1}, fileName: "Example.Show.S05E01.mkv", season: 5, episode: 1, want: true},
		{name: "movie is unfiltered", job: &Job{}, fileName: "anything.mkv", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesEpisodeFile(tt.job, tt.fileName, tt.season, tt.episode, tt.single); got != tt.want {
				t.Fatalf("MatchesEpisodeFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilesForEpisodeUsesPositionWhenUnindexed(t *testing.T) {
	j := &Job{Season: 5, Files: []File{
		{Name: "Example.Show.S05E01.mkv"},
		{Name: "Example.Show.S05E02.mkv"},
	}}
	got := FilesForEpisode(j, j.Files, 5, 2)
	if len(got) != 1 {
		t.Fatalf("files = %d, want 1", len(got))
	}
	if got[0].Index != 1 {
		t.Fatalf("index = %d, want position 1", got[0].Index)
	}
}

func TestFilesForEpisodeTrustsPersistedIndex(t *testing.T) {
	j := &Job{Season: 5, Files: []File{
		{Name: "Example.Show.S05E02.mkv", Index: 3},
		{Name: "Example.Show.S05E01.mkv", Index: 0},
	}}
	got := FilesForEpisode(j, j.Files, 5, 1)
	if len(got) != 1 {
		t.Fatalf("files = %d, want 1", len(got))
	}
	if got[0].Index != 0 {
		t.Fatalf("index = %d, want persisted 0", got[0].Index)
	}
}

func TestFilesForEpisodeDoesNotFilterMovie(t *testing.T) {
	j := &Job{Files: []File{
		{Name: "Movie.2024.mkv"},
		{Name: "Movie.behind-the-scenes.mkv"},
	}}
	got := FilesForEpisode(j, j.Files, 0, 0)
	if len(got) != len(j.Files) {
		t.Fatalf("movie files = %d, want %d", len(got), len(j.Files))
	}
	if got[1].Index != 1 {
		t.Fatalf("second movie index = %d, want 1", got[1].Index)
	}
}
