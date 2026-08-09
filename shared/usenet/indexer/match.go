package indexer

import (
	"strconv"
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

func Dedup(results []Result) []Result {
	best := map[string]int{}
	out := make([]Result, 0, len(results))
	for i := range results {
		key := normalizeTitle(results[i].Title) + "|" + strconv.FormatInt(results[i].Size, 10)
		if j, ok := best[key]; ok {
			if betterResult(results[i], out[j]) {
				out[j] = results[i]
			}
			continue
		}
		best[key] = len(out)
		out = append(out, results[i])
	}
	return out
}

func betterResult(a, b Result) bool {
	if a.Grabs != b.Grabs {
		return a.Grabs > b.Grabs
	}
	return a.Size > b.Size
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
