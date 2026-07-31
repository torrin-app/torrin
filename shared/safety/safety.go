package safety

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	ahocorasick "github.com/petar-dambovaliev/aho-corasick"
)

type Verdict struct {
	Blocked bool
	Ban     bool
	Reason  string
}

type termSet struct {
	hard, soft *ahocorasick.AhoCorasick
}

var terms atomic.Pointer[termSet]

func SetTerms(hard, soft []string) {
	terms.Store(&termSet{hard: build(hard), soft: build(soft)})
}

func build(in []string) *ahocorasick.AhoCorasick {
	pats := clean(in)
	if len(pats) == 0 {
		return nil
	}
	b := ahocorasick.NewAhoCorasickBuilder(ahocorasick.Opts{
		AsciiCaseInsensitive: true,
		MatchOnlyWholeWords:  true,
		MatchKind:            ahocorasick.LeftMostLongestMatch,
	})
	ac := b.Build(pats)
	return &ac
}

func clean(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func hit(ac *ahocorasick.AhoCorasick, text string) bool {
	return ac != nil && len(ac.FindAll(text)) > 0
}

func hasOnionAddress(text string) bool {
	t := strings.ToLower(text)
	for {
		i := strings.Index(t, ".onion")
		if i < 0 {
			return false
		}
		n := 0
		for j := i - 1; j >= 0; j-- {
			if c := t[j]; (c >= 'a' && c <= 'z') || (c >= '2' && c <= '7') {
				n++
				continue
			}
			break
		}
		if n == 16 || n == 56 {
			return true
		}
		t = t[i+6:]
	}
}

func Screen(texts ...string) Verdict {
	ts := terms.Load()
	var soft Verdict
	for _, t := range texts {
		if hasOnionAddress(t) {
			return Verdict{Blocked: true, Ban: true, Reason: "blocked:onion"}
		}
		if ts == nil {
			continue
		}
		if hit(ts.hard, t) {
			return Verdict{Blocked: true, Ban: true, Reason: "blocked:term"}
		}
		if !soft.Blocked && hit(ts.soft, t) {
			soft = Verdict{Blocked: true, Ban: false, Reason: "flagged:term"}
		}
	}
	return soft
}

func Refresh(ctx context.Context, load func(context.Context) (hard, soft []string, err error), interval time.Duration) {
	apply := func() {
		if h, s, err := load(ctx); err == nil {
			SetTerms(h, s)
		}
	}
	apply()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				apply()
			}
		}
	}()
}
