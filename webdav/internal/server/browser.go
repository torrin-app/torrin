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
	`ul{padding:0;margin:12px 0}li{list-style:none;padding:6px 0;border-bottom:1px solid #1c1a18;display:flex;justify-content:space-between;align-items:center;gap:10px}` +
	`.s{color:#666;font-size:13px}.d>.nm>a::before{content:"folder "}.f>.nm>a::before{content:"file "}` +
	`.nm{display:flex;align-items:center;gap:8px;min-width:0}.nm>a{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.off{opacity:.5}` +
	`button{background:transparent;border:1px solid #3a352f;color:#c8956c;border-radius:5px;padding:3px 9px;font-size:12px;cursor:pointer}button:hover{border-color:#c8956c}` +
	`.icon{padding:2px 6px;line-height:1}input.rn{background:#161412;border:1px solid #2a2724;color:#ddd;border-radius:5px;padding:3px 7px;font-size:12px;width:180px}` +
	`.bar{display:flex;gap:8px;margin:8px 0 4px}.ed{display:none;gap:4px;align-items:center}li.ing .ed{display:inline-flex}li.ing .pen{display:none}` +
	`input.ck,input.uck{display:none;margin:0}body.sel .nm input.ck{display:inline-block}body.usel .nm input.uck{display:inline-block}#hidesel,#unhidesel{display:none}body.sel #hidesel{display:inline-block}body.usel #unhidesel{display:inline-block}` +
	`details.hid{margin-top:18px}summary{color:#888;cursor:pointer;font-size:13px}`

const browserJS = `function ov(h,i,a,x){return fetch(location.pathname,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({info_hash:h,file_index:i,alias:a,excluded:x})})}` +
	`function li(e){return e.closest('li')}` +
	`function rl(r){if(!r||r.ok){location.reload()}}` +
	`document.querySelectorAll('button.pen').forEach(function(el){el.addEventListener('click',function(){li(el).classList.add('ing');var f=li(el).querySelector('input.rn');f.focus();f.select()})});` +
	`document.querySelectorAll('button.ok').forEach(function(el){el.addEventListener('click',function(){var l=li(el);ov(l.dataset.h,+l.dataset.i,l.querySelector('input.rn').value.trim(),l.dataset.x==='1').then(rl)})});` +
	`document.querySelectorAll('button.rs').forEach(function(el){el.addEventListener('click',function(){var l=li(el);ov(l.dataset.h,+l.dataset.i,'',l.dataset.x==='1').then(rl)})});` +
	`document.querySelectorAll('button.cx').forEach(function(el){el.addEventListener('click',function(){li(el).classList.remove('ing')})});` +
	`document.querySelectorAll('input.rn').forEach(function(el){el.addEventListener('keydown',function(e){if(e.key==='Enter'){li(el).querySelector('button.ok').click()}if(e.key==='Escape'){li(el).classList.remove('ing')}})});` +
	`var sb=document.getElementById('selbtn');if(sb){sb.addEventListener('click',function(){var on=document.body.classList.toggle('sel');sb.textContent=on?'Cancel':'Select to hide'})}` +
	`var ub=document.getElementById('uselbtn');if(ub){ub.addEventListener('click',function(){var on=document.body.classList.toggle('usel');ub.textContent=on?'Cancel':'Select to unhide'})}` +
	`function bulk(sel,x){var ck=[].slice.call(document.querySelectorAll(sel));if(!ck.length){return}Promise.all(ck.map(function(c){var l=li(c);return ov(l.dataset.h,+l.dataset.i,l.dataset.a,x)})).then(function(){location.reload()})}` +
	`var hs=document.getElementById('hidesel');if(hs){hs.addEventListener('click',function(){bulk('#items input.ck:checked',true)})}` +
	`var us=document.getElementById('unhidesel');if(us){us.addEventListener('click',function(){bulk('#hitems input.uck:checked',false)})}`

func renderHTML(w http.ResponseWriter, urlPath string, n *node) {
	title := "/"
	if n.name != "" {
		title = n.name
	}
	base := strings.TrimRight(urlPath, "/")

	var visible, hidden []*node
	for _, c := range n.children {
		if c.hidden {
			hidden = append(hidden, c)
		} else {
			visible = append(visible, c)
		}
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<title>` + html.EscapeString(title) + ` - Torrin</title><style>` + browserStyle + `</style>`)
	b.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
	b.WriteString(`<p class="s">Rename or hide files here , changes only affect your WebDAV view and stick even if a file is re-cached later.</p>`)
	b.WriteString(`<div class="bar"><button id="selbtn">Select to hide</button><button id="hidesel">Hide selected</button></div>`)
	b.WriteString(`<ul id="items">`)
	if n.name != "" {
		b.WriteString(`<li class="d"><span class="nm"><a href="` + html.EscapeString(parentURL(base)) + `">..</a></span><span></span></li>`)
	}
	for _, c := range visible {
		writeRow(&b, base, c)
	}
	b.WriteString(`</ul>`)

	if len(hidden) > 0 {
		b.WriteString(fmt.Sprintf(`<details class="hid" open><summary>Hidden (%d)</summary><div class="bar"><button id="uselbtn">Select to unhide</button><button id="unhidesel">Unhide selected</button></div><ul id="hitems">`, len(hidden)))
		for _, c := range hidden {
			writeHiddenRow(&b, base, c)
		}
		b.WriteString(`</ul></details>`)
	}

	b.WriteString(`<script>` + browserJS + `</script>`)
	w.Write([]byte(b.String()))
}

func rowData(c *node) string {
	x := "0"
	if c.hidden {
		x = "1"
	}
	return fmt.Sprintf(`data-h="%s" data-i="%d" data-a="%s" data-x="%s"`,
		html.EscapeString(c.hash), c.idx, html.EscapeString(c.alias), x)
}

func writeRow(b *strings.Builder, base string, c *node) {
	href := base + "/" + (&url.URL{Path: c.name}).EscapedPath()
	row := "f"
	if c.dir {
		row = "d"
	}
	b.WriteString(`<li class="` + row + `" ` + rowData(c) + `><span class="nm">`)
	b.WriteString(`<input type="checkbox" class="ck">`)
	b.WriteString(`<a href="` + html.EscapeString(href) + `">` + html.EscapeString(c.name) + `</a>`)
	if !c.dir {
		b.WriteString(`<button class="pen icon" title="Rename">&#9998;</button>`)
		b.WriteString(`<span class="ed"><input class="rn" placeholder="` + html.EscapeString(c.orig) + `" value="` + html.EscapeString(c.alias) + `">`)
		b.WriteString(`<button class="ok icon" title="Save">&#10003;</button>`)
		b.WriteString(`<button class="rs icon" title="Reset to original">&#8635;</button>`)
		b.WriteString(`<button class="cx icon" title="Cancel">&#10005;</button></span>`)
	}
	b.WriteString(`</span>`)
	if c.dir {
		b.WriteString(`<span></span>`)
	} else {
		b.WriteString(`<span class="s">` + humanSize(c.size) + `</span>`)
	}
	b.WriteString(`</li>`)
}

func writeHiddenRow(b *strings.Builder, base string, c *node) {
	href := base + "/" + (&url.URL{Path: c.name}).EscapedPath()
	row := "f off"
	if c.dir {
		row = "d off"
	}
	b.WriteString(`<li class="` + row + `" ` + rowData(c) + `><span class="nm">`)
	b.WriteString(`<input type="checkbox" class="uck">`)
	b.WriteString(`<a href="` + html.EscapeString(href) + `">` + html.EscapeString(c.name) + `</a></span>`)
	if c.dir {
		b.WriteString(`<span></span>`)
	} else {
		b.WriteString(`<span class="s">` + humanSize(c.size) + `</span>`)
	}
	b.WriteString(`</li>`)
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
	f, units := float64(n)/1024, "KMGT"
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %cB", f, units[i])
}
