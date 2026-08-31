package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/cinemeta"
	"github.com/torrin-app/torrin/shared/jobs"
)

func startIMDBResolver(ctx context.Context, repo *jobs.Postgres) {
	cm := cinemeta.NewClient()
	tried := map[string]bool{}
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for {
			resolveMissingIMDB(ctx, repo, cm, tried, 150)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

func resolveMissingIMDB(ctx context.Context, repo *jobs.Postgres, cm *cinemeta.Client, tried map[string]bool, batch int) {
	list, err := repo.UntaggedComplete(ctx, batch)
	if err != nil {
		return
	}
	tagged := 0
	for _, j := range list {
		if ctx.Err() != nil {
			return
		}
		title, year := jobs.TitleYear(j.Name)
		if title == "" {
			continue
		}
		series := jobs.IsSeries(j.Name) || j.Season > 0 || j.Episode > 0
		key := title + "|" + strconv.Itoa(year) + "|" + strconv.FormatBool(series)
		if tried[key] {
			continue
		}
		tried[key] = true
		imdb := lookupIMDB(ctx, cm, title, year, series)
		if imdb == "" {
			continue
		}
		if repo.SetIMDB(ctx, j.InfoHash, imdb) == nil {
			tagged++
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
	if tagged > 0 {
		slog.Info("imdb backfill", "tagged", tagged, "scanned", len(list))
	}
}

func lookupIMDB(ctx context.Context, cm *cinemeta.Client, title string, year int, series bool) string {
	ct := "movie"
	if series {
		ct, year = "series", 0
	}
	metas, err := cm.Search(ctx, title, ct)
	if err != nil {
		return ""
	}
	want := jobs.NormTitle(title)
	best := ""
	for _, m := range metas {
		if !strings.HasPrefix(m.ID, "tt") || jobs.NormTitle(m.Name) != want {
			continue
		}
		id := m.ID[2:]
		if best == "" {
			best = id
		}
		if year > 0 && strings.HasPrefix(m.ReleaseInfo, strconv.Itoa(year)) {
			return id
		}
	}
	return best
}
