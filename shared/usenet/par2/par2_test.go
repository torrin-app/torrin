package par2

import (
	"encoding/binary"
	"testing"
)

func TestFileDescsBoundsSafety(t *testing.T) {
	out := map[string]string{}
	FileDescs([]byte("PAR2\x00PKT\xff\xff\xff\xff\xff\xff\xff\xffshort"), out)
	if len(out) != 0 {
		t.Fatalf("truncated packet should yield no names, got %v", out)
	}
}

func TestFileDescsParse(t *testing.T) {
	md5 := make([]byte, 16)
	for i := range md5 {
		md5[i] = byte(i + 1)
	}
	pkt := fileDescPacket(md5, "Show.S01E01.1080p.mkv")
	out := map[string]string{}
	FileDescs(pkt, out)
	hexKey := "0102030405060708090a0b0c0d0e0f10"
	if out[hexKey] != "Show.S01E01.1080p.mkv" {
		t.Fatalf("want name for %s, got %v", hexKey, out)
	}
}

func fileDescPacket(md5_16k []byte, name string) []byte {
	body := make([]byte, 56)
	copy(body[32:48], md5_16k)
	nameB := []byte(name)
	for len(nameB)%4 != 0 {
		nameB = append(nameB, 0)
	}
	body = append(body, nameB...)
	l := make([]byte, 8)
	binary.LittleEndian.PutUint64(l, uint64(64+len(body)))
	pkt := append([]byte("PAR2\x00PKT"), l...)
	pkt = append(pkt, make([]byte, 16)...)
	pkt = append(pkt, make([]byte, 16)...)
	pkt = append(pkt, []byte("PAR 2.0\x00FileDesc")...)
	return append(pkt, body...)
}
