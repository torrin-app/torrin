package indexer

import (
	"strings"

	"github.com/moistari/rls"
	tnp "github.com/torrin-app/torrent-name-parser"
)

func normalizeTitle(s string) string {
	return rls.MustNormalize(rls.ParseString(s).Title)
}

func IMDBEqual(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return strings.TrimPrefix(a, "tt") == strings.TrimPrefix(b, "tt")
}

func TitleMatch(candidate, want string) bool {
	w := normalizeTitle(want)
	return w == "" || normalizeTitle(candidate) == w
}

func Verify(results []Result, imdbID, title string, season, episode int) []Result {
	out := make([]Result, 0, len(results))
	for i := range results {
		if !IMDBEqual(results[i].IMDBID, imdbID) {
			continue
		}
		if !TitleMatch(results[i].Title, title) {
			continue
		}
		if episode > 0 {
			info, _ := tnp.ParseName(strings.ReplaceAll(results[i].Title, ".", " "))
			if info.Season != season || info.Episode != episode {
				continue
			}
		}
		out = append(out, results[i])
	}
	return out
}
