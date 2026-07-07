package jobs

import (
	"strings"

	"github.com/moistari/rls"
)

var titleStopwords = map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true}

func titleNormFromName(name string) string {
	return NormTitle(rls.ParseString(stripSitePrefix(name)).Title)
}

func stripSitePrefix(name string) string {
	s := strings.TrimSpace(name)
	i := strings.Index(s, " - ")
	if i <= 0 || i > 40 {
		return s
	}
	if prefix := s[:i]; !strings.Contains(prefix, " ") && strings.Contains(prefix, ".") {
		return strings.TrimSpace(s[i+3:])
	}
	return s
}

func NormTitle(s string) string {
	var tokens []string
	var tok strings.Builder
	flush := func() {
		if t := tok.String(); t != "" {
			tokens = append(tokens, t)
		}
		tok.Reset()
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			tok.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()

	var sig strings.Builder
	for _, t := range tokens {
		if titleStopwords[t] || isYearToken(t) {
			continue
		}
		sig.WriteString(t)
	}
	if sig.Len() > 0 {
		return sig.String()
	}
	var raw strings.Builder
	for _, t := range tokens {
		raw.WriteString(t)
	}
	return raw.String()
}

func isYearToken(t string) bool {
	if len(t) != 4 {
		return false
	}
	for _, c := range t {
		if c < '0' || c > '9' {
			return false
		}
	}
	return t >= "1900" && t <= "2099"
}
