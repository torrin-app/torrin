package bot

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/torrin-app/torrin/shared/sources"
	"github.com/torrin-app/torrin/shared/storage"
)

func TestParseSeasonEpisode(t *testing.T) {
	cases := []struct {
		in      string
		season  int
		episode int
	}{
		{"The Wire S01E03", 1, 3},
		{"Severance.S02E05.1080p", 2, 5},
		{"The Batman 2022", 0, 0},
		{"just a movie name", 0, 0},
	}
	for _, c := range cases {
		s, e := parseSeasonEpisode(c.in)
		if s != c.season || e != c.episode {
			t.Errorf("parseSeasonEpisode(%q) = s%d e%d, want s%d e%d", c.in, s, e, c.season, c.episode)
		}
	}
}

func TestImdbToken(t *testing.T) {
	if got := imdbToken("some caption tt1877830 extra"); got != "tt1877830" {
		t.Errorf("imdbToken = %q, want tt1877830", got)
	}
	if got := imdbToken("The Batman 2022"); got != "" {
		t.Errorf("imdbToken should be empty, got %q", got)
	}
	if got := imdbToken("ttabc notanid"); got != "" {
		t.Errorf("ttabc is not a valid imdb token, got %q", got)
	}
}

func TestAllDigits(t *testing.T) {
	if !allDigits("1877830") {
		t.Error("digits should pass")
	}
	if allDigits("") || allDigits("12a3") {
		t.Error("empty or non-digit must fail")
	}
}

func TestDedupe(t *testing.T) {
	d := newDedupe(time.Minute)
	if !d.firstSight(1, 100) {
		t.Fatal("first sight should be true")
	}
	if d.firstSight(1, 100) {
		t.Error("same user+msg should be a duplicate")
	}
	if !d.firstSight(1, 101) {
		t.Error("different msg id should be a first sight")
	}
	if !d.firstSight(2, 100) {
		t.Error("different user should be a first sight")
	}
}

func TestDedupeExpiry(t *testing.T) {
	d := newDedupe(10 * time.Millisecond)
	d.firstSight(1, 100)
	time.Sleep(20 * time.Millisecond)
	if !d.firstSight(1, 100) {
		t.Error("should be a first sight again after ttl")
	}
}

func TestDocCacheKey(t *testing.T) {
	// Deterministic 40-hex (sha1) per Telegram doc id — the dedup key.
	a := docCacheKey(12345)
	if len(a) != 40 {
		t.Fatalf("want 40-hex, got %d chars", len(a))
	}
	if docCacheKey(12345) != a {
		t.Error("not deterministic")
	}
	if docCacheKey(54321) == a {
		t.Error("different ids collided")
	}
}

type fakeStore struct{ id string }

func (fakeStore) Has(context.Context, string) (bool, error) { return false, nil }
func (fakeStore) Head(context.Context, string) (*storage.Object, error) {
	return nil, fmt.Errorf("not found")
}
func (fakeStore) StreamUpload(context.Context, string, io.Reader, string) error { return nil }
func (fakeStore) PutSized(context.Context, string, io.Reader, int64, string) error {
	return nil
}
func (fakeStore) Put(context.Context, string, io.Reader, string) error { return nil }

func TestPickStore(t *testing.T) {
	home := fakeStore{"box1"}
	box2 := fakeStore{"box2"}
	overflow := map[string]sources.Store{"box2": box2}

	// target == home node -> primary store, home node
	if s, n := pickStore("", "", home, overflow); s != sources.Store(home) || n != "" {
		t.Fatalf("home node should use primary, got %v %q", s, n)
	}
	// target is an overflow node with a store -> that store, that node
	if s, n := pickStore("box2", "", home, overflow); s != sources.Store(box2) || n != "box2" {
		t.Fatalf("box2 should route to box2 store, got %v %q", s, n)
	}
	// target node with no store configured -> fall back to primary/home
	if s, n := pickStore("box3", "", home, overflow); s != sources.Store(home) || n != "" {
		t.Fatalf("unknown node must fall back to primary, got %v %q", s, n)
	}
}

func TestProgressWriter(t *testing.T) {
	var lastCur, lastTot int64
	calls := 0
	pw := &progressWriter{w: io.Discard, total: 100, report: func(cur, tot int64) {
		lastCur, lastTot = cur, tot
		calls++
	}}
	n, err := pw.Write(make([]byte, 40))
	if err != nil || n != 40 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	pw.Write(make([]byte, 60))
	if lastCur != 100 || lastTot != 100 {
		t.Fatalf("cumulative bytes wrong: cur=%d tot=%d", lastCur, lastTot)
	}
	if calls != 2 {
		t.Fatalf("expected a report per write, got %d", calls)
	}
}
