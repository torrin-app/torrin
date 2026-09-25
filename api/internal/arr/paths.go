package arr

import (
	"strings"

	"github.com/subosito/gozaru"
	"github.com/torrin-app/torrin/shared/jobs"
)

func folder(j *jobs.Job) string {
	name := j.Name
	if name == "" {
		name = j.InfoHash
	}
	return gozaru.Sanitize(name)
}

func contentPath(savePath, name string) string {
	return strings.TrimRight(savePath, "/") + "/" + name
}

func folderFor(fm map[string]string, j *jobs.Job) string {
	if n := fm[j.InfoHash]; n != "" {
		return n
	}
	return folder(j)
}

func totalBytes(j *jobs.Job) int64 {
	var t int64
	for _, f := range j.Files {
		t += f.Size
	}
	return t
}

func distinctCategories(labels map[string]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, c := range labels {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

var defaultCategories = []string{"tv", "tv-sonarr", "sonarr", "movies", "radarr", "anime"}

func categoryNames(labels map[string]string) []string {
	seen := map[string]bool{"*": true}
	out := []string{"*"}
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range defaultCategories {
		add(c)
	}
	for _, c := range distinctCategories(labels) {
		add(c)
	}
	return out
}

func catOrDefault(c string) string {
	if c == "" {
		return "*"
	}
	return c
}
