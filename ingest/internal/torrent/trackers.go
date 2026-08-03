package torrent

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/shared/qbit"
)

const trackersURL = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"

// StartTrackerRefresh keeps qBittorrent's auto-add-trackers list in sync with the
// ngosang "best" list (curated, updated daily upstream), qBit appends these to
// every new torrent, improving peer discovery for less popular content. Refreshes
// on startup, then daily.
func StartTrackerRefresh(ctx context.Context, qb *qbit.Client) {
	refresh := func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, trackersURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Warn("tracker refresh: fetch failed", "err", err)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		trackers := strings.TrimSpace(string(body))
		if resp.StatusCode != 200 || trackers == "" {
			slog.Warn("tracker refresh: bad list", "status", resp.StatusCode)
			return
		}
		if qb.Login() != nil {
			return
		}
		if err := qb.SetAutoTrackers(trackers); err != nil {
			slog.Warn("tracker refresh: set failed", "err", err)
			return
		}
		slog.Info("qbit auto-trackers updated", "lines", strings.Count(trackers, "\n")+1)
	}
	refresh()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refresh()
		}
	}
}
