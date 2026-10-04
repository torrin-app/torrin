package postproc

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFirstRARVolumeNewStyle(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "rel.part01.rar")
	touch(t, dir, "rel.part02.rar")
	if got := filepath.Base(firstRARVolume(dir)); got != "rel.part01.rar" {
		t.Errorf("got %q, want rel.part01.rar", got)
	}
}

func TestFirstRARVolumeOldStyle(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "rel.rar")
	touch(t, dir, "rel.r00") // continuation, not matched by *.rar
	if got := filepath.Base(firstRARVolume(dir)); got != "rel.rar" {
		t.Errorf("got %q, want rel.rar", got)
	}
}

func TestPasswordCandidates(t *testing.T) {
	got := PasswordCandidates("metapw", "Movie.2020 {{namepw}}", "Other password=eqpw")
	want := []string{"metapw", "namepw", "eqpw"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
	// empties and dupes dropped
	if c := PasswordCandidates("", "no password here", "dup {{x}}", "dup2 {{x}}"); len(c) != 1 || c[0] != "x" {
		t.Errorf("dedupe failed: %v", c)
	}
}

func TestUnrarArgs(t *testing.T) {
	got := unrarArgs("/s/rel.part01.rar", "/out", "")
	want := []string{"e", "-o+", "-y", "-p-", "/s/rel.part01.rar", "/out/"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
	withPw := unrarArgs("/s/a.rar", "/out", "secret")
	if withPw[3] != "-psecret" {
		t.Errorf("password arg = %q, want -psecret", withPw[3])
	}
}

func TestCollectVideos(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "movie.mkv")
	touch(t, dir, "readme.txt")
	touch(t, dir, "sample.nfo")
	got := collectVideos(dir)
	if len(got) != 1 || got[0].Name != "movie.mkv" {
		t.Errorf("got %+v, want only movie.mkv", got)
	}
}

func TestIsSample(t *testing.T) {
	samples := []string{
		"lanterns.2026.s01e07.internal.dv.2160p.web.h265-cakes-sample.mkv",
		"Sample.mkv",
		"movie-proof.mkv",
		"rel.sample.mkv",
	}
	for _, n := range samples {
		if !isSample("/x/"+n, n) {
			t.Errorf("%q should be a sample", n)
		}
	}
	notSamples := []string{
		"Lanterns.2026.S01E07.iNTERNAL.DV.2160p.WEB.H265-CAKES.mkv",
		"Free.Samples.2012.1080p.mkv", // "samples" not a bare token
		"resample.mkv",                // substring, not token
		"The.Proofreader.mkv",
	}
	for _, n := range notSamples {
		if isSample("/x/"+n, n) {
			t.Errorf("%q should not be a sample", n)
		}
	}
	if !isSample("/x/Sample/movie.mkv", "movie.mkv") {
		t.Error("file inside a Sample/ folder should be a sample")
	}
}

func TestCollectVideosSkipsSample(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "The.Movie.2160p.WEB.H265.mkv")
	touch(t, dir, "the.movie-sample.mkv")
	got := collectVideos(dir)
	if len(got) != 1 || got[0].Name != "The.Movie.2160p.WEB.H265.mkv" {
		t.Fatalf("expected only the real video, got %+v", got)
	}
}

func TestUndersized(t *testing.T) {
	full := []File{{Size: 6_500_000_000}}
	if Undersized(full, 7_000_000_000) {
		t.Error("a near-full result should not be flagged undersized")
	}
	sample := []File{{Size: 167_000_000}}
	if !Undersized(sample, 7_000_000_000) {
		t.Error("a 167MB result from a 7GB nzb should be flagged undersized")
	}
	if Undersized(sample, 0) {
		t.Error("unknown expected size should never flag undersized")
	}
}
