package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"
)

func testStream(t *testing.T) *Stream {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	s, err := NewStream(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func encrypt(t *testing.T, s *Stream, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := s.EncryptWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNewStreamDefaultOff(t *testing.T) {
	s, err := NewStream("")
	if err != nil || s != nil {
		t.Fatalf("empty key should yield nil stream, got %v %v", s, err)
	}
	if _, err := NewStream("zz"); err == nil {
		t.Fatal("bad hex should error")
	}
	if _, err := NewStream(hex.EncodeToString(make([]byte, 16))); err == nil {
		t.Fatal("short key should error")
	}
}

func TestRoundTripFull(t *testing.T) {
	s := testStream(t)
	plain := make([]byte, 200*1024)
	rand.Read(plain)
	enc := encrypt(t, s, plain)

	encSize, _ := s.EncryptedSize(int64(len(plain)))
	if int64(len(enc)) != encSize {
		t.Fatalf("EncryptedSize %d != actual %d", encSize, len(enc))
	}

	var out bytes.Buffer
	if err := s.DecryptAll(&out, bytes.NewReader(enc)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), plain) {
		t.Fatal("full round trip mismatch")
	}
}

func TestDecryptRange(t *testing.T) {
	s := testStream(t)
	total := int64(300*1024 + 777)
	plain := make([]byte, total)
	rand.Read(plain)
	enc := encrypt(t, s, plain)

	cases := []struct{ start, end int64 }{
		{0, 1},
		{0, darePlainPkg},
		{0, total},
		{100, 200},
		{darePlainPkg - 10, darePlainPkg + 10},
		{darePlainPkg, 2 * darePlainPkg},
		{darePlainPkg + 5, 3*darePlainPkg + 5},
		{total - 500, total},
		{total - 1, total},
		{123456, 250000},
	}

	for _, c := range cases {
		r, err := s.PlanRange(c.start, c.end, total)
		if err != nil {
			t.Fatalf("plan %d-%d: %v", c.start, c.end, err)
		}
		if r.EncStart < 0 || r.EncEnd > int64(len(enc)) || r.EncStart >= r.EncEnd {
			t.Fatalf("plan %d-%d bad window %+v (enc len %d)", c.start, c.end, r, len(enc))
		}
		window := enc[r.EncStart:r.EncEnd]
		var out bytes.Buffer
		if err := s.DecryptRange(&out, bytes.NewReader(window), r); err != nil {
			t.Fatalf("decrypt %d-%d: %v", c.start, c.end, err)
		}
		want := plain[c.start:c.end]
		if !bytes.Equal(out.Bytes(), want) {
			t.Fatalf("range %d-%d mismatch: got %d bytes want %d", c.start, c.end, out.Len(), len(want))
		}
	}
}

func TestPlanRangeRejectsBad(t *testing.T) {
	s := testStream(t)
	for _, c := range [][3]int64{{-1, 10, 100}, {0, 101, 100}, {50, 50, 100}, {60, 50, 100}} {
		if _, err := s.PlanRange(c[0], c[1], c[2]); err == nil {
			t.Fatalf("expected error for %v", c)
		}
	}
}

func TestTamperDetected(t *testing.T) {
	s := testStream(t)
	plain := make([]byte, 100*1024)
	rand.Read(plain)
	enc := encrypt(t, s, plain)
	enc[len(enc)/2] ^= 0xff
	err := s.DecryptAll(io.Discard, bytes.NewReader(enc))
	if err == nil {
		t.Fatal("tampered ciphertext should fail auth")
	}
}
