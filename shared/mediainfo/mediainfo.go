package mediainfo

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

type Info struct {
	Width       int          `json:"width,omitempty"`
	Height      int          `json:"height,omitempty"`
	Resolution  string       `json:"resolution,omitempty"`
	DurationSec float64      `json:"duration_sec,omitempty"`
	Bitrate     int64        `json:"bitrate,omitempty"`
	VideoCodec  string       `json:"video_codec,omitempty"`
	HDR         string       `json:"hdr,omitempty"`
	Audio       []AudioTrack `json:"audio,omitempty"`
	Subtitles   []SubTrack   `json:"subtitles,omitempty"`
}

type AudioTrack struct {
	Codec    string `json:"codec,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
}

type SubTrack struct {
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
}

func isImageCodec(name string) bool {
	switch name {
	case "mjpeg", "mjpg", "jpeg", "png", "gif", "bmp", "webp", "tiff", "ppm":
		return true
	}
	return false
}

func resolution(w, h int) string {
	switch {
	case w == 0 && h == 0:
		return ""
	case w >= 3400 || h >= 1700:
		return "2160p"
	case w >= 2500 || h >= 1300:
		return "1440p"
	case w >= 1800 || h >= 900:
		return "1080p"
	case w >= 1200 || h >= 620:
		return "720p"
	case w >= 950 || h >= 560:
		return "576p"
	default:
		return "480p"
	}
}

func Probe(ctx context.Context, path string) (*Info, error) {
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path).Output()
	if err != nil {
		return nil, err
	}
	return parse(out)
}

func parse(b []byte) (*Info, error) {
	var raw struct {
		Format struct {
			BitRate  string `json:"bit_rate"`
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType     string `json:"codec_type"`
			CodecName     string `json:"codec_name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			Channels      int    `json:"channels"`
			BitRate       string `json:"bit_rate"`
			ColorTransfer string `json:"color_transfer"`
			Tags          struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
			SideData []struct {
				Type string `json:"side_data_type"`
			} `json:"side_data_list"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	info := &Info{}
	info.Bitrate, _ = strconv.ParseInt(raw.Format.BitRate, 10, 64)
	info.DurationSec, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if isImageCodec(s.CodecName) || (s.Width == 0 && s.Height == 0) {
				continue
			}
			info.Width, info.Height, info.VideoCodec = s.Width, s.Height, s.CodecName
			info.Resolution = resolution(s.Width, s.Height)
			if info.Bitrate == 0 {
				info.Bitrate, _ = strconv.ParseInt(s.BitRate, 10, 64)
			}
			info.HDR = hdrType(s.ColorTransfer, s.SideData)
		case "audio":
			info.Audio = append(info.Audio, AudioTrack{
				Codec: s.CodecName, Channels: s.Channels,
				Language: s.Tags.Language, Title: s.Tags.Title,
			})
		case "subtitle":
			info.Subtitles = append(info.Subtitles, SubTrack{
				Language: s.Tags.Language, Title: s.Tags.Title,
			})
		}
	}
	return info, nil
}

func hdrType(transfer string, side []struct {
	Type string `json:"side_data_type"`
}) string {
	for _, d := range side {
		t := strings.ToLower(d.Type)
		if strings.Contains(t, "dovi") || strings.Contains(t, "dolby vision") {
			return "DV"
		}
	}
	switch transfer {
	case "smpte2084":
		return "HDR10"
	case "arib-std-b67":
		return "HLG"
	}
	return ""
}
