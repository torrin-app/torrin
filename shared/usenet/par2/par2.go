package par2

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"io"
	"path/filepath"
)

var Magic = []byte("PAR2\x00PKT")

var fileDescType = []byte("PAR 2.0\x00FileDesc")

func FileDescs(data []byte, out map[string]string) {
	for i := 0; i+64 <= len(data); {
		if !bytes.Equal(data[i:i+8], Magic) {
			i++
			continue
		}
		length := binary.LittleEndian.Uint64(data[i+8 : i+16])
		if length < 64 || uint64(i)+length > uint64(len(data)) {
			break
		}
		if bytes.Equal(data[i+48:i+64], fileDescType) {
			if body := data[i+64 : i+int(length)]; len(body) >= 56 {
				h := hex.EncodeToString(body[32:48])
				if name := string(bytes.TrimRight(body[56:], "\x00")); name != "" {
					out[h] = filepath.Base(name)
				}
			}
		}
		i += int(length)
	}
}

func Hash16k(r io.Reader) string {
	h := md5.New()
	if _, err := io.CopyN(h, r, 16384); err != nil && err != io.EOF {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func IsPar2(data []byte) bool {
	return len(data) >= 8 && bytes.Equal(data[:8], Magic)
}
