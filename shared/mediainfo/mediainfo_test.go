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

func TestParseHDR10AndResolutions(t *testing.T) {
	info, _ := parse([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"color_transfer":"smpte2084"}],"format":{}}`))
	if info.HDR != "HDR10" {
		t.Errorf("hdr = %q, want HDR10", info.HDR)
	}
	if info.Resolution != "1080p" {
		t.Errorf("resolution = %q, want 1080p", info.Resolution)
	}
	if r := resolution(0); r != "" {
		t.Errorf("zero height resolution = %q, want empty", r)
	}
}
