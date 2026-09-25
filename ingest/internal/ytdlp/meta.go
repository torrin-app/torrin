package ytdlp

import (
	"encoding/json"
	"fmt"

	"github.com/torrin-app/torrin/shared/video"
)

type meta struct {
	Title      string
	Size       int64
	IsLive     bool
	HasVideo   bool
	IsPlaylist bool
	Count      int
}

func parseMeta(out []byte) (*meta, error) {
	var j struct {
		Type             string            `json:"_type"`
		Title            string            `json:"title"`
		Filesize         int64             `json:"filesize"`
		FilesizeApprox   int64             `json:"filesize_approx"`
		Duration         float64           `json:"duration"`
		Tbr              float64           `json:"tbr"`
		IsLive           bool              `json:"is_live"`
		Vcodec           string            `json:"vcodec"`
		Width            int               `json:"width"`
		Height           int               `json:"height"`
		Ext              string            `json:"ext"`
		Entries          []json.RawMessage `json:"entries"`
		RequestedFormats []struct {
			Filesize       int64   `json:"filesize"`
			FilesizeApprox int64   `json:"filesize_approx"`
			Tbr            float64 `json:"tbr"`
			Vcodec         string  `json:"vcodec"`
			Width          int     `json:"width"`
			Height         int     `json:"height"`
		} `json:"requested_formats"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	if j.Type == "playlist" || len(j.Entries) > 0 {
		return &meta{Title: j.Title, IsPlaylist: true}, nil
	}
	hasVideo := looksVideo(j.Vcodec, j.Width, j.Height, j.Ext)
	for _, f := range j.RequestedFormats {
		if looksVideo(f.Vcodec, f.Width, f.Height, "") {
			hasVideo = true
		}
	}
	size := fileSize(j.Filesize, j.FilesizeApprox)
	var sum int64
	for _, f := range j.RequestedFormats {
		sum += fileSize(f.Filesize, f.FilesizeApprox)
	}
	if sum > 0 {
		size = sum
	}
	if size == 0 {
		tbr := j.Tbr
		var sumTbr float64
		for _, f := range j.RequestedFormats {
			sumTbr += f.Tbr
		}
		if sumTbr > 0 {
			tbr = sumTbr
		}
		size = bitrateSize(tbr, j.Duration)
	}
	return &meta{Title: j.Title, Size: size, IsLive: j.IsLive, HasVideo: hasVideo}, nil
}

func parsePlaylist(out []byte) (*meta, error) {
	var j struct {
		Title   string `json:"title"`
		Entries []struct {
			Filesize       int64  `json:"filesize"`
			FilesizeApprox int64  `json:"filesize_approx"`
			Vcodec         string `json:"vcodec"`
			Width          int    `json:"width"`
			Height         int    `json:"height"`
			Ext            string `json:"ext"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("parse playlist: %w", err)
	}
	var total int64
	hasVideo := false
	for _, e := range j.Entries {
		total += fileSize(e.Filesize, e.FilesizeApprox)
		if looksVideo(e.Vcodec, e.Width, e.Height, e.Ext) {
			hasVideo = true
		}
	}
	return &meta{Title: j.Title, Size: total, HasVideo: hasVideo, IsPlaylist: true, Count: len(j.Entries)}, nil
}

func looksVideo(vcodec string, width, height int, ext string) bool {
	if vcodec != "" && vcodec != "none" {
		return true
	}
	if width > 0 || height > 0 {
		return true
	}
	return ext != "" && video.IsVideo("x."+ext)
}

func fileSize(exact, approx int64) int64 {
	if exact > 0 {
		return exact
	}
	return approx
}

func bitrateSize(tbrKbps, durationSec float64) int64 {
	if tbrKbps <= 0 || durationSec <= 0 {
		return 0
	}
	return int64(tbrKbps * 1000 / 8 * durationSec)
}
