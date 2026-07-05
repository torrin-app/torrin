package postproc

import (
	"crypto/md5"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestDeobfuscateByPar2(t *testing.T) {
	dir := t.TempDir()

	content := []byte("the real video payload, delivered under an obfuscated name")
	obf := "9f3a1c0bd7e4random.dat"
	if err := os.WriteFile(filepath.Join(dir, obf), content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(content) // file < 16k → whole-file hash, matching par2 semantics
	real := "Silo.S01E01.1080p.WEB.h264-EDITH.mkv"

	if err := os.WriteFile(filepath.Join(dir, "recovery.par2"), fileDescPar2(sum[:], real, int64(len(content))), 0o644); err != nil {
		t.Fatal(err)
	}

	deobfuscateByPar2(dir)

	if _, err := os.Stat(filepath.Join(dir, real)); err != nil {
		t.Fatalf("expected obfuscated file renamed to %q, got: %v", real, err)
	}
	if _, err := os.Stat(filepath.Join(dir, obf)); err == nil {
		t.Fatalf("obfuscated name %q should be gone after rename", obf)
	}
}

func TestParPacketBoundsSafety(t *testing.T) {
	// truncated / garbage input must not panic and must yield nothing
	out := map[string]string{}
	parsePar2FileDescs([]byte("PAR2\x00PKT\xff\xff\xff\xff\xff\xff\xff\xffshort"), out)
	if len(out) != 0 {
		t.Fatalf("truncated packet should yield no names, got %v", out)
	}
}

func fileDescPar2(md5_16k []byte, name string, length int64) []byte {
	body := make([]byte, 56)
	copy(body[32:48], md5_16k)
	binary.LittleEndian.PutUint64(body[48:56], uint64(length))
	nameB := []byte(name)
	for len(nameB)%4 != 0 {
		nameB = append(nameB, 0)
	}
	body = append(body, nameB...)

	pktLen := 64 + len(body)
	l := make([]byte, 8)
	binary.LittleEndian.PutUint64(l, uint64(pktLen))
	pkt := append([]byte("PAR2\x00PKT"), l...)
	pkt = append(pkt, make([]byte, 16)...) // packet md5 (unused)
	pkt = append(pkt, make([]byte, 16)...) // set id (unused)
	pkt = append(pkt, []byte("PAR 2.0\x00FileDesc")...)
	return append(pkt, body...)
}
