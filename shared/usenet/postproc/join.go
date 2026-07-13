package postproc

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func runJoins(dir string) bool {
	if joinSplits(dir) {
		return true
	}
	return joinTS(dir)
}

func joinSplits(dir string) bool {
	joined := false
	for base, parts := range numberedGroups(dir) {
		if fileExists(filepath.Join(dir, base)) || !contiguous(parts) {
			continue
		}
		if concat(dir, base, parts) {
			joined = true
		}
	}
	return joined
}

func joinTS(dir string) bool {
	joined := false
	for base, parts := range tsGroups(dir) {
		out := base + ".ts"
		if fileExists(filepath.Join(dir, out)) || !contiguous(parts) {
			continue
		}
		if concat(dir, out, parts) {
			joined = true
		}
	}
	return joined
}

func numberedGroups(dir string) map[string][]part {
	out := map[string][]part{}
	for _, e := range dirFiles(dir) {
		num := strings.TrimPrefix(filepath.Ext(e), ".")
		if len(num) >= 2 && allDigits(num) {
			base := strings.TrimSuffix(e, filepath.Ext(e))
			n, _ := strconv.Atoi(num)
			out[base] = append(out[base], part{e, n})
		}
	}
	return out
}

func tsGroups(dir string) map[string][]part {
	out := map[string][]part{}
	for _, e := range dirFiles(dir) {
		if base, seq, ok := tsBaseSeq(e); ok {
			out[base] = append(out[base], part{e, seq})
		}
	}
	return out
}

type part struct {
	name string
	seq  int
}

func tsBaseSeq(name string) (string, int, bool) {
	if !strings.HasSuffix(strings.ToLower(name), ".ts") {
		return "", 0, false
	}
	stem := name[:len(name)-3]
	i := len(stem)
	for i > 0 && stem[i-1] >= '0' && stem[i-1] <= '9' {
		i--
	}
	if i == len(stem) {
		return "", 0, false
	}
	n, _ := strconv.Atoi(stem[i:])
	base := strings.TrimRight(stem[:i], "._-")
	if base == "" {
		base = "joined"
	}
	return base, n, true
}

func contiguous(parts []part) bool {
	if len(parts) < 2 {
		return false
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })
	if parts[0].seq > 1 {
		return false
	}
	for i := 1; i < len(parts); i++ {
		if parts[i].seq != parts[i-1].seq+1 {
			return false
		}
	}
	return true
}

func concat(dir, base string, parts []part) bool {
	sort.Slice(parts, func(i, j int) bool { return parts[i].seq < parts[j].seq })
	out, err := os.Create(filepath.Join(dir, base))
	if err != nil {
		return false
	}
	for _, p := range parts {
		in, err := os.Open(filepath.Join(dir, p.name))
		if err != nil {
			out.Close()
			return false
		}
		io.Copy(out, in)
		in.Close()
	}
	out.Close()
	for _, p := range parts {
		os.Remove(filepath.Join(dir, p.name))
	}
	return true
}

func dirFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
