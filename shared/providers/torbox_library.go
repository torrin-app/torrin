package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type tbListItem struct {
	ID               int    `json:"id"`
	Hash             string `json:"hash"`
	Name             string `json:"name"`
	Size             int64  `json:"size"`
	DownloadFinished bool   `json:"download_finished"`
	DownloadPresent  bool   `json:"download_present"`
}

func (t *torbox) libraryID(ctx context.Context, hash string) (int, error) {
	want := strings.ToLower(hash)
	for offset := 0; ; offset += 1000 {
		body, err := t.get(ctx, fmt.Sprintf("%s/torrents/mylist?offset=%d&limit=%d", tbBase, offset, 1000))
		if err != nil {
			return 0, err
		}
		var resp struct {
			Success bool         `json:"success"`
			Data    []tbListItem `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return 0, err
		}
		for _, it := range resp.Data {
			if strings.ToLower(it.Hash) == want && it.DownloadFinished && it.DownloadPresent {
				return it.ID, nil
			}
		}
		if len(resp.Data) < 1000 {
			return 0, nil
		}
	}
}

func TBLibrary(ctx context.Context, tbKey string) ([]LibraryItem, error) {
	t := newTorbox(tbKey)
	var out []LibraryItem
	for offset := 0; ; offset += 1000 {
		u := fmt.Sprintf("%s/torrents/mylist?offset=%d&limit=%d", tbBase, offset, 1000)
		body, err := t.get(ctx, u)
		if err != nil {
			return out, err
		}
		var resp struct {
			Success bool         `json:"success"`
			Data    []tbListItem `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return out, err
		}
		for _, it := range resp.Data {
			if it.Hash != "" && it.DownloadFinished && it.DownloadPresent {
				out = append(out, LibraryItem{Hash: strings.ToLower(it.Hash), Filename: it.Name, Size: it.Size})
			}
		}
		if len(resp.Data) < 1000 {
			return out, nil
		}
	}
}
