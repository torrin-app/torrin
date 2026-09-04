package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/release"
	"github.com/torrin-app/torrin/shared/rss"
	"github.com/torrin-app/torrin/shared/safeurl"
	"github.com/torrin-app/torrin/shared/torrentfile"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

const rssMaxAttempts = 4

func rssExhausted(attempts int) bool { return attempts >= rssMaxAttempts }

func (s *Server) rssParseFailed(ctx context.Context, feedID, guid string) {
	if rssExhausted(s.Users.RSSAttempt(ctx, feedID, guid)) {
		s.Users.MarkRSSSeen(ctx, feedID, guid)
	}
}

func (s *Server) rssTorrent(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item) (added, stop bool) {
	job := &jobs.Job{
		UserID: user.ID, InfoHash: extractInfoHash(item.Magnet), Magnet: item.Magnet, Name: item.Title,
		Source: jobs.SourceTorrent, Status: jobs.StatusPending,
		MaxBytes: plan.MaxTorrentBytes, Priority: plan.Priority,
	}
	return s.rssSubmit(ctx, feed, user, plan, item, job, "torrent")
}

func (s *Server) rssTorrentFile(ctx context.Context, feed *auth.RSSFeed, user *auth.User, plan plans.Plan, item rss.Item) (added, stop bool) {
	data, err := fetchTorrentData(ctx, item.TorrentURL)
	if err != nil {
		return false, false
	}
	meta, err := torrentfile.Parse(data)
	if err != nil {
		s.rssParseFailed(ctx, feed.ID, item.GUID)
		return false, false
	}
	if !meta.Private || !s.seedingAllowed(user) {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	if code, _ := s.seedGate(ctx, user, plan, meta); code != 0 {
		if code != 429 {
			s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		}
		return false, code == 429
	}
	if s.skipExisting(ctx, meta.InfoHash) {
		s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
		return false, false
	}
	job, err := s.createSeedJob(ctx, user, plan, meta, data)
	if err != nil {
		return false, false
	}
	s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
	slog.Info("rss: auto-seeded private torrent", "feed", feed.Name, "title", job.Name, "hash", job.InfoHash)
	return true, false
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
	created, err := jobs.CreateAccountOnce(ctx, s.Jobs, job)
	s.Slots.Release(user.ID)
	if err != nil {
		return false, false
	}
	s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
	if !created {
		return false, false
	}
	s.assign(job)
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
	if err != nil {
		s.rssParseFailed(ctx, feed.ID, item.GUID)
		return false, false
	}
	if len(parsed.Files) == 0 {
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
	s.Users.SetJobNZB(ctx, hash, data)
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
	created, err := jobs.CreateAccountOnce(ctx, s.Jobs, job)
	s.Slots.Release(user.ID)
	if err != nil {
		return false, false
	}
	s.Users.MarkRSSSeen(ctx, feed.ID, item.GUID)
	if !created {
		return false, false
	}
	s.assign(job)
	slog.Info("rss: auto-added usenet", "feed", feed.Name, "title", name, "hash", hash)
	return true, false
}

func fetchNZBData(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := safeurl.Client(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("nzb fetch HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
}

func fetchTorrentData(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := safeurl.Client(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("torrent fetch HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes))
}
