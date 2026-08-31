package urlnorm

import (
	"net/url"
	"path"
	"strings"
)

var tracking = map[string]bool{
	"utm_source": true, "utm_medium": true, "utm_campaign": true,
	"utm_term": true, "utm_content": true, "utm_id": true,
	"gclid": true, "gclsrc": true, "dclid": true, "fbclid": true,
	"msclkid": true, "mc_eid": true, "igshid": true, "ref_src": true,
	"yclid": true, "_ga": true,
}

func Canonical(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	if u.Path != "" {
		if cleaned := path.Clean(u.Path); cleaned != "." {
			u.Path = cleaned
		}
	}
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if tracking[strings.ToLower(k)] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
