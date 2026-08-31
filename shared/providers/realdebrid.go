package providers

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	rdPollAttempts = 6
	rdPollInterval = 5 * time.Second
)

type realdebrid struct {
	c *rdClient
}

func NewRealDebrid(apiKey string) Provider {
	return &realdebrid{c: newRDClient(apiKey)}
}

func (r *realdebrid) Name() string { return "realdebrid" }

type LibraryItem struct {
	Hash     string
	Filename string
	Size     int64
}

func RDLibrary(ctx context.Context, rdKey string) ([]LibraryItem, error) {
	c := newRDClient(rdKey)
	var out []LibraryItem
	for page := 1; ; page++ {
		ts, err := c.listTorrents(ctx, page, 100)
		if err != nil {
			return out, err
		}
		for _, t := range ts {
			if t.Hash != "" && t.Status == "downloaded" {
				out = append(out, LibraryItem{Hash: strings.ToLower(t.Hash), Filename: t.Filename, Size: t.Bytes})
			}
		}
		if len(ts) < 100 {
			return out, nil
		}
	}
}

func (r *realdebrid) Release(ctx context.Context, handle string) error {
	if handle == "" {
		return nil
	}
	return r.c.deleteTorrent(ctx, handle)
}

func (r *realdebrid) Fetch(ctx context.Context, magnet, infoHash string) (*Result, error) {
	if res, err := r.libraryFetch(ctx, infoHash); err != nil {
		return nil, err
	} else if res != nil {
		return res, nil
	}

	id, err := r.c.addMagnet(ctx, magnet)
	if err != nil {
		return nil, err
	}

	info, err := r.c.torrent(ctx, id)
	if err != nil {
		r.Release(context.Background(), id)
		return nil, err
	}
	if err := r.c.selectFiles(ctx, id, videoSelection(info)); err != nil {
		r.Release(context.Background(), id)
		return nil, err
	}

	if !r.poll(ctx, id) {
		r.Release(context.Background(), id)
		return nil, ctx.Err()
	}

	t, err := r.c.torrent(ctx, id)
	if err != nil {
		r.Release(context.Background(), id)
		return nil, err
	}

	files := r.linksFrom(ctx, t)
	if len(files) == 0 {
		r.Release(context.Background(), id)
		return nil, nil
	}

	name := t.Filename
	if name == "" {
		name = files[0].Name
	}
	return &Result{Name: name, Handle: id, Files: files}, nil
}

func (r *realdebrid) linksFrom(ctx context.Context, t *rdTorrent) []Link {
	var files []Link
	for _, link := range t.Links {
		restricted := link
		u, err := r.c.unrestrict(ctx, restricted)
		if err != nil || !isVideoFile(u.Filename) {
			continue
		}
		files = append(files, Link{
			Name: u.Filename, Size: u.FileSize, URL: u.Download,
			Renew: func(c context.Context) (string, error) {
				nu, err := r.c.unrestrict(c, restricted)
				if err != nil {
					return "", err
				}
				return nu.Download, nil
			},
		})
	}
	return files
}

func (r *realdebrid) poll(ctx context.Context, id string) bool {
	for i := 0; i < rdPollAttempts; i++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(rdPollInterval):
		}
		t, err := r.c.torrent(ctx, id)
		if err != nil {
			return false
		}
		switch {
		case t.Status == "downloaded":
			return true
		case t.Status == "error" || t.Status == "dead" || t.Status == "magnet_error" || t.Status == "virus":
			return false
		case t.Status == "downloading" && t.Progress > 0 && t.Progress < 100:
			return false
		}
	}
	return false
}

func videoSelection(t *rdTorrent) string {
	var ids []string
	for _, f := range t.Files {
		if isVideoFile(f.Path) {
			ids = append(ids, fmt.Sprintf("%d", f.ID))
		}
	}
	if len(ids) == 0 {
		return "all"
	}
	return strings.Join(ids, ",")
}
