package providers

import (
	"context"
	"strings"
)

func (a *alldebrid) linksFrom(ctx context.Context, files []adFile) []Link {
	var links []Link
	for _, f := range files {
		if !isVideoFile(f.Name) {
			continue
		}
		u, err := a.unlock(ctx, f.Link)
		if err != nil || u.Link == "" {
			continue
		}
		links = append(links, Link{Name: u.Filename, Size: u.FileSize, URL: u.Link})
	}
	return links
}

func (a *alldebrid) libraryFetch(ctx context.Context, infoHash string) (*Result, error) {
	hash := strings.ToLower(infoHash)
	if hash == "" {
		return nil, nil
	}
	magnets, err := ADListMagnets(ctx, a.key, "ready")
	if err != nil {
		return nil, err
	}
	for _, m := range magnets {
		if strings.ToLower(m.Hash) != hash {
			continue
		}
		files, err := a.files(ctx, m.ID)
		if err != nil {
			return nil, err
		}
		links := a.linksFrom(ctx, files)
		if len(links) == 0 {
			return nil, nil
		}
		name := m.Filename
		if name == "" {
			name = links[0].Name
		}
		return &Result{Name: name, Files: links}, nil
	}
	return nil, nil
}
