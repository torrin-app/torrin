package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/manifest"
	"github.com/torrin-app/torrin/shared/relabel"
)

const relabelTrackers = "&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.demonii.com:1337/announce&tr=udp://exodus.desync.com:6969/announce"

func (s *Server) adminRelabel(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	if len(hash) != 40 {
		web.WriteError(w, 400, "invalid info hash")
		return
	}
	if s.Qbit == nil {
		web.WriteError(w, 503, "qbit unavailable")
		return
	}
	data, err := s.Store.GetBytes(r.Context(), manifest.Path(hash))
	if err != nil {
		web.WriteError(w, 404, "not cached")
		return
	}
	man, err := manifest.Parse(data)
	if err != nil {
		web.WriteError(w, 500, "could not read manifest")
		return
	}

	real, err := s.torrentFiles(r.Context(), hash)
	if err != nil {
		web.WriteError(w, 502, "could not fetch torrent metadata")
		return
	}
	cached := make([]relabel.NamedSize, len(man.Files))
	for i, f := range man.Files {
		cached[i] = relabel.NamedSize{Name: f.FileName, Size: f.FileSize}
	}
	res := relabel.Match(real, cached)

	if r.URL.Query().Get("dryrun") == "1" {
		web.WriteJSON(w, 200, map[string]any{
			"matched":   len(res.Mapping),
			"ambiguous": res.Ambiguous,
			"unmatched": res.Unmatched,
			"mapping":   res.Mapping,
		})
		return
	}
	if len(res.Unmatched) > 0 || len(res.Ambiguous) > 0 {
		web.WriteError(w, 409, fmt.Sprintf("refusing partial rename: %d unmatched, %d ambiguous", len(res.Unmatched), len(res.Ambiguous)))
		return
	}

	for i := range man.Files {
		if nn := res.Mapping[man.Files[i].FileName]; nn != "" {
			man.Files[i].FileName = nn
		}
	}
	out, err := man.Marshal()
	if err != nil {
		web.WriteError(w, 500, "could not encode manifest")
		return
	}
	if err := s.Store.Put(r.Context(), manifest.Path(hash), bytes.NewReader(out), "application/json"); err != nil {
		web.WriteError(w, 500, "could not write manifest")
		return
	}

	list, _ := s.Jobs.ListByInfoHash(r.Context(), hash)
	rows := 0
	for _, j := range list {
		changed := false
		for i := range j.Files {
			if nn := res.Mapping[j.Files[i].Name]; nn != "" {
				j.Files[i].Name = nn
				changed = true
			}
		}
		if changed {
			s.Jobs.Update(r.Context(), j)
			rows++
		}
	}
	web.WriteJSON(w, 200, map[string]any{"renamed": len(res.Mapping), "jobs_updated": rows})
}

func (s *Server) torrentFiles(ctx context.Context, hash string) ([]relabel.NamedSize, error) {
	if existing, err := s.Qbit.GetTorrent(hash); err != nil || existing == nil {
		if err := s.Qbit.AddMagnet("magnet:?xt=urn:btih:" + hash + relabelTrackers); err != nil {
			return nil, err
		}
		defer s.Qbit.Delete(hash)
	}
	for i := 0; i < 20; i++ {
		if files, err := s.Qbit.GetFiles(hash); err == nil && len(files) > 0 {
			out := make([]relabel.NamedSize, len(files))
			for j, f := range files {
				out[j] = relabel.NamedSize{Name: f.Name, Size: f.Size}
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, fmt.Errorf("metadata not received")
}
