package video

import (
	"path/filepath"
	"strings"
)

var exts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".mov": true,
	".m2ts": true, ".mpeg": true, ".mpg": true,
	".wmv": true, ".ts": true, ".webm": true, ".m4v": true, ".flv": true,
}

func IsVideo(name string) bool {
	return exts[strings.ToLower(filepath.Ext(name))]
}

func ContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}
