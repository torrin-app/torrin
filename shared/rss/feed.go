package rss

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/moistari/rls"
	"github.com/moistari/rls/taginfo"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/useragent"
)

type Item struct {
	GUID       string
	Title      string
	Magnet     string
	TorrentURL string
	NzbURL     string
	Link       string
}

func FetchFeed(ctx context.Context, feedURL string) ([]Item, error) {
	fp := gofeed.NewParser()
	fp.UserAgent = useragent.Default
	fp.Client = safeurl.Client(15 * time.Second)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	feed, err := fp.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}

	var items []Item
	for _, entry := range feed.Items {
		magnet := extractMagnet(entry)
		torrentURL := ""
		nzbURL := ""
		if magnet == "" {
			torrentURL = extractTorrentURL(entry)
		}
		if magnet == "" && torrentURL == "" {
			nzbURL = extractNZB(entry)
		}
		if magnet == "" && torrentURL == "" && nzbURL == "" && entry.Link == "" {
			continue
		}
		guid := entry.GUID
		if guid == "" {
			guid = entry.Link
		}
		if guid == "" {
			switch {
			case magnet != "":
				guid = magnet
			case torrentURL != "":
				guid = torrentURL
			default:
				guid = nzbURL
			}
		}
		items = append(items, Item{GUID: guid, Title: entry.Title, Magnet: magnet, TorrentURL: torrentURL, NzbURL: nzbURL, Link: entry.Link})
	}
	return items, nil
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

func MatchesFilter(title, filter string) bool {
	if filter == "" {
		return true
	}
	lower := strings.ToLower(title)
	for _, kw := range strings.Split(filter, ",") {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

type Filter struct {
	Resolutions []string `json:"resolutions,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	Codecs      []string `json:"codecs,omitempty"`
	HDR         []string `json:"hdr,omitempty"`
	Audio       []string `json:"audio,omitempty"`
	Containers  []string `json:"containers,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	YearMin     int      `json:"year_min,omitempty"`
	YearMax     int      `json:"year_max,omitempty"`
	Match       string   `json:"match,omitempty"`
	Except      string   `json:"except,omitempty"`
}

func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) > 0 {
		json.Unmarshal(raw, &f)
	}
	return f
}

func (f Filter) Empty() bool {
	return len(f.Resolutions) == 0 && len(f.Sources) == 0 && len(f.Codecs) == 0 &&
		len(f.HDR) == 0 && len(f.Audio) == 0 && len(f.Containers) == 0 && len(f.Groups) == 0 &&
		f.YearMin == 0 && f.YearMax == 0 && f.Match == "" && f.Except == ""
}

func (f Filter) Matches(title string) bool {
	r := rls.ParseString(title)
	if len(f.Resolutions) > 0 && !overlaps(f.Resolutions, []string{r.Resolution}) {
		return false
	}
	if len(f.Sources) > 0 && !overlaps(f.Sources, []string{r.Source}) {
		return false
	}
	if len(f.Codecs) > 0 && !overlaps(f.Codecs, r.Codec) {
		return false
	}
	if len(f.HDR) > 0 && !hdrMatches(f.HDR, r.HDR) {
		return false
	}
	if len(f.Audio) > 0 && !overlaps(f.Audio, r.Audio) {
		return false
	}
	if len(f.Containers) > 0 && !overlaps(f.Containers, containersOf(title, r.Container)) {
		return false
	}
	if len(f.Groups) > 0 && !overlaps(f.Groups, []string{r.Group}) {
		return false
	}
	if f.YearMin > 0 && r.Year > 0 && r.Year < f.YearMin {
		return false
	}
	if f.YearMax > 0 && r.Year > 0 && r.Year > f.YearMax {
		return false
	}
	lower := strings.ToLower(title)
	if f.Match != "" && !matchExpr(lower, f.Match) {
		return false
	}
	if f.Except != "" && anyTermPresent(lower, f.Except) {
		return false
	}
	return true
}

func hdrMatches(wanted, have []string) bool {
	if len(have) == 0 {
		for _, w := range wanted {
			if strings.EqualFold(strings.TrimSpace(w), "SDR") {
				return true
			}
		}
	}
	return overlaps(wanted, have)
}

func overlaps(wanted, have []string) bool {
	for _, w := range wanted {
		if strings.TrimSpace(w) == "" {
			continue
		}
		for _, h := range have {
			if strings.EqualFold(strings.TrimSpace(w), h) {
				return true
			}
		}
	}
	return false
}

var tagVocab = taginfo.All()

func KnownTags(category string) []string {
	var out []string
	for _, info := range tagVocab[category] {
		if t := info.Tag(); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func containersOf(title, parsed string) []string {
	out := []string{}
	if parsed != "" {
		out = append(out, parsed)
	}
	lower := strings.ToLower(title)
	for _, e := range []string{"mkv", "mp4", "avi", "ts", "m2ts", "wmv"} {
		if strings.HasSuffix(lower, "."+e) {
			out = append(out, e)
		}
	}
	return out
}

func matchExpr(lower, expr string) bool {
	for _, group := range strings.Split(expr, ",") {
		terms := strings.Fields(strings.ToLower(group))
		if len(terms) == 0 {
			continue
		}
		all := true
		for _, t := range terms {
			if !strings.Contains(lower, t) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func anyTermPresent(lower, expr string) bool {
	for _, t := range strings.FieldsFunc(strings.ToLower(expr), func(r rune) bool { return r == ',' || r == ' ' }) {
		if t != "" && strings.Contains(lower, t) {
			return true
		}
	}
	return false
}
