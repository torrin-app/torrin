package server

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"
)

func assembleFull(t *testing.T, l *zipLayout, contents [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := l.writeRange(&buf, 0, l.total-1, func(idx int, off, length int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(contents[idx][off : off+length])), nil
	})
	if err != nil {
		t.Fatalf("writeRange: %v", err)
	}
	return buf.Bytes()
}

func TestZipLayoutValidArchive(t *testing.T) {
	contents := [][]byte{
		[]byte("hello world"),
		bytes.Repeat([]byte("A"), 5000),
		[]byte("third file body"),
	}
	names := []string{"a.txt", "folder sub/b.bin", "Résumé.mkv"}
	entries := make([]zipEntry, len(contents))
	for i := range contents {
		entries[i] = zipEntry{name: names[i], size: int64(len(contents[i])), crc: crc32.ChecksumIEEE(contents[i])}
	}

	l := buildZipLayout(entries)
	full := assembleFull(t, l, contents)
	if int64(len(full)) != l.total {
		t.Fatalf("assembled %d bytes, layout.total=%d", len(full), l.total)
	}

	zr, err := zip.NewReader(bytes.NewReader(full), int64(len(full)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if len(zr.File) != len(entries) {
		t.Fatalf("read %d files, want %d", len(zr.File), len(entries))
	}
	for i, zf := range zr.File {
		if zf.Name != names[i] {
			t.Errorf("name[%d]=%q want %q", i, zf.Name, names[i])
		}
		rc, err := zf.Open()
		if err != nil {
			t.Fatalf("open %s: %v", zf.Name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s (crc check): %v", zf.Name, err)
		}
		if !bytes.Equal(got, contents[i]) {
			t.Errorf("content[%d] mismatch", i)
		}
	}
}

func TestZipLayoutRangeConsistency(t *testing.T) {
	contents := [][]byte{[]byte("0123456789"), bytes.Repeat([]byte("xy"), 300)}
	entries := make([]zipEntry, len(contents))
	for i := range contents {
		entries[i] = zipEntry{name: string(rune('a'+i)) + ".dat", size: int64(len(contents[i])), crc: crc32.ChecksumIEEE(contents[i])}
	}
	l := buildZipLayout(entries)
	full := assembleFull(t, l, contents)

	fetch := func(idx int, off, length int64) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(contents[idx][off : off+length])), nil
	}
	spans := [][2]int64{{0, 0}, {0, l.total - 1}, {5, 40}, {l.total - 10, l.total - 1}, {100, 200}}
	for _, s := range spans {
		var buf bytes.Buffer
		if err := l.writeRange(&buf, s[0], s[1], fetch); err != nil {
			t.Fatalf("range %v: %v", s, err)
		}
		if !bytes.Equal(buf.Bytes(), full[s[0]:s[1]+1]) {
			t.Errorf("range %v differs from full slice", s)
		}
	}
}

func TestZipLayoutZip64(t *testing.T) {
	big := int64(0x100000000)
	e := zipEntry{name: "big.mkv", size: big, crc: 0x12345678}

	lh := localHeader(e)
	if binary.LittleEndian.Uint16(lh[4:6]) != 45 {
		t.Errorf("local version=%d want 45", binary.LittleEndian.Uint16(lh[4:6]))
	}
	if binary.LittleEndian.Uint32(lh[18:22]) != zip64Marker || binary.LittleEndian.Uint32(lh[22:26]) != zip64Marker {
		t.Error("local 32-bit sizes are not zip64 markers")
	}
	nl := int(binary.LittleEndian.Uint16(lh[26:28]))
	el := int(binary.LittleEndian.Uint16(lh[28:30]))
	lex := lh[30+nl : 30+nl+el]
	if el != 20 || binary.LittleEndian.Uint16(lex[0:2]) != zip64ID || binary.LittleEndian.Uint64(lex[4:12]) != uint64(big) {
		t.Error("local zip64 extra field malformed")
	}

	ch := centralHeader(e, 0)
	cnl := int(binary.LittleEndian.Uint16(ch[28:30]))
	cel := int(binary.LittleEndian.Uint16(ch[30:32]))
	cex := ch[46+cnl : 46+cnl+cel]
	if cel != 20 || binary.LittleEndian.Uint16(cex[0:2]) != zip64ID || binary.LittleEndian.Uint64(cex[4:12]) != uint64(big) {
		t.Error("central zip64 extra field malformed")
	}

	l := buildZipLayout([]zipEntry{e})
	trailer := l.segs[len(l.segs)-1].hdr
	if !bytes.Contains(trailer, binary.LittleEndian.AppendUint32(nil, zip64EOCDSig)) {
		t.Error("trailer missing zip64 EOCD record")
	}
	if l.total != int64(len(lh))+big+int64(len(ch))+int64(len(trailer)-len(ch)) {
		t.Errorf("total=%d inconsistent", l.total)
	}
}
