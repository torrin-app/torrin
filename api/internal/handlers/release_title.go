package handlers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/release"
)

func releaseTitleFromURL(ctx context.Context, raw string) string {
	if t := fetchReleaseTitle(ctx, raw); t != "" {
		return t
	}
	return titleFromSlug(raw)
}

func fetchReleaseTitle(ctx context.Context, raw string) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	doc, err := release.FetchDoc(ctx, http.DefaultClient, http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	heading := strings.TrimSpace(doc.Find("h1").First().Text())
	if heading == "" {
		heading = strings.TrimSpace(doc.Find("title").First().Text())
	}
	title, _ := release.SplitTitle(heading)
	return strings.TrimSpace(title)
}

func titleFromSlug(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	slug := strings.Trim(u.Path, "/")
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	toks := strings.Split(slug, "-")
	for len(toks) > 1 {
		last := strings.ToLower(toks[len(toks)-1])
		if last == "gb" || last == "mb" {
			toks = toks[:len(toks)-1]
			continue
		}
		if _, err := strconv.Atoi(last); err == nil {
			toks = toks[:len(toks)-1]
			continue
		}
		break
	}
	return strings.Join(toks, ".")
}
