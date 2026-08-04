package ytdlp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line                      string
		bytes, fragIdx, fragCount int64
		ok                        bool
	}{
		{"dl:123/0/0", 123, 0, 0, true},       // byte progress
		{"dl:0/5/57", 0, 5, 57, true},         // fragmented (HLS): use fragment index/count
		{"dl: 123 / 0 / 0 ", 123, 0, 0, true}, // whitespace tolerated
		{"dl:0/NA/NA", 0, 0, 0, false},        // no bytes, no fragments
		{"dl:NA/NA/NA", 0, 0, 0, false},
		{"dl:123", 0, 0, 0, false}, // wrong field count
		{"[download] 45%", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		b, fi, fc, ok := parseProgress(c.line)
		if ok != c.ok || b != c.bytes || fi != c.fragIdx || fc != c.fragCount {
			t.Errorf("parseProgress(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)", c.line, b, fi, fc, ok, c.bytes, c.fragIdx, c.fragCount, c.ok)
		}
	}
}

func TestProgressAccumulator(t *testing.T) {
	var p progress
	steps := []struct{ in, want int64 }{
		{1000, 1000},
		{5000, 5000},
		{223779, 223779}, // video stream finishes
		{1024, 224803},   // audio stream starts: reset detected, video total banked
		{100000, 323779},
		{252182, 475961}, // audio finishes: cumulative == combined total
		{252182, 475961}, // duplicate final line: no double-count
	}
	for _, s := range steps {
		if got := p.add(s.in); got != s.want {
			t.Errorf("add(%d) = %d, want %d", s.in, got, s.want)
		}
	}
}

func TestProgressReport(t *testing.T) {
	if c, d, ok := progressReport(50, 100, 5, 100); !ok || c != 50 || d != 100 {
		t.Errorf("plausible estimate: got %d/%d ok=%v", c, d, ok)
	}
	c, d, ok := progressReport(772, 7, 5, 100)
	if !ok || c > d {
		t.Errorf("blown+frag: %d/%d ok=%v — current must not exceed denom", c, d, ok)
	}
	if d != 772*100/5 {
		t.Errorf("blown+frag denom = %d, want fragment-extrapolated", d)
	}
	if _, _, ok := progressReport(0, 0, 0, 0); ok {
		t.Error("no usable signal should report ok=false")
	}
}

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
}
