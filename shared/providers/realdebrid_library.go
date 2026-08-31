package providers

import (
	"context"
	"strings"
)

func (r *realdebrid) libraryFetch(ctx context.Context, infoHash string) (*Result, error) {
	hash := strings.ToLower(infoHash)
	if hash == "" {
		return nil, nil
	}
	for page := 1; ; page++ {
		ts, err := r.c.listTorrents(ctx, page, 100)
		if err != nil {
			return nil, err
		}
		for i := range ts {
			if strings.ToLower(ts[i].Hash) != hash || ts[i].Status != "downloaded" {
				continue
			}
			t, err := r.c.torrent(ctx, ts[i].ID)
			if err != nil {
				return nil, err
			}
			files := r.linksFrom(ctx, t)
			if len(files) == 0 {
				return nil, nil
			}
			name := t.Filename
			if name == "" {
				name = files[0].Name
			}
			return &Result{Name: name, Files: files}, nil
		}
		if len(ts) < 100 {
			return nil, nil
		}
	}
}
