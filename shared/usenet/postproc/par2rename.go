package postproc

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

var fileDescType = []byte("PAR 2.0\x00FileDesc")

func deobfuscateByPar2(dir string) {
	names := par2FileNames(dir)
	if len(names) == 0 {
		return
	}
	entries, _ := os.ReadDir(dir)
	renamed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		real, ok := names[hash16k(filepath.Join(dir, e.Name()))]
		if !ok || real == "" || real == e.Name() {
			continue
		}
		target := filepath.Join(dir, real)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		if os.Rename(filepath.Join(dir, e.Name()), target) == nil {
			renamed++
		}
	}
	if renamed > 0 {
		slog.Info("postproc: deobfuscated files via par2 hashes", "dir", dir, "renamed", renamed)
	}
}

func par2FileNames(dir string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if !hasMagic(p, par2Magic) {
			continue
		}
		if data, err := os.ReadFile(p); err == nil {
			parsePar2FileDescs(data, out)
		}
	}
	return out
}

func parsePar2FileDescs(data []byte, out map[string]string) {
	for i := 0; i+64 <= len(data); {
		if !bytes.Equal(data[i:i+8], par2Magic) {
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

func hash16k(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.CopyN(h, f, 16384); err != nil && err != io.EOF {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
