package jobs

import (
	"strings"

	"github.com/moistari/rls"
)

var titleStopwords = map[string]bool{"the": true, "a": true, "an": true, "of": true, "and": true}

func titleNormFromName(name string) string {
	s := stripSitePrefix(name)
	if before, ok := beforeYearParen(s); ok {
		return NormTitle(stripLeadingTags(before))
	}
	return NormTitle(rls.ParseString(s).Title)
}

func TitleYear(name string) (string, int) {
	s := stripSitePrefix(name)
	p := rls.ParseString(s)
	if before, ok := beforeYearParen(s); ok {
		return stripLeadingTags(before), p.Year
	}
	return strings.TrimSpace(p.Title), p.Year
}

func IsSeries(name string) bool {
	return rls.ParseString(stripSitePrefix(name)).Type.Is(rls.Series, rls.Episode)
}

func beforeYearParen(s string) (string, bool) {
	for i := 0; i+5 < len(s); i++ {
		if s[i] == '(' && isYearToken(s[i+1:i+5]) && s[i+5] == ')' {
			if t := strings.TrimSpace(s[:i]); t != "" {
				return t, true
			}
		}
	}
	return "", false
}

func stripLeadingTags(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "[") {
		j := strings.IndexByte(s, ']')
		if j < 0 {
			break
		}
		s = strings.TrimSpace(s[j+1:])
	}
	return s
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
