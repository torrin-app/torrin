package handlers

import (
	"net/http"

	"github.com/torrin-app/torrin/api/internal/web"
)

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Users.AdminStats(r.Context())
	if err != nil {
		web.WriteError(w, 500, "could not load stats")
		return
	}
	jobCounts, _ := s.JobsPG.JobStatusCounts(r.Context())
	cachedSize, _ := s.JobsPG.GetTotalCachedSizeAll(r.Context())
	budgetUsed, _ := s.JobsPG.BudgetUsed(r.Context())
	nodeSummary, _ := s.JobsPG.NodeSummary(r.Context())

	web.WriteJSON(w, 200, map[string]any{
		"users": map[string]any{
			"total":         stats.TotalUsers,
			"active":        stats.ActiveUsers,
			"free_active":   stats.FreeActive,
			"expired":       stats.Expired,
			"paid_by_plan":  stats.PaidByPlan,
			"by_recurrence": stats.ByRecurrence,
		},
		"jobs": map[string]any{
			"by_status":             jobCounts,
			"cached_size":           cachedSize,
			"cached_size_formatted": formatBytes(cachedSize),
			"by_node":               nodeSummary,
		},
		"budget": map[string]any{
			"used":      budgetUsed,
			"available": s.Budget - budgetUsed,
		},
		"rd_keys_count":         stats.CredCounts["rd"],
		"ad_keys_count":         stats.CredCounts["ad"],
		"pm_keys_count":         stats.CredCounts["pm"],
		"tb_keys_count":         stats.CredCounts["tb"],
		"oc_keys_count":         stats.CredCounts["oc"],
		"usenet_creds_count":    stats.CredCounts["usenet"],
		"usenet_indexers_count": stats.CredCounts["indexers"],
		"rss_feeds_count":       stats.CredCounts["rss"],
	})
}
