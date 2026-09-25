package arr

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestQbitState(t *testing.T) {
	cases := map[jobs.Status]string{
		jobs.StatusComplete:    "pausedUP",
		jobs.StatusSeeding:     "pausedUP",
		jobs.StatusDownloading: "downloading",
		jobs.StatusPublishing:  "checkingUP",
		jobs.StatusQueued:      "queuedDL",
		jobs.StatusPending:     "metaDL",
		jobs.StatusFailed:      "error",
		jobs.StatusEvicted:     "missingFiles",
	}
	for status, want := range cases {
		if got := qbitState(status); got != want {
			t.Errorf("qbitState(%s) = %s, want %s", status, got, want)
		}
	}
}

func TestSabQueueStatus(t *testing.T) {
	if sabQueueStatus(jobs.StatusQueued) != "Queued" {
		t.Error("queued should map to Queued")
	}
	if sabQueueStatus(jobs.StatusDownloading) != "Downloading" {
		t.Error("downloading should map to Downloading")
	}
}

func TestContentPath(t *testing.T) {
	j := &jobs.Job{Name: "The Show S01E01 1080p", InfoHash: "abc"}
	if got := contentPath("/downloads", folder(j)); got != "/downloads/The Show S01E01 1080p" {
		t.Errorf("contentPath = %q", got)
	}
	j2 := &jobs.Job{Name: "", InfoHash: "deadbeef"}
	if got := contentPath("/downloads/", folder(j2)); got != "/downloads/deadbeef" {
		t.Errorf("contentPath fallback = %q", got)
	}
}

func TestFolderFor(t *testing.T) {
	j := &jobs.Job{Name: "The Show", InfoHash: "abc"}
	fm := map[string]string{"abc": "The Show [abc12345]"}
	if got := folderFor(fm, j); got != "The Show [abc12345]" {
		t.Errorf("folderFor map hit = %q, want disambiguated name", got)
	}
	j2 := &jobs.Job{Name: "Other", InfoHash: "xyz"}
	if got := folderFor(fm, j2); got != "Other" {
		t.Errorf("folderFor fallback = %q, want sanitized name", got)
	}
}

func TestCatOrDefault(t *testing.T) {
	if catOrDefault("") != "*" {
		t.Error("empty category should default to *")
	}
	if catOrDefault("tv") != "tv" {
		t.Error("set category should pass through")
	}
}

func TestDistinctCategories(t *testing.T) {
	in := map[string]string{"h1": "tv", "h2": "tv", "h3": "movies", "h4": ""}
	got := distinctCategories(in)
	if len(got) != 2 {
		t.Fatalf("want 2 distinct, got %v", got)
	}
}

func TestCategoryNames(t *testing.T) {
	got := categoryNames(map[string]string{"h1": "tv", "h2": "custom-cat"})
	has := map[string]bool{}
	for _, c := range got {
		has[c] = true
	}
	for _, want := range []string{"*", "tv", "tv-sonarr", "movies", "radarr", "anime", "custom-cat"} {
		if !has[want] {
			t.Errorf("categoryNames missing %q, got %v", want, got)
		}
	}
	seen := map[string]int{}
	for _, c := range got {
		seen[c]++
		if seen[c] > 1 {
			t.Errorf("categoryNames has duplicate %q", c)
		}
	}
}

func TestTotalBytes(t *testing.T) {
	j := &jobs.Job{Files: []jobs.File{{Size: 100}, {Size: 250}}}
	if totalBytes(j) != 350 {
		t.Errorf("totalBytes = %d, want 350", totalBytes(j))
	}
}
