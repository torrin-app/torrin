package server

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
)

const browserStyle = `body{font:15px/1.6 system-ui,-apple-system,sans-serif;background:#0b0a09;color:#ddd;max-width:900px;margin:0 auto;padding:24px}` +
	`a{color:#c8956c;text-decoration:none}a:hover{text-decoration:underline}h1{font-size:18px;font-weight:600}` +
	`ul{padding:0;margin:16px 0}li{list-style:none;padding:5px 0;border-bottom:1px solid #1c1a18;display:flex;justify-content:space-between}` +
	`.s{color:#666;font-size:13px}.d::before{content:"folder "}.f::before{content:"file "}`

func renderHTML(w http.ResponseWriter, urlPath string, n *node) {
	title := "/"
	if n.name != "" {
		title = n.name
	}
	base := strings.TrimRight(urlPath, "/")

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>` + html.EscapeString(title) + ` - Torrin</title><style>` + browserStyle + `</style>`)
	b.WriteString(`<h1>` + html.EscapeString(title) + `</h1><ul>`)
	if n.name != "" {
		b.WriteString(`<li class="d"><a href="` + html.EscapeString(parentURL(base)) + `">..</a><span></span></li>`)
	}
	for _, c := range n.children {
		href := base + "/" + (&url.URL{Path: c.name}).EscapedPath()
		cls, size := "f", `<span class="s">`+humanSize(c.size)+`</span>`
		if c.dir {
			cls, size = "d", "<span></span>"
		}
		b.WriteString(`<li class="` + cls + `"><a href="` + html.EscapeString(href) + `">` + html.EscapeString(c.name) + `</a>` + size + `</li>`)
	}
	b.WriteString(`</ul>`)
	w.Write([]byte(b.String()))
}

func parentURL(p string) string {
	p = strings.TrimRight(p, "/")
	i := strings.LastIndexByte(p, '/')
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func humanSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	f, units := float64(n), "KMGT"
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %cB", f, units[i])
}
