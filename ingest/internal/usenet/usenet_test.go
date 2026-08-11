package usenet

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/torrin-app/torrin/ingest/internal/publish"
	"github.com/torrin-app/torrin/shared/crypto"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func TestReasonSurfacesRealError(t *testing.T) {
	r := &Runner{scratch: "/scratch"}
	job := &jobs.Job{InfoHash: "abc"}

	err := r.reason(job, fmt.Errorf("download: rename /scratch/abc/File.part /scratch/abc/File: no such file or directory"))
	if got := failure.Message(err); got != "download: rename File.part File: no such file or directory" {
		t.Fatalf("got %q", got)
	}

	if got := failure.Message(r.reason(job, failure.Newf("too_large", "nzb too large"))); got != "nzb too large" {
		t.Fatalf("typed passthrough got %q", got)
	}
}

type nameRepo struct {
	jobs.Repository
	rows []*jobs.Job
}

func (r *nameRepo) ListByInfoHash(context.Context, string) ([]*jobs.Job, error) {
	return r.rows, nil
}

var matroskaMagic = []byte{0x1A, 0x45, 0xDF, 0xA3}

func TestDecryptInPlaceRecoversVideo(t *testing.T) {
	cipher, err := crypto.NewStream(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	plain := append(append([]byte{}, matroskaMagic...), bytes.Repeat([]byte("matroska-payload"), 5000)...)

	path := filepath.Join(t.TempDir(), "d8e5f029953273e5adeb") // obfuscated cairn name, no extension
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	er, err := cipher.EncryptReader(bytes.NewReader(plain))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(f, er); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// as Cairn stores it: encrypted bytes must NOT look like a video (that's the bug on restore)
	if enc, _ := os.ReadFile(path); bytes.HasPrefix(enc, matroskaMagic) {
		t.Fatal("encrypted blob unexpectedly starts with matroska magic")
	}

	if err := decryptInPlace(path, cipher); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: %d vs %d bytes", len(got), len(plain))
	}
	if !bytes.HasPrefix(got, matroskaMagic) {
		t.Error("decrypted blob is not a matroska video")
	}
}

func TestDecryptInPlaceRejectsCorrupt(t *testing.T) {
	cipher, err := crypto.NewStream(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "truncated")
	if err := os.WriteFile(path, []byte("not a valid dare stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := decryptInPlace(path, cipher); err == nil {
		t.Error("expected decrypt error on corrupt/incomplete blob")
	}
}

func TestRestoreNamesRecoversFromCleanRow(t *testing.T) {
	parsed := &nzb.NZB{Files: []nzb.File{
		{Filename: "aaaaaaaaaaaaaaaaaaaa.mp4"},
		{Filename: "bbbbbbbbbbbbbbbbbbbb.mp4"},
	}}
	hexRow := &jobs.Job{Files: []jobs.File{
		{Name: "aaaaaaaaaaaaaaaaaaaa.mp4"},
		{Name: "bbbbbbbbbbbbbbbbbbbb.mp4"},
	}}
	cleanRow := &jobs.Job{Files: []jobs.File{
		{Name: "Show.S01E01.mp4"},
		{Name: "Show.S01E02.mp4"},
	}}
	r := &Runner{repo: &nameRepo{rows: []*jobs.Job{hexRow, cleanRow}}}
	files := []publish.File{
		{Name: "bbbbbbbbbbbbbbbbbbbb.mp4"},
		{Name: "aaaaaaaaaaaaaaaaaaaa.mp4"},
	}
	r.restoreNames(context.Background(), "hash", parsed, files)
	if files[0].Name != "Show.S01E02.mp4" || files[1].Name != "Show.S01E01.mp4" {
		t.Fatalf("names not restored by disk-name mapping: %+v", files)
	}
}

func TestRestoreNamesSkipsOnCountMismatch(t *testing.T) {
	parsed := &nzb.NZB{Files: []nzb.File{{Filename: "aaaaaaaaaaaaaaaaaaaa.mp4"}}}
	cleanRow := &jobs.Job{Files: []jobs.File{{Name: "A.mp4"}, {Name: "B.mp4"}}}
	r := &Runner{repo: &nameRepo{rows: []*jobs.Job{cleanRow}}}
	files := []publish.File{{Name: "aaaaaaaaaaaaaaaaaaaa.mp4"}}
	r.restoreNames(context.Background(), "h", parsed, files)
	if files[0].Name != "aaaaaaaaaaaaaaaaaaaa.mp4" {
		t.Fatalf("should not remap on count mismatch: %s", files[0].Name)
	}
}

func TestIsBlobName(t *testing.T) {
	for _, n := range []string{"d8e5f029953273e5adeb.mp4", "006cdc58147823088268.mp4", "sub/0a2f25fae55a6f594f6c.mkv"} {
		if !isBlobName(n) {
			t.Errorf("%q should be a blob name", n)
		}
	}
	for _, n := range []string{"Show.S01E01.mp4", "movie.mkv", "d8e5f029953273e5adeb1.mp4", "g8e5f029953273e5adeb.mp4"} {
		if isBlobName(n) {
			t.Errorf("%q should not be a blob name", n)
		}
	}
}
