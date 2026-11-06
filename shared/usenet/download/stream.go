package download

import (
	"strings"

	"github.com/torrin-app/torrin/shared/usenet/nzb"
	"github.com/torrin-app/torrin/shared/video"
)

func Streamable(n *nzb.NZB) (nzb.File, bool) {
	var vid nzb.File
	found := 0
	for _, f := range n.Files {
		name := strings.ToLower(FileName(f))
		if strings.Contains(name, ".par2") {
			continue
		}
		if isArchive(name) {
			return nzb.File{}, false
		}
		if isOptional(name) {
			continue
		}
		if video.IsVideo(name) {
			vid = f
			found++
		}
	}
	if found == 1 {
		return vid, true
	}
	return nzb.File{}, false
}

func isArchive(name string) bool {
	if strings.HasSuffix(name, ".rar") || strings.HasSuffix(name, ".7z") ||
		strings.HasSuffix(name, ".zip") || strings.Contains(name, ".par2") {
		return true
	}
	if n := len(name); n >= 4 && name[n-4] == '.' {
		ext := name[n-3:]
		if ext[0] == 'r' && isDigit(ext[1]) && isDigit(ext[2]) {
			return true
		}
		if isDigit(ext[0]) && isDigit(ext[1]) && isDigit(ext[2]) {
			return true
		}
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
