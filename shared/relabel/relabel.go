package relabel

import "strings"

type NamedSize struct {
	Name string
	Size int64
}

type Result struct {
	Mapping   map[string]string
	Ambiguous []string
	Unmatched []string
}

func base(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func Match(real, cached []NamedSize) Result {
	bySize := map[int64][]string{}
	for _, r := range real {
		bySize[r.Size] = append(bySize[r.Size], base(r.Name))
	}
	res := Result{Mapping: make(map[string]string)}
	for _, c := range cached {
		switch cand := bySize[c.Size]; len(cand) {
		case 1:
			res.Mapping[c.Name] = cand[0]
		case 0:
			res.Unmatched = append(res.Unmatched, c.Name)
		default:
			res.Ambiguous = append(res.Ambiguous, c.Name)
		}
	}
	return res
}
