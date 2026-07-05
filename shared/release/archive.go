package release

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var Hosts = []string{"rapidgator", "nitroflare", "ddownload", "fikper"}

var sizeUnits = map[string]int64{"KB": 1 << 10, "MB": 1 << 20, "GB": 1 << 30, "TB": 1 << 40}

func ParseSize(s string) int64 {
	f := strings.Fields(s)
	if len(f) != 2 {
		return 0
	}
	n, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0
	}
	return int64(n * float64(sizeUnits[strings.ToUpper(f[1])]))
}

func SplitTitle(text string) (title, size string) {
	if i := strings.LastIndex(text, "–"); i >= 0 {
		return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+len("–"):])
	}
	return text, ""
}

func BestArchive(doc *goquery.Document, want string) [][]string {
	byName := map[string][]string{}
	var order []string
	for _, host := range Hosts {
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			if !strings.Contains(strings.ToLower(href), host) {
				return
			}
			clean := strings.TrimSuffix(href, ".html")
			name := strings.ToLower(path.Base(clean))
			if !isReleaseFile(name) || isSample(name) {
				return
			}
			if _, ok := byName[name]; !ok {
				order = append(order, name)
			}
			byName[name] = appendUnique(byName[name], clean)
		})
	}
	if len(byName) == 0 {
		return nil
	}

	var parts []string
	if hasRARParts(order) {
		for _, n := range order {
			if strings.HasSuffix(n, ".rar") {
				parts = append(parts, n)
			}
		}
		sort.Slice(parts, func(i, j int) bool { return partNum(parts[i]) < partNum(parts[j]) })
	} else if best := pickMatch(order, want); best != "" {
		parts = []string{best}
	}

	out := make([][]string, len(parts))
	for i, n := range parts {
		out[i] = byName[n]
	}
	return out
}

func hasRARParts(names []string) bool {
	for _, n := range names {
		if strings.HasSuffix(n, ".rar") {
			return true
		}
	}
	return false
}

func pickMatch(names []string, want string) string {
	wanted := tokenize(want)
	best, bestScore := "", -1
	for _, n := range names {
		if s := overlap(tokenize(n), wanted); s > bestScore {
			best, bestScore = n, s
		}
	}
	return best
}

func tokenize(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		out[t] = struct{}{}
	}
	return out
}

func overlap(a, b map[string]struct{}) int {
	n := 0
	for t := range a {
		if _, ok := b[t]; ok {
			n++
		}
	}
	return n
}

func appendUnique(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}

func isReleaseFile(name string) bool {
	return strings.HasSuffix(name, ".rar") || strings.HasSuffix(name, ".mkv") || strings.HasSuffix(name, ".mp4")
}

func isSample(name string) bool {
	return strings.Contains(name, "sample") || strings.Contains(name, "proof")
}

func partNum(name string) int {
	i := strings.Index(name, ".part")
	if i < 0 {
		return 0
	}
	digits := ""
	for _, ch := range name[i+5:] {
		if ch < '0' || ch > '9' {
			break
		}
		digits += string(ch)
	}
	n, _ := strconv.Atoi(digits)
	return n
}
