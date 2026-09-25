package foldername

import (
	"sort"
	"strconv"

	"github.com/subosito/gozaru"
	"github.com/torrin-app/torrin/shared/jobs"
)

func Unique(taken map[string]bool, name, salt string) string {
	name = gozaru.Sanitize(name)
	if !taken[name] {
		taken[name] = true
		return name
	}
	out := name + " [" + salt + "]"
	for i := 2; taken[out]; i++ {
		out = name + " [" + salt + "-" + strconv.Itoa(i) + "]"
	}
	taken[out] = true
	return out
}

func Of(j *jobs.Job) string {
	if j.Name != "" {
		return j.Name
	}
	return j.InfoHash
}

func Short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

func Map(list []*jobs.Job) map[string]string {
	sorted := make([]*jobs.Job, len(list))
	copy(sorted, list)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Name != sorted[j].Name {
			return sorted[i].Name < sorted[j].Name
		}
		return sorted[i].InfoHash < sorted[j].InfoHash
	})
	taken := map[string]bool{}
	out := map[string]string{}
	for _, j := range sorted {
		if j.Status != jobs.StatusComplete || len(j.Files) == 0 {
			continue
		}
		out[j.InfoHash] = Unique(taken, Of(j), Short(j.InfoHash))
	}
	return out
}
