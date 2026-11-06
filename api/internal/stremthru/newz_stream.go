package stremthru

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func (h *Handler) usenetStreamEntry(ctx context.Context, userID, contentHash string) ([]map[string]any, string, int64, bool) {
	data, err := h.Store.GetBytes(ctx, nzb.StorageKey(contentHash))
	if err != nil {
		return nil, "", 0, false
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		return nil, "", 0, false
	}
	kind, direct := download.Classify(parsed)
	if kind == download.KindNone {
		return nil, "", 0, false
	}
	name, size := usenetStreamNameSize(parsed, kind, direct)
	link := h.Store.SignURLNodeUser("", nzb.StorageKey(contentHash), userID, 24*time.Hour) + "&usenet=1"
	entry := map[string]any{"index": 0, "link": link, "name": name, "path": "/" + name, "size": size}
	return []map[string]any{entry}, name, size, true
}

func usenetStreamNameSize(parsed *nzb.NZB, kind download.Kind, direct nzb.File) (string, int64) {
	if kind == download.KindDirect {
		return filepath.Base(download.FileName(direct)), direct.Size()
	}
	name := parsed.Name()
	if name == "" {
		name = "video"
	}
	if !strings.Contains(name, ".") {
		name += ".mkv"
	}
	return name, parsed.TotalSize()
}
