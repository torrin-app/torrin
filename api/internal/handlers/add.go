package handlers

import (
	"io"
	"net/http"
	"strings"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/detect"
)

const maxAddBytes = 20 << 20

func (s *Server) addJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAddBytes)
	data, filename, err := readAddInput(r)
	if err != nil {
		web.WriteError(w, 400, "could not read input")
		return
	}
	if len(data) == 0 {
		web.WriteError(w, 400, "paste a magnet, link or infohash, or upload a .torrent or .nzb")
		return
	}

	user := middleware.GetUser(r)
	plan := middleware.GetPlan(r)
	switch detect.Detect(data, filename) {
	case detect.Torrent:
		s.ingestTorrent(w, r, user, plan, data)
	case detect.NZB:
		s.ingestNZB(w, r, user, plan, data, strings.TrimSuffix(filename, ".nzb"), "", true)
	case detect.Magnet, detect.InfoHash, detect.URL:
		s.ingestText(w, r, strings.TrimSpace(string(data)))
	default:
		web.WriteError(w, 400, "couldn't recognize this, paste a magnet, link or infohash, or upload a .torrent or .nzb")
	}
}

func readAddInput(r *http.Request) ([]byte, string, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		file, hdr, err := r.FormFile("file")
		if err != nil {
			return nil, "", err
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		name := ""
		if hdr != nil {
			name = hdr.Filename
		}
		return data, name, err
	}
	data, err := io.ReadAll(r.Body)
	return data, "", err
}
