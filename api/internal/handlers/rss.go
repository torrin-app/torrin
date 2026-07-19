package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/release"
	"github.com/torrin-app/torrin/shared/rss"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

func (s *Server) registerRSSRoutes(mux *http.ServeMux, authMW func(http.Handler) http.Handler) {
	mux.Handle("GET /api/rss/feeds", authMW(http.HandlerFunc(s.listFeeds)))
	mux.Handle("GET /api/rss/tags", authMW(http.HandlerFunc(s.rssTags)))
	mux.Handle("POST /api/rss/feeds", authMW(http.HandlerFunc(s.addFeed)))
	mux.Handle("PUT /api/rss/feeds/{id}", authMW(http.HandlerFunc(s.updateFeed)))
	mux.Handle("DELETE /api/rss/feeds/{id}", authMW(http.HandlerFunc(s.deleteFeed)))
	mux.Handle("POST /api/rss/feeds/{id}/toggle", authMW(http.HandlerFunc(s.toggleFeed)))
}

func (s *Server) rssTags(w http.ResponseWriter, r *http.Request) {
	out := map[string][]string{}
	for _, c := range []string{"resolution", "source", "codec", "hdr", "audio", "container"} {
		out[c] = rss.KnownTags(c)
	}
	web.WriteJSON(w, 200, out)
}

func (s *Server) listFeeds(w http.ResponseWriter, r *http.Request) {
	feeds, _ := s.Users.ListRSSFeeds(r.Context(), middleware.GetUser(r).ID)
	if feeds == nil {
		feeds = []*auth.RSSFeed{}
	}
	web.WriteJSON(w, 200, feeds)
}

func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user.PlanID == "free" {
		web.WriteError(w, 403, "rss feeds require a paid plan")
		return
	}
	var req struct {
		URL      string          `json:"url"`
		Name     string          `json:"name"`
		Filter   string          `json:"filter"`
		Criteria json.RawMessage `json:"criteria"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		web.WriteError(w, 400, "url required")
		return
	}
	if _, err := rss.FetchFeed(r.Context(), req.URL); err != nil {
		web.WriteError(w, 400, "invalid feed url")
		return
	}
	if req.Name == "" {
		req.Name = req.URL
	}
	crit, _ := json.Marshal(rss.ParseFilter(req.Criteria))
	feed := &auth.RSSFeed{ID: uuid.NewString(), UserID: user.ID, URL: req.URL, Name: req.Name, Filter: req.Filter, Criteria: crit}
	if err := s.Users.AddRSSFeed(r.Context(), feed); err != nil {
		web.WriteError(w, 500, "failed to save")
		return
	}
	web.WriteJSON(w, 201, feed)
}

func (s *Server) updateFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string          `json:"url"`
		Name     string          `json:"name"`
		Filter   string          `json:"filter"`
		Criteria json.RawMessage `json:"criteria"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		web.WriteError(w, 400, "url required")
		return
	}
	if _, err := rss.FetchFeed(r.Context(), req.URL); err != nil {
		web.WriteError(w, 400, "invalid feed url")
		return
	}
	if req.Name == "" {
		req.Name = req.URL
	}
	crit, _ := json.Marshal(rss.ParseFilter(req.Criteria))
	if err := s.Users.UpdateRSSFeed(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID, req.URL, req.Name, req.Filter, crit); err != nil {
		web.WriteError(w, 500, "failed to save")
		return
	}
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) deleteFeed(w http.ResponseWriter, r *http.Request) {
	s.Users.DeleteRSSFeed(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) toggleFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	s.Users.ToggleRSSFeed(r.Context(), r.PathValue("id"), middleware.GetUser(r).ID, req.Enabled)
	web.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) RunRSSWorker(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.rssSweep(ctx)
		t.Reset(30 * time.Minute)
	}
}

func (s *Server) rssSweep(ctx context.Context) {
	feeds, err := s.Users.GetAllEnabledFeeds(ctx)
	if err != nil {
		slog.Warn("rss: get feeds failed", "err", err)
		return
	}
	slog.Info("rss sweep", "feeds", len(feeds))
	for _, feed := range feeds {
		if ctx.Err() != nil {
			return
		}
		items, err := rss.FetchFeed(ctx, feed.URL)
		if err != nil {
			slog.Warn("rss: fetch failed", "feed", feed.Name, "err", err)
			continue
		}
		user, err := s.Users.GetByID(ctx, feed.UserID)
		if err != nil || user == nil {
			continue
		}
		plan, _ := plans.Get(user.PlanID)
		src := releaseSourceFor(feed.URL)
		submitted := 0
		for _, item := range items {
			if s.Users.IsRSSSeen(ctx, feed.ID, item.GUID) {
				continue
			}
			if !feedMatches(feed, item.Title) {
				s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
				continue
			}
			var added, stop bool
			switch {
			case src != "":
				added, stop = s.rssRelease(ctx, feed, user, plan, item, src)
			case item.NzbURL != "":
				added, stop = s.rssUsenet(ctx, feed, user, plan, item)
			default:
				added, stop = s.rssTorrent(ctx, feed, user, plan, item)
			}
			if stop {
				break
			}
			if added {
				submitted++
			}
		}
		s.Users.MarkFeedChecked(ctx, feed.ID)
		slog.Info("rss feed polled", "feed", feed.Name, "filter", feed.Filter, "items", len(items), "submitted", submitted)
	}
}

func feedMatches(feed *auth.RSSFeed, title string) bool {
	if !rss.ParseFilter(feed.Criteria).Matches(title) {
		return false
	}
	return rss.MatchesFilter(title, feed.Filter)
}

func (s *Server) skipExisting(ctx context.Context, hash string) bool {
	if cached, _ := s.Store.Has(ctx, manifest.Path(hash)); cached {
		return true
	}
	if ex, _ := s.Jobs.GetByInfoHash(ctx, hash); ex != nil && ex.Status != jobs.StatusFailed {
		return true
	}
	return false
}

func releaseSourceFor(feedURL string) jobs.Source {
	u, err := url.Parse(feedURL)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case strings.HasSuffix(host, "hdencode.org"):
		return jobs.SourceHDEncode
	case strings.HasSuffix(host, "scene-rls.net"):
		return jobs.SourceScenerls
	}
	return ""
}

func (s *Server) rssTorrent(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item) (added, stop bool) {
	job := &jobs.Job{
		UserID: user.ID, InfoHash: extractInfoHash(item.Magnet), Magnet: item.Magnet, Name: item.Title,
		Source: jobs.SourceTorrent, Status: jobs.StatusPending,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	return s.rssSubmit(ctx, feed, user, plan, item, job, "torrent")
}

func (s *Server) rssRelease(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item, source jobs.Source) (added, stop bool) {
	if item.Link == "" {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	title, size := release.SplitTitle(item.Title)
	ih := hosterInfoHash(item.Link)
	s.JobsPG.StoreReleaseLink(ctx, ih, item.Link, title, string(source), release.ParseSize(size))
	job := &jobs.Job{
		UserID: user.ID, InfoHash: ih, Magnet: item.Link, Name: title,
		Source: source, Status: jobs.StatusPending,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	return s.rssSubmit(ctx, feed, user, plan, item, job, "release")
}

func (s *Server) rssSubmit(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item, job *jobs.Job, kind string) (added, stop bool) {
	if job.InfoHash == "" || s.skipExisting(ctx, job.InfoHash) {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	if !s.Slots.Acquire(ctx, user.ID, plan) {
		return false, true
	}
	err := s.Jobs.Create(ctx, job)
	s.Slots.Release(user.ID)
	if err != nil {
		return false, false
	}
	s.assign(job)
	s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
	slog.Info("rss: auto-added "+kind, "feed", feed.Name, "title", job.Name, "hash", job.InfoHash)
	return true, false
}

func (s *Server) rssUsenet(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item) (added, stop bool) {
	if _, err := s.Users.GetUsenetCreds(ctx, user.ID); err != nil && !plan.SystemUsenet {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	data, err := fetchNZBData(ctx, item.NzbURL)
	if err != nil {
		return false, false
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil || len(parsed.Files) == 0 {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	hash := nzb.Hash(parsed)
	name := parsed.Name()
	if name == "" {
		name = item.Title
	}
	if name == "" {
		name = "usenet download"
	}
	size := parsed.TotalSize()
	if s.skipExisting(ctx, hash) || (plan.MaxTorrentBytes > 0 && size > plan.MaxTorrentBytes) {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	if !s.Slots.Acquire(ctx, user.ID, plan) {
		return false, true
	}
	if err := s.Store.Put(ctx, nzb.StorageKey(hash), bytes.NewReader(data), "application/x-nzb"); err != nil {
		s.Slots.Release(user.ID)
		return false, false
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: hash, Name: name, Source: jobs.SourceUsenet,
		Status: jobs.StatusPending, FileSize: size,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	for i, f := range parsed.Files {
		var fsize int64
		for _, sg := range f.Segments {
			fsize += sg.Bytes
		}
		job.Files = append(job.Files, jobs.File{Index: i, Name: f.Subject, Size: fsize})
	}
	err = s.Jobs.Create(ctx, job)
	s.Slots.Release(user.ID)
	if err != nil {
		return false, false
	}
	s.assign(job)
	s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
	slog.Info("rss: auto-added usenet", "feed", feed.Name, "title", name, "hash", hash)
	return true, false
}

func fetchNZBData(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("nzb fetch HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
}
