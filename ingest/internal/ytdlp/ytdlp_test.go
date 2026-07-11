package ytdlp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		line string
		n    int64
		ok   bool
	}{
		{"dl:123", 123, true},
		{"dl: 123 ", 123, true},
		{"dl:0", 0, false},
		{"dl:NA", 0, false},
		{"[download] 45%", 0, false},
		{"dl:", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		n, ok := parseProgress(c.line)
		if ok != c.ok || n != c.n {
			t.Errorf("parseProgress(%q) = (%d,%v), want (%d,%v)", c.line, n, ok, c.n, c.ok)
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
	if r.format != "bv*+ba/b" {
		t.Errorf("format default = %q, want bv*+ba/b", r.format)
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

	if _, err := parseMeta([]byte(`not json`)); err == nil {
		t.Error("expected error on bad json")
	}
}
