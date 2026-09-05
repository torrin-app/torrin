package stremthru

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/jobs"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

const checkBudget = 20 * time.Second

func (h *Handler) checkMagnets(w http.ResponseWriter, r *http.Request, user *auth.User) {
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
		if _, exists := idxOf[hash]; exists {
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
	copyMatches := map[string]string{}
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
		warmByHash, _ = lookup.CachedByHashes(ctx, allHashes)
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

	// A miss applies only to this copy. Continue through other storage and upstream tiers.
	setCached := func(hash string, cached playableJobFiles, job *jobs.Job) {
		if job == nil {
			job = &jobs.Job{Name: cached.name}
		}
		selected, match := h.EpisodeResolver.Assess(ctx, target.IMDBID, job, cached.files, target.Season, target.Episode)
		mu.Lock()
		defer mu.Unlock()
		if len(selected) == 0 {
			// Any ambiguous copy prevents a definitive rejection of available files.
			if previous := copyMatches[hash]; previous == "" || match == "unknown" {
				copyMatches[hash] = match
			}
			return
		}
		name := cached.name
		if name == "" {
			name, _ = items[idxOf[hash]]["name"].(string)
		}
		cached.name = name
		items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "private": cached.byos,
			"files": h.playableEntries(user.ID, hash, cached, selected, target)}
		if job.FileSize > 0 {
			for _, file := range items[idxOf[hash]]["files"].([]map[string]any) {
				file["release_size"] = job.FileSize
			}
		}
		if target.IsEpisode() {
			items[idxOf[hash]]["episode_status"] = "match"
			items[idxOf[hash]]["episode_sid"] = r.URL.Query().Get("sid")
		}
	}
	fanOut(warmCandidates(), func(hash string) {
		if cached, ok := h.warmJobFiles(ctx, hash); ok {
			setCached(hash, cached, warmByHash[hash])
		}
	})
	for _, hash := range uncached() {
		job := warmByHash[hash]
		if isWarmNodeJob(hash, job) {
			setCached(hash, playableJobFiles{name: job.Name, files: job.Files, node: job.Node}, job)
		}
	}
	fanOut(uncached(), func(hash string) {
		if cached, ok := h.cairnJobFiles(ctx, hash); ok {
			setCached(hash, cached, nil)
		}
	})

	for hash, o := range h.privateCopies(ctx, user.ID, uncached()) {
		setCached(hash, playableJobFiles{name: o.Name, files: o.Files, byos: true}, privateJob(o))
	}

	// Tier 1b: known release links (hdencode/scene-rls) are fetchable via AD + unrar.
	// Tier 2: system AD library (torrin's own shared pool), fast DB lookup.
	fanOut(uncached(), func(hash string) {
		if h.Jobs != nil {
			if pURL, _, _, _ := h.Jobs.ReleaseLink(ctx, hash); pURL != "" {
				setStatus(hash, "acceleratable")
				return
			}
		}
		if h.Users != nil && h.Users.IsInADLibrary(ctx, hash) {
			setStatus(hash, "acceleratable")
		}
	})

	if still := uncached(); len(still) > 0 {
		h.liveCacheCheck(ctx, user, byok, still, setStatus)
	}

	if target.IsEpisode() {
		for _, e := range valid {
			item := items[idxOf[e.hash]]
			if item["episode_status"] == "match" {
				continue
			}
			item["episode_sid"] = r.URL.Query().Get("sid")
			item["episode_status"] = "unknown"
			if item["status"] == "unknown" && copyMatches[e.hash] == "no_match" {
				item["episode_status"] = "no_match"
				item["episode_scope"] = "available_files"
				item["reason"] = "episode_not_found"
			}
		}
	}
	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items}})
}

func (h *Handler) liveCacheCheck(ctx context.Context, user *auth.User, byok bool, hashes []string, setStatus func(hash, status string)) {
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}

	// Tier 3: live TorrentClaw check across every provider the user can stream from
	// (system AD/RD + their BYOK). Runs on TorrentClaw's side.
	if h.TC != nil {
		type pk struct{ provider, key string }
		var checks []pk
		if h.SysADKey != "" {
			checks = append(checks, pk{"alldebrid", h.SysADKey})
		}
		if h.SysRDKey != "" {
			checks = append(checks, pk{"real-debrid", h.SysRDKey})
		}
		if byok && h.Users != nil {
			if k, _ := h.Users.GetRDKey(ctx, user.ID); k != "" {
				checks = append(checks, pk{"real-debrid", k})
			}
			if k, _ := h.Users.GetADKey(ctx, user.ID); k != "" {
				checks = append(checks, pk{"alldebrid", k})
			}
		}
		for _, c := range checks {
			c := c
			run(func() {
				for hash, ok := range h.TC.CheckCache(ctx, c.provider, c.key, hashes) {
					if ok {
						setStatus(strings.ToLower(hash), "acceleratable")
					}
				}
			})
		}
	}

	if byok && h.Users != nil {
		// Tier 4: the user's Premiumize cache, checked directly.
		run(func() {
			if k, _ := h.Users.GetPMKey(ctx, user.ID); k != "" {
				for hash, ok := range providers.PremiumizeCached(ctx, k, hashes) {
					if ok {
						setStatus(hash, "acceleratable")
					}
				}
			}
		})
		// Tier 5: the user's TorBox cache, checked directly.
		run(func() {
			if k, _ := h.Users.GetTBKey(ctx, user.ID); k != "" {
				for hash, cached := range providers.TorBoxCached(ctx, k, hashes) {
					if !cached {
						continue
					}
					setStatus(hash, "acceleratable")
				}
			}
		})
		// Tier 6: the user's Offcloud cache, checked directly.
		run(func() {
			if k, _ := h.Users.GetOCKey(ctx, user.ID); k != "" {
				for hash, ok := range providers.OffcloudCached(ctx, k, hashes) {
					if ok {
						setStatus(hash, "acceleratable")
					}
				}
			}
		})
	}

	wg.Wait()
}
