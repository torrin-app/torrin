// Package episodes selects files consistently for search, availability and
// playback. Filename ranges cover ordinary releases; catalog story titles
// resolve unambiguous alternate numbering without show-specific arithmetic.
package episodes

import (
	"context"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/video"
	"golang.org/x/sync/singleflight"
)

type Catalog interface {
	Episodes(context.Context, string) ([]cinemeta.Episode, error)
}
type cachedCatalog struct {
	entries []cinemeta.Episode
	expires time.Time
}
type Resolver struct {
	catalog Catalog
	mu      sync.Mutex
	cache   map[string]cachedCatalog
	flight  singleflight.Group
}

func New(c Catalog) *Resolver { return &Resolver{catalog: c, cache: make(map[string]cachedCatalog)} }

func (r *Resolver) catalogEpisodes(ctx context.Context, id string) []cinemeta.Episode {
	if r == nil || r.catalog == nil || id == "" {
		return nil
	}
	id = strings.TrimPrefix(id, "tt")
	r.mu.Lock()
	item, ok := r.cache[id]
	r.mu.Unlock()
	if ok && time.Now().Before(item.expires) {
		return item.entries
	}
	result := r.flight.DoChan(id, func() (any, error) {
		r.mu.Lock()
		recent, exists := r.cache[id]
		r.mu.Unlock()
		if exists && time.Now().Before(recent.expires) {
			return recent.entries, nil
		}
		// A canceled stream-list request must not cancel other waiters.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		entries, err := r.catalog.Episodes(fetchCtx, id)
		ttl := 30 * time.Minute
		if err != nil {
			ttl = 30 * time.Second
		}
		r.mu.Lock()
		if len(r.cache) >= 512 {
			clear(r.cache)
		}
		r.cache[id] = cachedCatalog{entries: entries, expires: time.Now().Add(ttl)}
		r.mu.Unlock()
		return entries, err
	})
	select {
	case <-ctx.Done():
		return nil
	case out := <-result:
		entries, _ := out.Val.([]cinemeta.Episode)
		return entries
	}
}

var sample = regexp.MustCompile(`(?i)(^|[ ._-])sample([ ._-]|$)`)

// Annotate returns copies with stable indexes and optional canonical coverage.
// No DB or schema migration is required for existing libraries.
func (r *Resolver) Annotate(ctx context.Context, imdb string, j *jobs.Job, files []jobs.File) []jobs.File {
	indexed := jobs.FilesForEpisode(j, files, 0, 0)
	catalog := r.catalogEpisodes(ctx, imdb)
	out := make([]jobs.File, 0, len(indexed))
	for _, file := range indexed {
		base := path.Base(strings.ReplaceAll(file.Name, `\`, "/"))
		if !video.IsVideo(base) || sample.MatchString(base) {
			continue
		}
		if len(file.Episodes) == 0 {
			seasons, numbers := jobs.FileEpisodeNumbers(j, file.Name)
			if len(seasons) == 1 {
				file.Episodes = titleMatches(base, seasons[0], catalog)
				if len(file.Episodes) == 0 {
					for _, n := range numbers {
						file.Episodes = append(file.Episodes, jobs.EpisodeRef{Season: seasons[0], Episode: n})
					}
				}
			}
		}
		out = append(out, file)
	}
	return out
}

func (r *Resolver) Select(ctx context.Context, imdb string, j *jobs.Job, files []jobs.File, season, episode int) []jobs.File {
	if episode <= 0 {
		imdb = ""
	} // Movies and generic hash checks require no catalog lookup.
	return jobs.FilesForEpisode(j, r.Annotate(ctx, imdb, j, files), season, episode)
}

// Assess describes only the supplied files, never the full original torrent.
// Unmapped video files keep a negative result unknown rather than proving absence.
func (r *Resolver) Assess(ctx context.Context, imdb string, j *jobs.Job, files []jobs.File, season, episode int) ([]jobs.File, string) {
	if episode <= 0 {
		return r.Select(ctx, imdb, j, files, season, episode), "unknown"
	}
	annotated := r.Annotate(ctx, imdb, j, files)
	selected := jobs.FilesForEpisode(j, annotated, season, episode)
	if len(selected) > 0 {
		return selected, "match"
	}
	if len(annotated) == 0 {
		return selected, "unknown"
	}
	for _, file := range annotated {
		if len(file.Episodes) == 0 {
			return selected, "unknown"
		}
	}
	return selected, "no_match"
}

func words(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }), " ")
}

func titleMatches(name string, season int, catalog []cinemeta.Episode) []jobs.EpisodeRef {
	name = " " + words(name) + " "
	counts := map[string]int{}
	for _, e := range catalog {
		if e.Season == season {
			counts[words(e.Name)]++
		}
	}
	var out []jobs.EpisodeRef
	for _, e := range catalog {
		title := words(e.Name)
		// Short/repeated titles (e.g. "Pilot") cannot override explicit numbering.
		if e.Season != season || counts[title] != 1 || len(strings.Fields(title)) < 3 || !strings.Contains(name, " "+title+" ") {
			continue
		}
		nested := false
		for other := range counts {
			if other != title && strings.Contains(" "+other+" ", " "+title+" ") && strings.Contains(name, " "+other+" ") {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, jobs.EpisodeRef{Season: e.Season, Episode: e.Number})
		}
	}
	return out
}
