package foldername

import (
	"testing"

	"github.com/torrin-app/torrin/shared/jobs"
)

func TestMapDisambiguatesCollisions(t *testing.T) {
	f := []jobs.File{{Name: "a.mkv", Size: 1}}
	list := []*jobs.Job{
		{InfoHash: "bbbbbbbbbbb", Name: "The Movie 2020", Status: jobs.StatusComplete, Files: f},
		{InfoHash: "aaaaaaaaaaa", Name: "The Movie 2020", Status: jobs.StatusComplete, Files: f},
		{InfoHash: "ccccccccccc", Name: "Other", Status: jobs.StatusComplete, Files: f},
		{InfoHash: "ddddddddddd", Name: "Incomplete", Status: jobs.StatusDownloading, Files: f},
		{InfoHash: "eeeeeeeeeee", Name: "NoFiles", Status: jobs.StatusComplete},
	}
	m := Map(list)
	if m["aaaaaaaaaaa"] != "The Movie 2020" {
		t.Errorf("first (sorted) collision should keep clean name, got %q", m["aaaaaaaaaaa"])
	}
	if m["bbbbbbbbbbb"] != "The Movie 2020 [bbbbbbbb]" {
		t.Errorf("second collision should get shorthash suffix, got %q", m["bbbbbbbbbbb"])
	}
	if m["ccccccccccc"] != "Other" {
		t.Errorf("non-colliding = %q", m["ccccccccccc"])
	}
	if _, ok := m["ddddddddddd"]; ok {
		t.Error("incomplete job should be excluded")
	}
	if _, ok := m["eeeeeeeeeee"]; ok {
		t.Error("complete job with no files should be excluded")
	}
}
