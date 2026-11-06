package rss

import (
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/release"
)

func extractSize(entry *gofeed.Item) int64 {
	if s := sizeFromExtensions(entry); s > 0 {
		return s
	}
	for _, enc := range entry.Enclosures {
		if n, err := strconv.ParseInt(strings.TrimSpace(enc.Length), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	_, size := release.SplitTitle(entry.Title)
	return release.ParseSize(size)
}

func sizeFromExtensions(entry *gofeed.Item) int64 {
	for _, ns := range entry.Extensions {
		for name, exts := range ns {
			for _, e := range exts {
				switch {
				case strings.EqualFold(name, "size") || strings.EqualFold(name, "contentLength"):
					if n, err := strconv.ParseInt(strings.TrimSpace(e.Value), 10, 64); err == nil && n > 0 {
						return n
					}
				case strings.EqualFold(name, "attr") && strings.EqualFold(e.Attrs["name"], "size"):
					if n, err := strconv.ParseInt(strings.TrimSpace(e.Attrs["value"]), 10, 64); err == nil && n > 0 {
						return n
					}
				}
			}
		}
	}
	return 0
}

func extractEnclosure(entry *gofeed.Item, typeNeedle, urlNeedle string) string {
	for _, enc := range entry.Enclosures {
		if strings.Contains(strings.ToLower(enc.Type), typeNeedle) ||
			strings.Contains(strings.ToLower(enc.URL), urlNeedle) {
			return enc.URL
		}
	}
	if strings.Contains(strings.ToLower(entry.Link), urlNeedle) {
		return entry.Link
	}
	return ""
}

func extractTorrentURL(entry *gofeed.Item) string {
	return extractEnclosure(entry, "bittorrent", ".torrent")
}

func extractNZB(entry *gofeed.Item) string {
	return extractEnclosure(entry, "nzb", ".nzb")
}

func extractMagnet(entry *gofeed.Item) string {
	for _, enc := range entry.Enclosures {
		if strings.HasPrefix(enc.URL, "magnet:") {
			return enc.URL
		}
	}
	if strings.HasPrefix(entry.Link, "magnet:") {
		return entry.Link
	}
	for _, text := range []string{entry.Description, entry.Content} {
		if m := magnet.Find(text); m != "" {
			return m
		}
	}
	if ih := infoHashFromExtensions(entry); ih != "" {
		return magnet.Build(ih, entry.Title)
	}
	return ""
}

func infoHashFromExtensions(entry *gofeed.Item) string {
	for _, ns := range entry.Extensions {
		for name, exts := range ns {
			for _, e := range exts {
				switch {
				case strings.EqualFold(name, "infohash"):
					if h := magnet.Hash(e.Value); h != "" {
						return h
					}
				case strings.EqualFold(name, "attr") && strings.EqualFold(e.Attrs["name"], "infohash"):
					if h := magnet.Hash(e.Attrs["value"]); h != "" {
						return h
					}
				}
			}
		}
	}
	return ""
}
