package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tnp "github.com/torrin-app/torrent-name-parser"
	"github.com/torrin-app/torrin/shared/jobs"
)

type identity struct {
	imdb    string
	season  int
	episode int
	title   string
}

func parseSeasonEpisode(text string) (int, int) {
	info, err := tnp.ParseName(strings.ReplaceAll(text, ".", " "))
	if err != nil || info.Episode == 0 {
		return 0, 0
	}
	season := 1
	if len(info.Seasons) > 0 {
		season = info.Seasons[0]
	}
	return season, info.Episode
}

func imdbToken(text string) string {
	for _, tok := range strings.Fields(text) {
		if len(tok) > 2 && strings.HasPrefix(tok, "tt") && allDigits(tok[2:]) {
			return tok
		}
	}
	return ""
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

func (b *Bot) resolveIdentity(ctx context.Context, caption, filename string) (identity, bool) {
	caption = strings.TrimSpace(caption)
	if caption == "" || b.cm == nil {
		return identity{}, false
	}
	season, episode := parseSeasonEpisode(caption)
	if season == 0 && episode == 0 {
		season, episode = parseSeasonEpisode(filename)
	}
	kind := "movie"
	if episode > 0 {
		kind = "series"
	}
	if imdb := imdbToken(caption); imdb != "" {
		title, _ := b.cm.Title(ctx, imdb, kind)
		return identity{imdb: imdb, season: season, episode: episode, title: title}, true
	}
	info, _ := tnp.ParseName(strings.ReplaceAll(caption, ".", " "))
	query := info.Title
	if query == "" {
		query = caption
	}
	metas, err := b.cm.Search(ctx, query, kind)
	if err != nil || len(metas) == 0 {
		return identity{}, false
	}
	m := metas[0]
	if info.Year > 0 {
		for _, cand := range metas {
			if strings.HasPrefix(cand.ReleaseInfo, strconv.Itoa(info.Year)) {
				m = cand
				break
			}
		}
	}
	if m.ID == "" {
		return identity{}, false
	}
	return identity{imdb: m.ID, season: season, episode: episode, title: m.Name}, true
}

func (b *Bot) applyIdentity(ctx context.Context, job *jobs.Job, caption, filename string) string {
	id, ok := b.resolveIdentity(ctx, caption, filename)
	if !ok {
		if strings.TrimSpace(caption) != "" {
			return " I couldn't match \"" + strings.TrimSpace(caption) + "\" to a title, so it may not show in search, resend with an imdb id like tt1877830."
		}
		return " Tip: add a caption with the title or imdb id (e.g. \"The Batman 2022\" or tt1877830) so it shows up in Stremio search."
	}
	job.IMDBID = strings.TrimPrefix(id.imdb, "tt")
	job.Season = id.season
	job.Episode = id.episode
	label := id.title
	if label == "" {
		label = "tt" + job.IMDBID
	}
	if id.episode > 0 {
		return fmt.Sprintf(" Tagged as %s S%02dE%02d, searchable in Stremio.", label, id.season, id.episode)
	}
	return " Tagged as " + label + ", searchable in Stremio."
}
