package detect

import (
	"bytes"
	"strings"

	"github.com/torrin-app/torrin/shared/magnet"
)

type Kind int

const (
	Unknown Kind = iota
	Magnet
	InfoHash
	Torrent
	NZB
	URL
)

func Detect(data []byte, filename string) Kind {
	if isTorrent(data, filename) {
		return Torrent
	}
	if isNZB(data, filename) {
		return NZB
	}
	s := strings.TrimSpace(string(data))
	switch {
	case strings.HasPrefix(strings.ToLower(s), "magnet:"):
		return Magnet
	case isHTTPURL(s):
		return URL
	case magnet.Valid(s):
		return InfoHash
	}
	return Unknown
}

func isHTTPURL(s string) bool {
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isTorrent(data []byte, filename string) bool {
	if hasExt(filename, ".torrent") {
		return true
	}
	return len(data) > 0 && data[0] == 'd' && bytes.Contains(window(data, 8192), []byte("4:info"))
}

func isNZB(data []byte, filename string) bool {
	if hasExt(filename, ".nzb") {
		return true
	}
	return bytes.Contains(bytes.ToLower(window(data, 4096)), []byte("<nzb"))
}

func hasExt(filename, ext string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ext)
}

func window(data []byte, n int) []byte {
	if len(data) > n {
		return data[:n]
	}
	return data
}
