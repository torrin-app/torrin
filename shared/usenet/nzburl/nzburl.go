package nzburl

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"github.com/nlnwa/whatwg-url/url"
)

func Hash(raw string) string {
	if c, ok := canonical(raw); ok {
		return md5hex(c)
	}
	return md5hex(clean(raw))
}

type shape struct {
	pathSuffix string
	keep       []string
	matches    func(*url.SearchParams) bool
}

var shapes = []shape{
	{"/api", []string{"t", "id"}, func(q *url.SearchParams) bool {
		return q.Get("t") == "get" || q.Get("t") == "g"
	}},
	{"/download", []string{"link"}, nil},
	{"/getnzb", []string{"id"}, nil},
}

func canonical(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	q := u.SearchParams()
	pathName := strings.TrimSuffix(u.Pathname(), "/")
	for _, sh := range shapes {
		if !strings.HasSuffix(pathName, sh.pathSuffix) {
			continue
		}
		if !hasAll(q, sh.keep) {
			continue
		}
		if sh.matches != nil && !sh.matches(q) {
			continue
		}
		return u.Protocol() + "//" + u.Host() + u.Pathname() + "?" + keepQuery(q, sh.keep) + u.Hash(), true
	}
	return "", false
}

func hasAll(q *url.SearchParams, keep []string) bool {
	for _, k := range keep {
		if q.Get(k) == "" {
			return false
		}
	}
	return true
}

func keepQuery(q *url.SearchParams, keep []string) string {
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	var b strings.Builder
	q.Iterate(func(p *url.NameValuePair) {
		if !kept[p.Name] {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		formEncode(&b, p.Name)
		b.WriteByte('=')
		formEncode(&b, p.Value)
	})
	return b.String()
}

const formUnreserved = "*-._"

func formEncode(b *strings.Builder, s string) {
	const hexdig = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case isAlnum(c) || strings.IndexByte(formUnreserved, c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexdig[c>>4])
			b.WriteByte(hexdig[c&0x0f])
		}
	}
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func clean(raw string) string {
	c := raw
	if i := strings.IndexByte(c, '?'); i != -1 {
		c = c[:i]
	} else if i := strings.IndexByte(c, '&'); i != -1 {
		c = c[:i]
	}
	if i := strings.IndexByte(c, '#'); i != -1 {
		c = c[:i]
	}
	return c
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
