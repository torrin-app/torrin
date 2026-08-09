package release

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tnp "github.com/torrin-app/torrent-name-parser"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/failure"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	srelease "github.com/torrin-app/torrin/shared/release"
	"github.com/torrin-app/torrin/shared/usenet/indexer"
)

type usenetRunner interface {
	HasProvider(ctx context.Context, userID string) bool
	RunNZB(ctx context.Context, job *jobs.Job, data []byte) error
	AssemblePack(ctx context.Context, job *jobs.Job, parts [][]byte) error
}

type UsenetFallback struct {
	users  *auth.Store
	un     usenetRunner
	meta   *cinemeta.Client
	sysURL string
	sysKey string
}

func NewUsenetFallback(users *auth.Store, un usenetRunner, sysURL, sysKey string) *UsenetFallback {
	return &UsenetFallback{users: users, un: un, meta: cinemeta.NewClient(), sysURL: sysURL, sysKey: sysKey}
}

type relInfo struct {
	title   string
	year    int
	season  int
	episode int
	res     string
}

func parseRel(name string) relInfo {
	info, _ := tnp.ParseName(strings.ReplaceAll(name, ".", " "))
	return relInfo{title: info.Title, year: info.Year, season: info.Season, episode: info.Episode, res: resolutionOf(name)}
}

func (f *UsenetFallback) Try(ctx context.Context, job *jobs.Job) error {
	if !f.un.HasProvider(ctx, job.UserID) {
		return failure.Wrap(failure.UsenetNotSetup, "no usenet provider for user")
	}
	sources := f.sourcesFor(ctx, job.UserID)
	if len(sources) == 0 {
		return failure.Wrap(failure.UsenetNotSetup, "no usenet indexer for user")
	}
	ri := parseRel(job.Name)
	if ri.season > 0 && ri.episode == 0 {
		return f.tryPack(ctx, job, sources, ri)
	}
	return f.trySingle(ctx, job, sources, ri)
}

func (f *UsenetFallback) sourcesFor(ctx context.Context, userID string) []indexer.Source {
	u, err := f.users.GetByID(ctx, userID)
	if err != nil || u == nil {
		return nil
	}
	plan, _ := plans.Get(u.PlanID)
	return f.users.IndexerSources(ctx, userID, plan, f.sysURL, f.sysKey)
}

func clientFor(sources []indexer.Source, id string) *indexer.Client {
	if c := indexer.Find(sources, id); c != nil {
		return c
	}
	if len(sources) > 0 {
		return sources[0].Client
	}
	return nil
}

func (f *UsenetFallback) trySingle(ctx context.Context, job *jobs.Job, sources []indexer.Source, ri relInfo) error {
	var results []indexer.Result
	if job.IMDBID != "" {
		results = indexer.FanOut(ctx, sources, 10*time.Second, func(c *indexer.Client) ([]indexer.Result, error) {
			if ri.season > 0 && ri.episode > 0 {
				return c.SearchTV(job.IMDBID, ri.season, ri.episode, 0, 0)
			}
			return c.SearchMovie(job.IMDBID, 0, 0)
		})
	}
	best := bestMatch(results, job.Name, job.FileSize, job.IMDBID)
	if best == nil {
		q := indexer.FanOut(ctx, sources, 10*time.Second, func(c *indexer.Client) ([]indexer.Result, error) {
			return c.SearchQuery(buildQuery(ri), "", 0, 0)
		})
		best = bestMatch(q, job.Name, job.FileSize, job.IMDBID)
	}
	if best == nil {
		return failure.Wrap(failure.NotOnUsenet, "no matching nzb on indexer")
	}
	client := clientFor(sources, best.Source)
	if client == nil {
		return failure.Wrap(failure.NotOnUsenet, "no matching nzb on indexer")
	}
	data, err := client.DownloadNZB(best)
	if err != nil {
		return fmt.Errorf("download nzb: %w", err)
	}
	slog.Info("usenet fallback: single nzb", "job", job.ID, "nzb", best.Title)
	return f.un.RunNZB(ctx, job, data)
}

func (f *UsenetFallback) tryPack(ctx context.Context, job *jobs.Job, sources []indexer.Source, ri relInfo) error {
	query := fmt.Sprintf("%s S%02d", ri.title, ri.season)
	if ri.res != "" {
		query += " " + ri.res
	}
	results := indexer.FanOut(ctx, sources, 10*time.Second, func(c *indexer.Client) ([]indexer.Result, error) {
		return c.SearchQuery(query, "5000,5040,5045", 0, 0)
	})

	byEp := map[int]*indexer.Result{}
	for i := range results {
		if !indexer.IMDBEqual(results[i].IMDBID, job.IMDBID) || !indexer.TitleMatch(results[i].Title, job.Name) {
			continue
		}
		ei := parseRel(results[i].Title)
		if ei.season != ri.season || ei.episode == 0 {
			continue
		}
		if ri.res != "" && ei.res != ri.res {
			continue
		}
		if cur, ok := byEp[ei.episode]; !ok || betterEp(&results[i], cur) {
			byEp[ei.episode] = &results[i]
		}
	}
	if len(byEp) == 0 {
		return failure.Wrap(failure.NotOnUsenet, "no matching episodes on indexer")
	}

	want, err := f.seasonLength(ctx, job.IMDBID, ri.season, byEp)
	if err != nil {
		return err
	}
	parts := make([][]byte, 0, want)
	for e := 1; e <= want; e++ {
		client := clientFor(sources, byEp[e].Source)
		if client == nil {
			return failure.Wrap(failure.NotOnUsenet, "indexer unavailable for episode %d", e)
		}
		data, err := client.DownloadNZB(byEp[e])
		if err != nil {
			return fmt.Errorf("download nzb %q: %w", byEp[e].Title, err)
		}
		parts = append(parts, data)
	}
	slog.Info("usenet fallback: season pack", "job", job.ID, "episodes", len(parts), "res", ri.res)
	return f.un.AssemblePack(ctx, job, parts)
}

func (f *UsenetFallback) seasonLength(ctx context.Context, imdbID string, season int, byEp map[int]*indexer.Result) (int, error) {
	if imdbID != "" {
		if n, err := f.meta.SeasonEpisodes(ctx, imdbID, season); err == nil && n > 0 {
			for e := 1; e <= n; e++ {
				if byEp[e] == nil {
					return 0, failure.Wrap(failure.SeasonPartial, "season incomplete: episode %d/%d missing", e, n)
				}
			}
			return n, nil
		}
	}
	max := 0
	for e := range byEp {
		if e > max {
			max = e
		}
	}
	if max == 0 || byEp[1] == nil || len(byEp) != max {
		return 0, failure.Wrap(failure.SeasonPartial, "season gaps: found %d up to %d", len(byEp), max)
	}
	slog.Warn("usenet fallback: cinemeta unavailable, using gapless heuristic", "season_len", max)
	return max, nil
}

func buildQuery(ri relInfo) string {
	q := ri.title
	switch {
	case ri.season > 0 && ri.episode > 0:
		q += fmt.Sprintf(" S%02dE%02d", ri.season, ri.episode)
	case ri.year > 0:
		q += fmt.Sprintf(" %d", ri.year)
	}
	if ri.res != "" {
		q += " " + ri.res
	}
	return strings.TrimSpace(q)
}

func resolutionOf(name string) string {
	l := strings.ToLower(name)
	for _, r := range []string{"2160p", "1080p", "720p", "480p"} {
		if strings.Contains(l, r) {
			return r
		}
	}
	return ""
}

func betterEp(a, b *indexer.Result) bool {
	if a.Grabs != b.Grabs {
		return a.Grabs > b.Grabs
	}
	return a.Size > b.Size
}

func bestMatch(results []indexer.Result, title string, size int64, jobIMDB string) *indexer.Result {
	want := srelease.Tokenize(title)
	if len(want) == 0 {
		return nil
	}
	var best *indexer.Result
	bestScore := -1.0
	for i := range results {
		if !indexer.IMDBEqual(results[i].IMDBID, jobIMDB) || !indexer.TitleMatch(results[i].Title, title) {
			continue
		}
		frac := float64(srelease.Overlap(srelease.Tokenize(results[i].Title), want)) / float64(len(want))
		if frac < 0.5 {
			continue
		}
		score := frac
		if size > 0 && results[i].Size > 0 {
			if ratio := float64(results[i].Size) / float64(size); ratio >= 0.5 && ratio <= 2.0 {
				score += 0.5
			}
		}
		if score > bestScore {
			bestScore, best = score, &results[i]
		}
	}
	return best
}
