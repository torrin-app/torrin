package ytdlp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArgsProxy(t *testing.T) {
	noProxy := &Runner{}
	if got := noProxy.args("-J", "url"); len(got) != 2 || got[0] != "-J" {
		t.Errorf("no proxy: got %v", got)
	}
	withProxy := &Runner{proxy: "http://gluetun:8888"}
	got := withProxy.args("-J", "url")
	if len(got) != 4 || got[0] != "--proxy" || got[1] != "http://gluetun:8888" || got[2] != "-J" {
		t.Errorf("with proxy: got %v", got)
	}
}

func TestNewRunnerDefaults(t *testing.T) {
	r := NewRunner(nil, nil, nil, nil, "/scratch", "", "", "")
	if r.bin != "yt-dlp" {
		t.Errorf("bin default = %q, want yt-dlp", r.bin)
	}
	wantFormat := "bv*[vcodec^=avc1]+ba[acodec^=mp4a]/bv*[vcodec^=avc1]+ba/b[vcodec^=avc1]/bv*+ba/b"
	if r.format != wantFormat {
		t.Errorf("format default = %q, want %q", r.format, wantFormat)
	}
	r2 := NewRunner(nil, nil, nil, nil, "/scratch", "/usr/bin/yt-dlp", "", "best")
	if r2.bin != "/usr/bin/yt-dlp" || r2.format != "best" {
		t.Errorf("overrides not honored: bin=%q format=%q", r2.bin, r2.format)
	}
}

func TestCollectVideos(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, size int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("The.Movie.2026.mp4", 2048)
	write("clip.mkv", 1024)
	write("notes.txt", 10)
	write("cover.jpg", 10)
	if err := os.Mkdir(filepath.Join(dir, "subdir.mp4"), 0o755); err != nil {
		t.Fatal(err)
	}

	files := collectVideos(dir)
	if len(files) != 2 {
		t.Fatalf("got %d video files, want 2: %+v", len(files), files)
	}
	byName := map[string]int64{}
	for _, f := range files {
		byName[f.Name] = f.Size
		if f.Path != filepath.Join(dir, f.Name) {
			t.Errorf("bad path for %q: %q", f.Name, f.Path)
		}
	}
	if byName["The.Movie.2026.mp4"] != 2048 || byName["clip.mkv"] != 1024 {
		t.Errorf("wrong files/sizes: %v", byName)
	}
}

func TestParseMeta(t *testing.T) {
	m, err := parseMeta([]byte(`{"title":"Some Video","filesize":123,"filesize_approx":999,"is_live":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Some Video" || m.Size != 123 || m.IsLive {
		t.Errorf("got %+v", m)
	}

	approx, err := parseMeta([]byte(`{"title":"No Exact","filesize_approx":777}`))
	if err != nil {
		t.Fatal(err)
	}
	if approx.Size != 777 {
		t.Errorf("size fallback = %d, want 777", approx.Size)
	}

	live, err := parseMeta([]byte(`{"title":"Stream","is_live":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !live.IsLive {
		t.Error("expected IsLive true")
	}

	merged, err := parseMeta([]byte(`{"title":"DASH","filesize":117526,"requested_formats":[{"filesize":223779},{"filesize":252182}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if merged.Size != 475961 {
		t.Errorf("requested_formats sum = %d, want 475961 (video+audio, not top-level filesize)", merged.Size)
	}

	approxRF, err := parseMeta([]byte(`{"title":"x","requested_formats":[{"filesize_approx":100},{"filesize_approx":200}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if approxRF.Size != 300 {
		t.Errorf("requested_formats approx sum = %d, want 300", approxRF.Size)
	}

	hls, err := parseMeta([]byte(`{"title":"HLS","tbr":8000,"duration":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	if hls.Size != 1_000_000_000 {
		t.Errorf("bitrate estimate = %d, want 1000000000 (8000kbps*1000s)", hls.Size)
	}

	hlsRF, err := parseMeta([]byte(`{"title":"HLS","duration":1000,"requested_formats":[{"tbr":6000},{"tbr":2000}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if hlsRF.Size != 1_000_000_000 {
		t.Errorf("summed-tbr estimate = %d, want 1000000000", hlsRF.Size)
	}

	filesizeWins, err := parseMeta([]byte(`{"title":"x","filesize":500,"tbr":8000,"duration":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	if filesizeWins.Size != 500 {
		t.Errorf("real filesize should win over estimate, got %d", filesizeWins.Size)
	}

	if _, err := parseMeta([]byte(`not json`)); err == nil {
		t.Error("expected error on bad json")
	}
}

func TestYtdlpReason(t *testing.T) {
	cases := []struct{ in, want string }{
		{"WARNING: fallback\nERROR: Unsupported URL: https://frdl.my/a.mkv.html", "Unsupported URL: https://frdl.my/a.mkv.html"},
		{"ERROR: [youtube] 00000000000: This video is unavailable", "This video is unavailable"},
		{"ERROR: [vimeo] 000000000: Unable to download webpage: HTTP Error 404: Not Found (caused by <HTTPError 404: Not Found>)", "Unable to download webpage: HTTP Error 404: Not Found"},
		{"ERROR: [generic] Movie.mkv: Some error; Please report this issue on https://github.com/yt-dlp/yt-dlp/issues", "Some error"},
		{"no error line here", ""},
	}
	for _, c := range cases {
		if got := ytdlpReason(c.in); got != c.want {
			t.Errorf("ytdlpReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseMetaHasVideo(t *testing.T) {
	audio, _ := parseMeta([]byte(`{"title":"song","vcodec":"none","duration":180,"tbr":128}`))
	if audio.HasVideo {
		t.Error("audio-only (vcodec none) should be HasVideo=false")
	}
	merged, _ := parseMeta([]byte(`{"title":"clip","requested_formats":[{"vcodec":"avc1.42","tbr":1000},{"vcodec":"none","tbr":128}]}`))
	if !merged.HasVideo {
		t.Error("video+audio merge should be HasVideo=true")
	}
	single, _ := parseMeta([]byte(`{"title":"clip","vcodec":"vp9","duration":60,"tbr":2000}`))
	if !single.HasVideo {
		t.Error("top-level video codec should be HasVideo=true")
	}
	archive, _ := parseMeta([]byte(`{"title":"doc","width":995,"height":576,"ext":"m4v","duration":7392,"filesize":962741625}`))
	if !archive.HasVideo {
		t.Error("null-codec video with real dimensions (archive.org) should be HasVideo=true")
	}
	extOnly, _ := parseMeta([]byte(`{"title":"clip","ext":"mp4"}`))
	if !extOnly.HasVideo {
		t.Error("null-codec video with a video extension should be HasVideo=true")
	}
	audioExt, _ := parseMeta([]byte(`{"title":"song","ext":"mp3","duration":180}`))
	if audioExt.HasVideo {
		t.Error("null-codec audio (no dims, non-video ext) should be HasVideo=false")
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"001-Show.mp4.mp4": "001-Show.mp4",
		"Movie.mkv.mkv":    "Movie.mkv",
		"Episode.mp4":      "Episode.mp4",
		"file.mp4.mkv":     "file.mp4.mkv",
		"noext":            "noext",
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParsePlaylist(t *testing.T) {
	pm, _ := parseMeta([]byte(`{"_type":"playlist","title":"NaruCannon","entries":[{},{}]}`))
	if !pm.IsPlaylist {
		t.Error("_type playlist should set IsPlaylist")
	}
	pl, err := parsePlaylist([]byte(`{"title":"Coll","entries":[
		{"filesize":100,"height":480,"ext":"mp4"},
		{"filesize":200,"height":480,"ext":"mp4"},
		{"filesize_approx":50,"height":480,"ext":"mp4"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if pl.Size != 350 {
		t.Errorf("total size = %d, want 350", pl.Size)
	}
	if pl.Count != 3 {
		t.Errorf("count = %d, want 3", pl.Count)
	}
	if !pl.HasVideo || !pl.IsPlaylist {
		t.Errorf("playlist with video entries: HasVideo=%v IsPlaylist=%v", pl.HasVideo, pl.IsPlaylist)
	}
}
