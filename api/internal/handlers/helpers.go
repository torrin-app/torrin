package handlers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/middleware"
	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/cairn"
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/safety"
	"github.com/torrin-app/torrin/shared/urlnorm"
)

func (s *Server) blockedBySafety(w http.ResponseWriter, r *http.Request, userID string, texts ...string) bool {
	v := safety.Screen(texts...)
	if !v.Blocked {
		return false
	}
	if v.Ban {
		s.Users.BanUser(r.Context(), userID, v.Reason)
		s.Users.AuditLog(r.Context(), userID, "banned", v.Reason, clientIP(r))
	}
	web.WriteError(w, http.StatusForbidden, "content blocked by safety policy")
	return true
}

func isWebURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func urlKey(u string) string {
	sum := sha1.Sum([]byte(urlnorm.Canonical(u)))
	return hex.EncodeToString(sum[:])
}

func cleanMagnet(m string) string {
	if decoded, err := url.QueryUnescape(m); err == nil {
		m = decoded
	}
	m = html.UnescapeString(m)
	if h := strings.TrimSpace(m); magnet.Valid(h) {
		return magnet.Build(h, "")
	}
	return m
}

func extractInfoHash(m string) string {
	return magnet.Hash(cleanMagnet(m))
}

func clientIP(r *http.Request) string {
	return middleware.ClientIP(r)
}

func (s *Server) signStreams(job *jobs.Job, r *http.Request) []jobs.Stream {
	byos := false
	if s.Users != nil {
		if creds, err := s.Users.GetStorageCreds(r.Context(), job.UserID); err == nil && creds != nil && creds.Enabled && creds.IsRclone() {
			byos = true
		}
	}
	node := job.Node
	if byos {
		if n, ok := s.warmCachedNode(r.Context(), job.InfoHash); ok {
			byos = false
			if n != "" {
				node = n
			}
		}
	}
	out := make([]jobs.Stream, len(job.Files))
	for i, f := range job.Files {
		key := manifest.ResolveKey(job.InfoHash, i, f.Key, f.Name)
		var u string
		if byos {
			u = s.Store.SignURLNodeUser(job.Node, key, job.UserID, 24*time.Hour) + "&byos=1"
			u += "&bk=" + url.QueryEscape(manifest.Key(job.InfoHash, i, f.Name))
		} else if _, _, _, ok := cairn.ParseStreamPath(key); ok {
			u = s.Store.SignURLNodeUser("", key, job.UserID, 24*time.Hour)
		} else {
			u = s.Store.SignURLNode(node, key, 24*time.Hour)
		}
		u += manifest.StreamQuery(job.InfoHash, f.Enc)
		out[i] = jobs.Stream{FileName: f.Name, Size: f.Size, SignedURL: georoute.URL(r, u)}
	}
	return out
}

func (s *Server) warmCachedNode(ctx context.Context, hash string) (string, bool) {
	lookup := s.cairnCachedLookup()
	if lookup == nil {
		return "", false
	}
	byHash, _ := lookup.CachedByHashes(ctx, []string{hash})
	j := byHash[hash]
	if j == nil {
		return "", false
	}
	return j.Node, true
}

func (s *Server) serveFromBYOS(w http.ResponseWriter, r *http.Request, infoHash, magnet string, source jobs.Source) bool {
	if s.Users == nil || s.JobsPG == nil {
		return false
	}
	user := middleware.GetUser(r)
	obj, ok := s.JobsPG.GetBYOSObjectByUserHash(r.Context(), user.ID, infoHash)
	if !ok {
		return false
	}
	var total int64
	for _, f := range obj.Files {
		total += f.Size
	}
	job := &jobs.Job{
		UserID: user.ID, InfoHash: infoHash, Magnet: magnet, Name: obj.Name,
		Source: source, Status: jobs.StatusComplete, Files: obj.Files, FileSize: total,
	}
	created, err := jobs.CreateAccountOnce(r.Context(), s.Jobs, job)
	if err != nil {
		web.WriteError(w, 500, "could not restore from your storage")
		return true
	}
	if created {
		s.Bus.Publish(events.ByosRehydrate, events.RehydrateBYOS{JobID: job.ID, UserID: user.ID, InfoHash: infoHash, Node: job.Node})
	}
	job.StreamURLs = s.signStreams(job, r)
	web.WriteJSON(w, 200, job)
	return true
}

func (s *Server) buildCachedJob(ctx context.Context, infoHash, magnet, userID string, source jobs.Source) (*jobs.Job, error) {
	data, err := s.Store.GetBytes(ctx, manifest.Path(infoHash))
	if err != nil {
		return nil, err
	}
	man, err := manifest.Parse(data)
	if err != nil {
		return nil, err
	}
	files := make([]jobs.File, len(man.Files))
	var total int64
	for i, mf := range man.Files {
		files[i] = jobs.File{Index: i, Name: mf.FileName, Size: mf.FileSize, Key: mf.DirectURL, Enc: mf.Enc, MediaInfo: mf.MediaInfo}
		total += mf.FileSize
	}
	job := &jobs.Job{
		UserID:   userID,
		InfoHash: infoHash,
		Name:     man.Name,
		Magnet:   magnet,
		Source:   source,
		Status:   jobs.StatusComplete,
		Files:    files,
		FileSize: total,
	}
	if _, err := jobs.CreateAccountOnce(ctx, s.Jobs, job); err != nil {
		return nil, err
	}
	return job, nil
}
