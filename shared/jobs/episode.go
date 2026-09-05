package jobs

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tnp "github.com/torrin-app/torrent-name-parser"
)

// MatchesEpisodeFile applies job metadata when it is available, but prefers
// explicit episode information in the filename. That keeps season packs
// seekable while preventing one episode request from returning every file in
// the pack. single reports whether the job holds a single video file; the job
// metadata fallback is only trustworthy then, since a pack shares one
// Season/Episode across every file.
func MatchesEpisodeFile(j *Job, fileName string, season, episode int, single bool) bool {
	if season < 0 || episode <= 0 {
		return true
	}

	seasons, episodes := FileEpisodeNumbers(j, fileName)
	if len(episodes) > 0 {
		if !slices.Contains(episodes, episode) {
			return false
		}
		if len(seasons) == 0 {
			if j != nil && (j.Season > 0 || j.Episode > 0) {
				return j.Season == season
			}
			return season == 1
		}
		return len(seasons) == 1 && seasons[0] == season
	}

	return single && j != nil && j.Season == season && j.Episode == episode
}

var bareEpisode = regexp.MustCompile(`(?i)^e(?:p(?:isode)?)?[ ._-]*([0-9]{1,3})(?:[ ._-]*-?[ ._-]*e?([0-9]{1,3}))?(?:[ ._-]|$)`)

// Prefer the basename's season to a multi-season parent directory. If the
// basename only names an episode, use the nearest unambiguous season folder.
func FileEpisodeNumbers(j *Job, name string) ([]int, []int) {
	name = strings.ReplaceAll(name, `\`, "/")
	info, err := tnp.ParseName(strings.ReplaceAll(path.Base(name), ".", " "))
	if err != nil {
		return nil, nil
	}
	if len(info.Episodes) == 0 {
		if m := bareEpisode.FindStringSubmatch(path.Base(name)); m != nil {
			first, _ := strconv.Atoi(m[1])
			last := first
			if m[2] != "" {
				last, _ = strconv.Atoi(m[2])
			}
			if last >= first && last-first <= 100 {
				for n := first; n <= last; n++ {
					info.Episodes = append(info.Episodes, n)
				}
			}
		}
	}
	seasons := info.Seasons
	for dir := path.Dir(name); len(seasons) == 0 && dir != "." && dir != "/"; dir = path.Dir(dir) {
		if parent, err := tnp.ParseName(strings.ReplaceAll(path.Base(dir), ".", " ")); err == nil && len(parent.Seasons) == 1 {
			seasons = parent.Seasons
		}
	}
	if len(seasons) == 0 && j != nil {
		if pack, err := tnp.ParseName(strings.ReplaceAll(j.Name, ".", " ")); err == nil && len(pack.Seasons) == 1 {
			seasons = pack.Seasons
		}
	}
	return seasons, info.Episodes
}

// FilesForEpisode returns the matching files with stable original indexes.
// When a row never persisted File.Index (every index is zero) the slice
// position is used as the storage-key index; when any index is set they are
// trusted as-is so a legitimate index 0 is not overwritten.
func FilesForEpisode(j *Job, files []File, season, episode int) []File {
	indexed := false
	for _, f := range files {
		if f.Index > 0 {
			indexed = true
			break
		}
	}
	single := len(files) == 1
	out := make([]File, 0, len(files))
	for position, file := range files {
		if !indexed {
			file.Index = position
		}
		matches := MatchesEpisodeFile(j, file.Name, season, episode, single)
		if season >= 0 && episode > 0 && len(file.Episodes) > 0 {
			matches = slices.Contains(file.Episodes, EpisodeRef{Season: season, Episode: episode})
		}
		if matches {
			out = append(out, file)
		}
	}
	return out
}
