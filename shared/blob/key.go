package blob

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const sampleSize = 1 << 20

func ContentKey(path string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	var sz [8]byte
	binary.BigEndian.PutUint64(sz[:], uint64(size))
	h.Write(sz[:])

	if size <= 2*sampleSize {
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return "b_" + hex.EncodeToString(h.Sum(nil)), nil
	}

	if _, err := io.CopyN(h, f, sampleSize); err != nil {
		return "", err
	}
	if _, err := f.Seek(size-sampleSize, io.SeekStart); err != nil {
		return "", err
	}
	if _, err := io.CopyN(h, f, sampleSize); err != nil {
		return "", err
	}
	return "b_" + hex.EncodeToString(h.Sum(nil)), nil
}

func StorageKey(contentKey string) string {
	return fmt.Sprintf("blobs/%s", contentKey)
}
