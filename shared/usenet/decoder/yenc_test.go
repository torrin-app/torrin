package decoder

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"testing"
)

// payload is the decoded form of the "*+,-." body used throughout these tests:
// bytes 0..4 yEnc-encode (no escapes needed) to (b+42).
var payload = []byte{0, 1, 2, 3, 4}

func payloadCRC() uint32 { return crc32.ChecksumIEEE(payload) }

func TestDecode(t *testing.T) {
	// bytes 0..4 yEnc-encode (no escapes needed) to (b+42): '*' '+' ',' '-' '.'
	msg := "=ybegin part=1 total=1 line=128 size=5 name=test.bin\n*+,-.\n=yend size=5 part=1\n"
	r, err := Decode([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if r.Filename != "test.bin" {
		t.Errorf("filename = %q", r.Filename)
	}
	if !bytes.Equal(r.Data, []byte{0, 1, 2, 3, 4}) {
		t.Errorf("data = %v, want [0 1 2 3 4]", r.Data)
	}
}

func TestDecodeNoYenc(t *testing.T) {
	if _, err := Decode([]byte("not yenc data")); err == nil {
		t.Error("expected error for non-yenc input")
	}
}

func TestDecodeAcceptsValidPartChecksum(t *testing.T) {
	msg := fmt.Sprintf("=ybegin part=1 total=2 line=128 size=10 name=test.bin\n"+
		"=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 pcrc32=%08X\n", payloadCRC())
	r, err := Decode([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Data, payload) {
		t.Errorf("data = %v, want %v", r.Data, payload)
	}
}

func TestDecodeRejectsCorruptPart(t *testing.T) {
	// A pcrc32 that does not match the body: the article arrived, but the bytes
	// are not what was posted.
	msg := "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n" +
		"=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 pcrc32=DEADBEEF\n"
	if _, err := Decode([]byte(msg)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestDecodeAcceptsLowercaseChecksum(t *testing.T) {
	msg := fmt.Sprintf("=ybegin part=1 total=2 line=128 size=10 name=test.bin\n"+
		"=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 pcrc32=%08x\n", payloadCRC())
	if _, err := Decode([]byte(msg)); err != nil {
		t.Fatalf("lowercase pcrc32 should verify: %v", err)
	}
}

func TestDecodeVerifiesSinglePartCrc32(t *testing.T) {
	// No part= header: the bare crc32 covers the whole file, which here is the
	// only part, so it applies to these bytes.
	msg := fmt.Sprintf("=ybegin line=128 size=5 name=test.bin\n*+,-.\n=yend size=5 crc32=%08X\n", payloadCRC())
	if _, err := Decode([]byte(msg)); err != nil {
		t.Fatalf("valid single-part crc32 should verify: %v", err)
	}

	bad := "=ybegin line=128 size=5 name=test.bin\n*+,-.\n=yend size=5 crc32=DEADBEEF\n"
	if _, err := Decode([]byte(bad)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestDecodeIgnoresWholeFileCrc32OnMultipart(t *testing.T) {
	// A bare crc32 on a multipart post covers the ENTIRE file, not this part.
	// Verifying it against one part's bytes would reject every segment of every
	// multipart post, so it must be skipped. The value here is deliberately not
	// the CRC of the part body.
	msg := "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n" +
		"=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 crc32=DEADBEEF\n"
	r, err := Decode([]byte(msg))
	if err != nil {
		t.Fatalf("whole-file crc32 must not be checked against a part: %v", err)
	}
	if !bytes.Equal(r.Data, payload) {
		t.Errorf("data = %v, want %v", r.Data, payload)
	}
}

func TestDecodeReadsPcrc32NotSuffixOfCrc32(t *testing.T) {
	// "pcrc32=" ends in "crc32=", so a substring match would read the part
	// checksum as a whole-file one. On this multipart post that would make the
	// mismatching pcrc32 get skipped instead of rejected.
	msg := "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n" +
		"=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 pcrc32=DEADBEEF\n"
	if _, err := Decode([]byte(msg)); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("err = %v, want ErrChecksumMismatch", err)
	}
}

func TestDecodeToleratesAbsentOrUnusableChecksum(t *testing.T) {
	// Posters vary in what they emit; nothing to verify is not an error.
	cases := map[string]string{
		"absent":     "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1\n",
		"malformed":  "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n=ypart begin=1 end=5\n*+,-.\n=yend size=5 part=1 pcrc32=zzzz\n",
		"no trailer": "=ybegin part=1 total=2 line=128 size=10 name=test.bin\n=ypart begin=1 end=5\n*+,-.\n",
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(msg)); err != nil {
				t.Fatalf("should decode without verification: %v", err)
			}
		})
	}
}
