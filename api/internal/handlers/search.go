package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	tnp "github.com/torrin-app/torrent-name-parser"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/mediainfo"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user.PlanID == "free" || user.Recurrence == "days" || !user.ExpiresAt.After(time.Now()) {
		web.WriteError(w, 403, "the search API requires an active monthly, yearly, or lifetime plan")
		return
	}

	imdb := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("imdb")), "tt"), "TT")
	titles := r.URL.Query()["title"]
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	if imdb == "" && len(titles) == 0 {
		web.WriteError(w, 400, "imdb (e.g. tt0816692) or title is required")
		return
	}
	wantEpisode := season > 0 && episode > 0

	seen := map[string]bool{}
	out := []searchResult{}
	add := func(j *jobs.Job) {
		if j.InfoHash == "" || seen[j.InfoHash] {
			return
		}
		var files []searchFile
		for _, f := range j.Files {
			if f.Name == "" {
				continue
			}
			if wantEpisode && !fileMatchesEpisode(f.Name, season, episode) {
				continue
			}
			files = append(files, searchFile{FileName: f.Name, Index: f.Index, Size: f.Size, MediaInfo: f.MediaInfo})
		}
		if len(files) == 0 {
			return
		}
		seen[j.InfoHash] = true
		out = append(out, searchResult{
			Name:     j.Name,
			InfoHash: j.InfoHash,
			Size:     j.FileSize,
			Magnet:   magnet.Build(j.InfoHash, ""),
			Cached:   true,
			Files:    files,
		})
	}

	if imdb != "" {
		cached, err := s.JobsPG.ListByIMDB(r.Context(), imdb)
		if err != nil {
			web.WriteError(w, 500, "search failed")
			return
		}
		for _, j := range cached {
			add(j)
		}
	}

	triedNorm := map[string]bool{}
	for _, t := range titles {
		norm := jobs.NormTitle(t)
		if norm == "" || triedNorm[norm] {
			continue
		}
		triedNorm[norm] = true
		matches, err := s.JobsPG.ListByTitleNorm(r.Context(), norm)
		if err != nil {
			web.WriteError(w, 500, "search failed")
			return
		}
		for _, j := range matches {
			if seen[j.InfoHash] {
				continue
			}
			if !wantEpisode && year > 0 && !yearMatches(j.Name, year) {
				continue
			}
			add(j)
		}
	}

	web.WriteJSON(w, 200, map[string]any{"results": out, "count": len(out)})
}

type searchFile struct {
	FileName  string          `json:"file_name"`
	Index     int             `json:"index"`
	Size      int64           `json:"size,omitempty"`
	MediaInfo *mediainfo.Info `json:"media_info,omitempty"`
}

type searchResult struct {
	Name     string       `json:"name"`
	InfoHash string       `json:"info_hash"`
	Size     int64        `json:"size"`
	Magnet   string       `json:"magnet"`
	Cached   bool         `json:"cached"`
	Files    []searchFile `json:"files"`
}

func fileMatchesEpisode(fileName string, season, episode int) bool {
	info, err := tnp.ParseName(strings.ReplaceAll(fileName, ".", " "))
	if err != nil || info.Episode != episode {
		return false
	}
	if len(info.Seasons) == 0 {
		return season == 1
	}
	for _, sn := range info.Seasons {
		if sn == season {
			return true
		}
	}
	return false
}

func yearMatches(releaseName string, year int) bool {
	info, err := tnp.ParseName(strings.ReplaceAll(releaseName, ".", " "))
	if err != nil || info.Year == 0 {
		return true
	}
	return info.Year == year
}
