package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/usenet/download"
	"github.com/torrin-app/torrin/shared/usenet/nzb"
)

type usenetUsers interface {
	GetJobNZB(ctx context.Context, infoHash string) ([]byte, bool)
	GetUsenetCreds(ctx context.Context, userID string) (*auth.UsenetCreds, error)
	GetByID(ctx context.Context, userID string) (*auth.User, error)
}

type usenetBackend struct {
	users    usenetUsers
	sysCreds []download.Credentials
}

func (s *Server) SetUsenet(users *auth.Store, sysCreds []download.Credentials) {
	s.usenet = &usenetBackend{users: users, sysCreds: sysCreds}
}

type nzbByteStore interface {
	GetBytes(ctx context.Context, key string) ([]byte, error)
}

// nzb reads the release NZB from Postgres first (written synchronously, so no
// fresh-write object-store listing lag), falling back to the object store.
func (u *usenetBackend) nzb(ctx context.Context, infoHash, nzbKey string, store nzbByteStore) ([]byte, bool) {
	if u.users != nil {
		if data, ok := u.users.GetJobNZB(ctx, infoHash); ok && len(data) > 0 {
			return data, true
		}
	}
	data, err := store.GetBytes(ctx, nzbKey)
	return data, err == nil && len(data) > 0
}

func (s *Server) serveUsenetStream(w http.ResponseWriter, r *http.Request, nzbKey, userID string) {
	u := s.usenet
	if u == nil {
		httpError(w, 503, "usenet streaming unavailable")
		return
	}
	infoHash := strings.TrimSuffix(strings.TrimPrefix(nzbKey, "nzb/"), ".nzb")
	if infoHash == "" {
		httpError(w, 503, "usenet streaming unavailable")
		return
	}
	data, ok := u.nzb(r.Context(), infoHash, nzbKey, s.store)
	if !ok {
		httpError(w, 404, "nzb not found")
		return
	}
	parsed, err := nzb.ParseBytes(data)
	if err != nil {
		httpError(w, 422, "invalid nzb")
		return
	}
	creds := u.creds(r.Context(), userID)
	if len(creds) == 0 {
		httpError(w, 403, "no usenet provider configured")
		return
	}

	pools, release, err := download.AcquireSharedMulti(creds)
	if err != nil {
		httpError(w, 503, "usenet provider unavailable")
		return
	}
	defer release()

	if !download.CensusRelease(r.Context(), pools, parsed).Streamable() {
		httpError(w, 409, "release too damaged to stream")
		return
	}

	reader, total, err := download.OpenStream(r.Context(), pools, parsed)
	if err != nil || total <= 0 {
		httpError(w, 409, "release is not streamable")
		return
	}

	start, end, status := int64(0), total-1, http.StatusOK
	if rng := r.Header.Get("Range"); rng != "" {
		s2, e2, ok := parseByteRange(rng, total)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
			httpError(w, http.StatusRequestedRangeNotSatisfiable, "range not satisfiable")
			return
		}
		start, end, status = s2, e2, http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	}
	h := w.Header()
	h.Set("Content-Type", usenetContentType(nzbKey))
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}
	s.recordView(r, infoHash+"/")
	w.WriteHeader(status)
	streamCopy(w, io.NewSectionReader(reader, start, end-start+1))
}

func usenetContentType(name string) string {
	if strings.HasSuffix(strings.ToLower(name), ".mp4") {
		return "video/mp4"
	}
	return "video/x-matroska"
}

func (u *usenetBackend) creds(ctx context.Context, userID string) []download.Credentials {
	if c, err := u.users.GetUsenetCreds(ctx, userID); err == nil && c.Host != "" {
		return []download.Credentials{{Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, SSL: c.SSL, MaxConns: c.MaxConns}}
	}
	if usr, err := u.users.GetByID(ctx, userID); err == nil && usr != nil {
		if p, ok := plans.Get(usr.PlanID); ok && p.SystemUsenet {
			return u.sysCreds
		}
	}
	return nil
}
