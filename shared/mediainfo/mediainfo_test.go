package mediainfo

import "testing"

const sample = `{
  "streams": [
    {"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,"color_transfer":"smpte2084",
     "side_data_list":[{"side_data_type":"DOVI configuration record"}]},
    {"codec_type":"audio","codec_name":"truehd","channels":8,"tags":{"language":"eng","title":"Atmos"}},
    {"codec_type":"audio","codec_name":"ac3","channels":6,"tags":{"language":"spa"}},
    {"codec_type":"subtitle","tags":{"language":"eng","title":"SDH"}},
    {"codec_type":"subtitle","tags":{"language":"fre"}}
  ],
  "format": {"bit_rate":"52000000","duration":"7261.5"}
}`

func TestParse(t *testing.T) {
	info, err := parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 3840 || info.Height != 2160 || info.VideoCodec != "hevc" {
		t.Errorf("video: %dx%d %s", info.Width, info.Height, info.VideoCodec)
	}
	if info.Resolution != "2160p" {
		t.Errorf("resolution = %q, want 2160p", info.Resolution)
	}
	if info.HDR != "DV" {
		t.Errorf("hdr = %q, want DV (dolby vision side data wins)", info.HDR)
	}
	if info.Bitrate != 52_000_000 || info.DurationSec != 7261.5 {
		t.Errorf("bitrate=%d dur=%v", info.Bitrate, info.DurationSec)
	}
	if len(info.Audio) != 2 || info.Audio[0].Codec != "truehd" || info.Audio[0].Channels != 8 || info.Audio[0].Language != "eng" {
		t.Errorf("audio: %+v", info.Audio)
	}
	if len(info.Subtitles) != 2 || info.Subtitles[0].Language != "eng" || info.Subtitles[1].Language != "fre" {
		t.Errorf("subs: %+v", info.Subtitles)
	}
}

func TestParseSkipsCoverArt(t *testing.T) {
	// A poster image muxed as an mjpeg attached_pic must not be read as the video.
	// The real Black Panther file: real hevc + a flagged poster + an UNFLAGGED
	// mjpeg thumbnail. Both mjpeg streams must be ignored as images.
	const withCover = `{
	  "streams": [
	    {"codec_type":"video","codec_name":"hevc","width":1920,"height":1040},
	    {"codec_type":"video","codec_name":"mjpeg","width":2000,"height":3000,"disposition":{"attached_pic":1}},
	    {"codec_type":"video","codec_name":"mjpeg","width":640,"height":268},
	    {"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"eng"}}
	  ],
	  "format": {"bit_rate":"2000000","duration":"8100"}
	}`
	info, err := parse([]byte(withCover))
	if err != nil {
		t.Fatal(err)
	}
	if info.VideoCodec != "hevc" || info.Height != 1040 {
		t.Errorf("picked an image stream instead of video: %s %dp", info.VideoCodec, info.Height)
	}
}

func TestParseHDR10AndResolutions(t *testing.T) {
	info, _ := parse([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"color_transfer":"smpte2084"}],"format":{}}`))
	if info.HDR != "HDR10" {
		t.Errorf("hdr = %q, want HDR10", info.HDR)
	}
	if info.Resolution != "1080p" {
		t.Errorf("resolution = %q, want 1080p", info.Resolution)
	}
	if r := resolution(0, 0); r != "" {
		t.Errorf("zero resolution = %q, want empty", r)
	}
}

func TestResolutionWidthAware(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{3840, 2160, "2160p"},
		{3840, 1600, "2160p"},
		{2560, 1440, "1440p"},
		{1920, 1080, "1080p"},
		{1920, 804, "1080p"},
		{1440, 1080, "1080p"},
		{1280, 720, "720p"},
		{1280, 536, "720p"},
		{1024, 576, "576p"},
		{854, 480, "480p"},
		{640, 480, "480p"},
	}
	for _, c := range cases {
		if got := resolution(c.w, c.h); got != c.want {
			t.Errorf("resolution(%d,%d) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestPlayable(t *testing.T) {
	if Playable(nil) {
		t.Error("nil probe is not playable")
	}
	if Playable(&Info{}) {
		t.Error("no video dimensions is not playable (truncated/corrupt)")
	}
	if Playable(&Info{Width: 1920}) {
		t.Error("width without height is not playable")
	}
	if !Playable(&Info{Width: 1920, Height: 1080}) {
		t.Error("a real video with dimensions should be playable")
	}
}
