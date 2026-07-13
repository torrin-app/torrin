package postproc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFirstSevenZipVolumeSplit(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "rel.7z.001")
	touch(t, dir, "rel.7z.002")
	touch(t, dir, "rel.7z")
	if got := filepath.Base(firstSevenZipVolume(dir)); got != "rel.7z.001" {
		t.Errorf("got %q, want rel.7z.001", got)
	}
}

func TestFirstSevenZipVolumeSingle(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "rel.7z")
	if got := filepath.Base(firstSevenZipVolume(dir)); got != "rel.7z" {
		t.Errorf("got %q, want rel.7z", got)
	}
}

func TestFirstSevenZipVolumeByMagic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AbCdEf123456"), sevenZipMagic, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(firstSevenZipVolume(dir)); got != "AbCdEf123456" {
		t.Errorf("got %q, want AbCdEf123456", got)
	}
}

func TestFirstSevenZipVolumeNone(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "movie.mkv")
	touch(t, dir, "notes.txt")
	if got := firstSevenZipVolume(dir); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtract7zRoundTrip(t *testing.T) {
	bin := sevenZipBinary()
	if bin == "" {
		t.Skip("no 7z binary available")
	}
	src := t.TempDir()
	content := []byte("fake mkv payload")
	if err := os.WriteFile(filepath.Join(src, "movie.mkv"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(src, "rel.7z")
	create := exec.Command(bin, "a", arch, "movie.mkv")
	create.Dir = src
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create 7z: %v\n%s", err, out)
	}

	out := t.TempDir()
	if err := extract7z(arch, out, nil); err != nil {
		t.Fatalf("extract7z: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(out, "movie.mkv"))
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
}

func TestProcessSevenZipEndToEnd(t *testing.T) {
	if sevenZipBinary() == "" {
		t.Skip("no 7z binary available")
	}
	src := t.TempDir()
	content := bytes.Repeat([]byte("v"), 4096)
	if err := os.WriteFile(filepath.Join(src, "inner.mkv"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(src, "obf.7z")
	create := exec.Command(sevenZipBinary(), "a", arch, "inner.mkv")
	create.Dir = src
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create 7z: %v\n%s", err, out)
	}

	work := t.TempDir()
	if err := os.Rename(arch, filepath.Join(work, "obf.7z")); err != nil {
		t.Fatal(err)
	}
	files, err := Process(work, nil, "The.Movie.2021.1080p")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if files[0].Name != "The.Movie.2021.1080p.mkv" {
		t.Errorf("named %q, want The.Movie.2021.1080p.mkv", files[0].Name)
	}
}
