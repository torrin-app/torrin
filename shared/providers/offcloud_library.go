package providers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/torrin-app/torrin/shared/magnet"
)

func (o *offcloud) libraryFetch(ctx context.Context, infoHash string) (*Result, error) {
	hash := strings.ToLower(infoHash)
	if hash == "" {
		return nil, nil
	}
	body, err := o.get(ctx, "/api/cloud/history")
	if err != nil {
		return nil, err
	}
	var items []struct {
		RequestID    string `json:"requestId"`
		FileName     string `json:"fileName"`
		Status       string `json:"status"`
		OriginalLink string `json:"originalLink"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	for _, it := range items {
		if it.Status != "downloaded" || it.RequestID == "" {
			continue
		}
		h := strings.ToLower(strings.TrimSuffix(filepath.Base(it.OriginalLink), ".torrent"))
		if !magnet.Valid(h) || h != hash {
			continue
		}
		files, err := o.explore(ctx, it.RequestID, it.FileName)
		if err != nil || len(files) == 0 {
			return nil, err
		}
		name := it.FileName
		if name == "" {
			name = files[0].Name
		}
		return &Result{Name: name, Files: files}, nil
	}
	return nil, nil
}
