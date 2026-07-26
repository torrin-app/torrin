package crypto

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/minio/sio"
)

const darePlainPkg = 64 * 1024

type Stream struct {
	key    []byte
	encPkg int64
}

func NewStream(hexKey string) (*Stream, error) {
	if hexKey == "" {
		return nil, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("storage key must be 32 bytes (64 hex chars), got %d", len(key))
	}
	encPkg, err := sio.EncryptedSize(darePlainPkg)
	if err != nil {
		return nil, err
	}
	return &Stream{key: key, encPkg: int64(encPkg)}, nil
}

func (s *Stream) config(seq uint32) sio.Config {
	return sio.Config{Key: s.key, SequenceNumber: seq}
}

func (s *Stream) EncryptWriter(dst io.Writer) (io.WriteCloser, error) {
	return sio.EncryptWriter(dst, s.config(0))
}

func (s *Stream) EncryptReader(src io.Reader) (io.Reader, error) {
	return sio.EncryptReader(src, s.config(0))
}

func (s *Stream) EncryptedSize(plain int64) (int64, error) {
	n, err := sio.EncryptedSize(uint64(plain))
	return int64(n), err
}

func (s *Stream) PlainSize(enc int64) (int64, error) {
	n, err := sio.DecryptedSize(uint64(enc))
	return int64(n), err
}

type Range struct {
	EncStart int64
	EncEnd   int64
	Seq      uint32
	Skip     int64
	Length   int64
}

func (s *Stream) PlanRange(start, end, plainTotal int64) (Range, error) {
	if start < 0 || end > plainTotal || start >= end {
		return Range{}, fmt.Errorf("bad range %d-%d of %d", start, end, plainTotal)
	}
	firstPkg := start / darePlainPkg
	lastPkg := (end - 1) / darePlainPkg
	encEndPlain := (lastPkg + 1) * darePlainPkg
	if encEndPlain > plainTotal {
		encEndPlain = plainTotal
	}
	encEnd, err := s.EncryptedSize(encEndPlain)
	if err != nil {
		return Range{}, err
	}
	return Range{
		EncStart: firstPkg * s.encPkg,
		EncEnd:   encEnd,
		Seq:      uint32(firstPkg),
		Skip:     start - firstPkg*darePlainPkg,
		Length:   end - start,
	}, nil
}

func (s *Stream) DecryptRange(dst io.Writer, enc io.Reader, r Range) error {
	dr, err := sio.DecryptReader(enc, s.config(r.Seq))
	if err != nil {
		return err
	}
	if r.Skip > 0 {
		if _, err := io.CopyN(io.Discard, dr, r.Skip); err != nil {
			return err
		}
	}
	_, err = io.CopyN(dst, dr, r.Length)
	return err
}

func (s *Stream) DecryptAll(dst io.Writer, enc io.Reader) error {
	_, err := sio.Decrypt(dst, enc, s.config(0))
	return err
}
