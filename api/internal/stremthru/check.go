package stremthru

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/availcache"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

const checkBudget = 20 * time.Second

func (h *Handler) checkMagnets(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var trace *checkTrace
	if r.URL.Query().Get("diagnostics") == "1" {
		trace = &checkTrace{byHash: make(map[string]map[string]checkObservation)}
	}
	target, validTarget := playbackTarget(r.URL.Query().Get("sid"))
	if !validTarget {
		stError(w, 400, "invalid series sid")
		return
	}
	var magnets []string
	for _, key := range []string{"magnet", "hash"} {
		for _, p := range r.URL.Query()[key] {
			for _, m := range strings.Split(p, ",") {
				if m = strings.TrimSpace(m); m != "" {
					magnets = append(magnets, m)
				}
			}
		}
	}
	if len(magnets) == 0 {
		stJSON(w, 200, map[string]any{"data": map[string]any{"items": []any{}}})
		return
	}
	if trace != nil && len(magnets) > 500 {
		stError(w, http.StatusBadRequest, "too many hashes for diagnostics")
		return
	}

	type entry struct{ hash, magnet string }
	var valid []entry
	items := []map[string]any{}
	idxOf := map[string]int{}
	for _, m := range magnets {
		hash := strings.ToLower(strings.TrimSpace(m))
		if strings.HasPrefix(hash, "magnet:") {
			hash = extractHash(hash)
		}
		if len(hash) != 40 {
			items = append(items, map[string]any{"hash": hash, "magnet": m, "status": "unknown", "name": displayName(m), "files": []any{}})
			continue
		}
		idxOf[hash] = len(items)
		items = append(items, map[string]any{"hash": hash, "magnet": magnet.Build(hash, displayName(m)), "status": "unknown", "name": displayName(m), "files": []any{}})
		valid = append(valid, entry{hash, m})
	}

	ctx, cancel := context.WithTimeout(r.Context(), checkBudget)
	defer cancel()
	byok := plans.CanBYOK(user.PlanID)

	var mu sync.Mutex
	setStatus := func(hash, status string) {
		mu.Lock()
		if idx, ok := idxOf[hash]; ok && items[idx]["status"] == "unknown" {
			items[idx]["status"] = status
		}
		mu.Unlock()
	}
	uncached := func() []string {
		var hs []string
		mu.Lock()
		for _, e := range valid {
			if items[idxOf[e.hash]]["status"] == "unknown" {
				hs = append(hs, e.hash)
			}
		}
		mu.Unlock()
		return hs
	}
	fanOut := func(hashes []string, fn func(hash string)) {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 32)
		for _, hash := range hashes {
			wg.Add(1)
			sem <- struct{}{}
			go func(hash string) {
				defer wg.Done()
				defer func() { <-sem }()
				fn(hash)
			}(hash)
		}
		wg.Wait()
	}

	// Which of these are cached jobs at all? One batched DB query up front so Tier 1
	// storage-verifies only real candidates. Probing every hash against the blob store
	// made a big batch (130+) blow the budget under disk load and return 0 cached.
	allHashes := make([]string, len(valid))
	for i, e := range valid {
		allHashes[i] = e.hash
	}
	warmByHash := map[string]*jobs.Job{}
	if lookup := h.cachedLookup(); lookup != nil {
		var err error
		warmByHash, err = lookup.CachedByHashes(ctx, allHashes)
		for _, hash := range allHashes {
			if err != nil {
				trace.record(hash, "warm_cache", checkFailure(ctx, err, "database_error"))
				trace.record(hash, "cross_node_cache", checkFailure(ctx, err, "database_error"))
			} else if warmByHash[hash] == nil {
				trace.record(hash, "warm_cache", "not_cached")
				trace.record(hash, "cross_node_cache", "not_cached")
			}
		}
	} else {
		for _, hash := range allHashes {
			trace.record(hash, "warm_cache", "not_configured")
			trace.record(hash, "cross_node_cache", "not_configured")
		}
	}
	warmCandidates := func() []string {
		var hs []string
		for _, hash := range uncached() {
			if warmByHash[hash] != nil {
				hs = append(hs, hash)
			}
		}
		return hs
	}

	// Tier 1: a cached job on this node — storage-verify the blob is present (catches
	// eviction). Only the candidates, so the round-trips stay inside the budget.
	fanOut(warmCandidates(), func(hash string) {
		cached, reason := h.inspectWarmJobFiles(ctx, hash)
		trace.record(hash, "warm_cache", reason)
		if reason != "cached" {
			return
		}
		name := cached.name
		selected := jobs.FilesForEpisode(nil, cached.files, target.Season, target.Episode)
		if len(selected) == 0 {
			trace.record(hash, "warm_cache", "episode_not_in_cached_release")
		}
		files := h.buildFileEntries(user.ID, hash, cached.node, selected)
		mu.Lock()
		if name == "" {
			name, _ = items[idxOf[hash]]["name"].(string)
		}
		items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "files": files}
		mu.Unlock()
	})

	// Tier 1.5: a cached job on another node — the local store can't see it, so trust
	// the DB row already fetched; links route to the job's node.
	if still := uncached(); len(still) > 0 {
		mu.Lock()
		for _, hash := range still {
			job := warmByHash[hash]
			if job == nil {
				continue
			}
			if !isWarmNodeJob(hash, job) {
				trace.record(hash, "cross_node_cache", "not_applicable")
				continue
			}
			trace.record(hash, "cross_node_cache", "cached")
			name := job.Name
			if name == "" {
				name, _ = items[idxOf[hash]]["name"].(string)
			}
			selected := jobs.FilesForEpisode(job, job.Files, target.Season, target.Episode)
			if len(selected) == 0 {
				trace.record(hash, "cross_node_cache", "episode_not_in_cached_release")
			}
			items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "files": h.buildFileEntries(user.ID, hash, job.Node, selected)}
		}
		mu.Unlock()
	}

	// Tier 1.75: a durable Cairn is immediately playable even when its warm
	// cache copy was evicted. Prefer local and cross-node warm copies above.
	fanOut(uncached(), func(hash string) {
		cached, reason := h.inspectCairnJobFiles(ctx, hash)
		trace.record(hash, "durable_archive", reason)
		if reason != "cached" {
			return
		}
		name := cached.name
		selected := jobs.FilesForEpisode(nil, cached.files, target.Season, target.Episode)
		if len(selected) == 0 {
			trace.record(hash, "durable_archive", "episode_not_in_cached_release")
		}
		files := h.buildFileEntries(user.ID, hash, cached.node, selected)
		mu.Lock()
		if name == "" {
			name, _ = items[idxOf[hash]]["name"].(string)
		}
		items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "files": files}
		mu.Unlock()
	})

	// Tier 1b: known release links (hdencode/scene-rls) are fetchable via AD + unrar.
	// Tier 2: system AD library (torrin's own shared pool), fast DB lookup.
	fanOut(uncached(), func(hash string) {
		if h.Jobs == nil {
			trace.record(hash, "release_link", "not_configured")
		} else {
			pURL, _, _, _, err := h.Jobs.LookupReleaseLink(ctx, hash)
			if err != nil {
				trace.record(hash, "release_link", checkFailure(ctx, err, "database_error"))
			} else if pURL != "" {
				trace.record(hash, "release_link", "release_link_unverified")
				setStatus(hash, "acceleratable")
				return
			} else {
				trace.record(hash, "release_link", "not_cached")
			}
		}
		if h.Users == nil {
			trace.record(hash, "shared_library", "not_configured")
			return
		}
		found, err := h.Users.LookupADLibrary(ctx, hash)
		if err != nil {
			trace.record(hash, "shared_library", checkFailure(ctx, err, "database_error"))
			return
		}
		if found {
			trace.record(hash, "shared_library", "acceleratable")
			setStatus(hash, "acceleratable")
		} else {
			trace.record(hash, "shared_library", "not_cached")
		}
	})

	if still := uncached(); len(still) > 0 {
		h.liveCacheCheck(ctx, user, byok, still, setStatus, trace)
	}

	trace.attach(ctx, items)
	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items}})
}

func (h *Handler) liveCacheCheck(ctx context.Context, user *auth.User, byok bool, hashes []string, setStatus func(hash, status string), trace *checkTrace) {
	var wg sync.WaitGroup
	run := func(fn func()) { wg.Add(1); go func() { defer wg.Done(); fn() }() }
	mark := func(tier, reason string) {
		for _, hash := range hashes {
			trace.record(hash, tier, reason)
		}
	}
	apply := func(tier string, result availcache.CheckResult) {
		for _, hash := range hashes {
			if cached, known := result.Cached[hash]; known {
				if cached {
					trace.record(hash, tier, "acceleratable")
					setStatus(hash, "acceleratable")
				} else {
					trace.record(hash, tier, "not_cached")
				}
			} else {
				reason := result.Reasons[hash]
				if reason == "" {
					reason = "partial_response"
				}
				trace.record(hash, tier, reason)
			}
		}
	}
	// System and personal credentials stay distinct; no key material enters diagnostics.
	tcCheck := func(tier, provider, key string) {
		if h.TC == nil || key == "" {
			mark(tier, "not_configured")
			return
		}
		run(func() { apply(tier, h.TC.CheckCacheDetailed(ctx, provider, key, hashes)) })
	}
	tcCheck("system_alldebrid", "alldebrid", h.SysADKey)
	tcCheck("system_real_debrid", "real-debrid", h.SysRDKey)
	personal := func(tier string, getter func(context.Context, string) (string, error), check func(string)) {
		if !byok {
			mark(tier, "plan_not_eligible")
			return
		}
		if h.Users == nil {
			mark(tier, "not_configured")
			return
		}
		run(func() {
			key, err := getter(ctx, user.ID)
			if err != nil {
				mark(tier, checkFailure(ctx, err, "database_error"))
				return
			}
			if key == "" {
				mark(tier, "not_configured")
				return
			}
			check(key)
		})
	}
	if h.TC == nil {
		mark("personal_alldebrid", "not_configured")
		mark("personal_real_debrid", "not_configured")
	} else {
		personal("personal_alldebrid", h.Users.GetADKey, func(key string) { tcCheck("personal_alldebrid", "alldebrid", key) })
		personal("personal_real_debrid", h.Users.GetRDKey, func(key string) { tcCheck("personal_real_debrid", "real-debrid", key) })
	}
	personal("personal_premiumize", h.Users.GetPMKey, func(key string) { apply("personal_premiumize", providers.PremiumizeCachedDetailed(ctx, key, hashes)) })
	personal("personal_torbox", h.Users.GetTBKey, func(key string) { apply("personal_torbox", providers.TorBoxCachedDetailed(ctx, key, hashes)) })
	personal("personal_offcloud", h.Users.GetOCKey, func(key string) { apply("personal_offcloud", providers.OffcloudCachedDetailed(ctx, key, hashes)) })
	wg.Wait()
}
