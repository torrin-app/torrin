package rdapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/storage"
)

func jobToRDTorrent(j *jobs.Job, store *storage.Client) map[string]any {
	return map[string]any{
		"id": j.ID, "filename": j.Name, "hash": j.InfoHash, "bytes": totalBytes(j),
		"host": "torrin.app", "split": 2048, "progress": progressPct(j.Status),
		"status": mapStatus(j.Status), "added": j.CreatedAt.Format(time.RFC3339),
		"links": buildLinks(j), "ended": j.UpdatedAt.Format(time.RFC3339),
		"speed": 0, "seeders": 0,
	}
}

func jobToRDTorrentFull(j *jobs.Job, store *storage.Client) map[string]any {
	t := jobToRDTorrent(j, store)
	files := []map[string]any{}
	for i, f := range j.Files {
		selected := 0
		for _, idx := range j.SelectedIdxs {
			if idx == i {
				selected = 1
				break
			}
		}
		files = append(files, map[string]any{"id": i + 1, "path": "/" + f.Name, "bytes": f.Size, "selected": selected})
	}
	t["files"] = files
	t["original_filename"] = j.Name
	t["original_bytes"] = totalBytes(j)
	return t
}

func buildLinks(j *jobs.Job) []string {
	links := []string{}
	if j.Status != jobs.StatusComplete {
		return links
	}
	for i, f := range j.Files {
		links = append(links, "torrin://"+manifest.Key(j.InfoHash, i, f.Name))
	}
	return links
}

func mapStatus(s jobs.Status) string {
	switch s {
	case jobs.StatusPending:
		return "magnet_conversion"
	case jobs.StatusQueued:
		return "queued"
	case jobs.StatusDownloading, jobs.StatusProcessing:
		return "downloading"
	case jobs.StatusPublishing:
		return "uploading"
	case jobs.StatusComplete:
		return "downloaded"
	case jobs.StatusFailed:
		return "error"
	default:
		return "queued"
	}
}

func progressPct(s jobs.Status) float64 {
	switch s {
	case jobs.StatusComplete:
		return 100
	case jobs.StatusDownloading, jobs.StatusProcessing, jobs.StatusPublishing:
		return 50
	default:
		return 0
	}
}

func totalBytes(j *jobs.Job) int64 {
	var t int64
	for _, f := range j.Files {
		t += f.Size
	}
	return t
}

func extractHash(m string) string { return magnet.Hash(m) }

func mimeFor(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".mkv"):
		return "video/x-matroska"
	case strings.HasSuffix(n, ".mp4"):
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}

func isValidKey(key string) bool {
	if strings.Contains(key, "..") {
		return false
	}
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 || !magnet.Valid(parts[0]) || parts[2] == "" {
		return false
	}
	n, ok := strings.CutPrefix(parts[1], "file_")
	return ok && isDigits(n)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func generateShortID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeRDError(w http.ResponseWriter, httpStatus, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]any{"error": msg, "error_code": code})
}
