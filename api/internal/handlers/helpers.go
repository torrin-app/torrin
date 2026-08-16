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
	"github.com/torrin-app/torrin/shared/events"
	"github.com/torrin-app/torrin/shared/georoute"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/manifest"
)

func isWebURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func urlKey(u string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(u)))
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
	if cfip := r.Header.Get("CF-Connecting-IP"); cfip != "" {
		return cfip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}

func (s *Server) signStreams(job *jobs.Job, r *http.Request) []jobs.Stream {
	byos := false
	if s.Users != nil {
		if creds, err := s.Users.GetStorageCreds(r.Context(), job.UserID); err == nil && creds != nil && creds.Enabled && creds.IsRclone() {
			byos = true
		}
	}
	if byos {
		if cached, _ := s.Store.Has(r.Context(), manifest.Path(job.InfoHash)); cached {
			byos = false
		}
	}
	out := make([]jobs.Stream, len(job.Files))
	for i, f := range job.Files {
		key := manifest.ResolveKey(job.InfoHash, i, f.Key, f.Name)
		var u string
		if byos {
			u = s.Store.SignURLNodeUser(job.Node, key, job.UserID, 24*time.Hour) + "&byos=1"
		} else {
			u = s.Store.SignURLNode(job.Node, key, 24*time.Hour)
		}
		u += manifest.StreamQuery(job.InfoHash, key, f.Enc)
		out[i] = jobs.Stream{FileName: f.Name, Size: f.Size, SignedURL: georoute.URL(r, u)}
	}
	return out
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
	if err := s.Jobs.Create(r.Context(), job); err != nil {
		web.WriteError(w, 500, "could not restore from your storage")
		return true
	}
	s.Bus.Publish(events.ByosRehydrate, events.RehydrateBYOS{JobID: job.ID, UserID: user.ID, InfoHash: infoHash, Node: job.Node})
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
	if err := s.Jobs.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}
