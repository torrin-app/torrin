package handlers

import (
	"net/http"

	"github.com/torrin-app/torrin/api/internal/web"
	"github.com/torrin-app/torrin/shared/blob"
)

func (s *Server) adminCache(w http.ResponseWriter, r *http.Request) {
	top, _ := s.JobsPG.CacheTopItems(r.Context(), 20)
	candidates, _ := s.JobsPG.CacheEvictionCandidates(r.Context(), 20)
	total, _ := s.JobsPG.GetTotalCachedSize(r.Context())

	web.WriteJSON(w, 200, map[string]any{
		"top":                  top,
		"eviction_candidates":  candidates,
		"total_size":           total,
		"total_size_formatted": formatBytes(total),
	})
}

func (s *Server) adminEvictCache(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		web.WriteError(w, 400, "hash required")
		return
	}
	if err := s.Store.DeletePrefix(r.Context(), hash+"/"); err != nil {
		web.WriteError(w, 500, "internal error")
		return
	}
	if orphaned, err := s.JobsPG.DropBlobRefs(r.Context(), hash); err == nil {
		for _, ck := range orphaned {
			if s.Store.Delete(r.Context(), blob.StorageKey(ck)) == nil {
				s.JobsPG.DeleteBlob(r.Context(), ck)
			}
		}
	}
	s.JobsPG.DeleteCachedByHash(r.Context(), hash)
	web.WriteJSON(w, 200, map[string]string{"status": "evicted"})
}
