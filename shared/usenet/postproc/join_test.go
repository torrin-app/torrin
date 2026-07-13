package postproc

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestJoinSplitsContiguous(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "movie.mkv.001", "aaa")
	writeFile(t, dir, "movie.mkv.002", "bbb")
	writeFile(t, dir, "movie.mkv.003", "ccc")
	if !joinSplits(dir) {
		t.Fatal("expected join")
	}
	got, err := os.ReadFile(filepath.Join(dir, "movie.mkv"))
	if err != nil || string(got) != "aaabbbccc" {
		t.Fatalf("joined = %q (err %v), want aaabbbccc", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "movie.mkv.001")); err == nil {
		t.Error("parts should be removed after join")
	}
}

func TestJoinSplitsGap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "movie.mkv.001", "aaa")
	writeFile(t, dir, "movie.mkv.003", "ccc")
	if joinSplits(dir) {
		t.Fatal("should not join an incomplete sequence")
	}
	if _, err := os.Stat(filepath.Join(dir, "movie.mkv")); err == nil {
		t.Error("should not have created joined file from a gapped sequence")
	}
}

func TestJoinSplitsSkipIfExists(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "movie.mkv", "original")
	writeFile(t, dir, "movie.mkv.001", "aaa")
	writeFile(t, dir, "movie.mkv.002", "bbb")
	if joinSplits(dir) {
		t.Fatal("should skip when target already exists")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "movie.mkv"))
	if string(got) != "original" {
		t.Errorf("clobbered existing file: %q", got)
	}
}

func TestJoinTS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clip.001.ts", "x")
	writeFile(t, dir, "clip.002.ts", "y")
	if !joinTS(dir) {
		t.Fatal("expected ts join")
	}
	got, err := os.ReadFile(filepath.Join(dir, "clip.ts"))
	if err != nil || string(got) != "xy" {
		t.Fatalf("joined ts = %q (err %v), want xy", got, err)
	}
}

func TestTsBaseSeq(t *testing.T) {
	cases := []struct {
		name string
		base string
		seq  int
		ok   bool
	}{
		{"clip.001.ts", "clip", 1, true},
		{"clip.002.ts", "clip", 2, true},
		{"00001.ts", "joined", 1, true},
		{"movie.mkv", "", 0, false},
		{"show.ts", "", 0, false},
	}
	for _, c := range cases {
		base, seq, ok := tsBaseSeq(c.name)
		if ok != c.ok || (ok && (base != c.base || seq != c.seq)) {
			t.Errorf("tsBaseSeq(%q) = (%q,%d,%v), want (%q,%d,%v)", c.name, base, seq, ok, c.base, c.seq, c.ok)
		}
	}
}

func TestFirstZip(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "rel.zip")
	if got := filepath.Base(firstZip(dir)); got != "rel.zip" {
		t.Errorf("got %q, want rel.zip", got)
	}
	empty := t.TempDir()
	touch(t, empty, "movie.mkv")
	if firstZip(empty) != "" {
		t.Error("firstZip should be empty when no zip present")
	}
}
