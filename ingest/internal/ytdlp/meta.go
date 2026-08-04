package ytdlp

import (
	"encoding/json"
	"fmt"
)

type meta struct {
	Title    string
	Size     int64
	IsLive   bool
	HasVideo bool
}

func parseMeta(out []byte) (*meta, error) {
	var j struct {
		Title            string  `json:"title"`
		Filesize         int64   `json:"filesize"`
		FilesizeApprox   int64   `json:"filesize_approx"`
		Duration         float64 `json:"duration"`
		Tbr              float64 `json:"tbr"`
		IsLive           bool    `json:"is_live"`
		Vcodec           string  `json:"vcodec"`
		RequestedFormats []struct {
			Filesize       int64   `json:"filesize"`
			FilesizeApprox int64   `json:"filesize_approx"`
			Tbr            float64 `json:"tbr"`
			Vcodec         string  `json:"vcodec"`
		} `json:"requested_formats"`
	}
	if err := json.Unmarshal(out, &j); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	hasVideo := hasVideoCodec(j.Vcodec)
	for _, f := range j.RequestedFormats {
		if hasVideoCodec(f.Vcodec) {
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

func hasVideoCodec(v string) bool { return v != "" && v != "none" }

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
