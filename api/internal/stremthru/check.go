package stremthru

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/magnet"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

const checkBudget = 20 * time.Second

func (h *Handler) checkMagnets(w http.ResponseWriter, r *http.Request, user *auth.User) {
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

	// Tier 1: our shared cache. Fan out (each is a storage round-trip) so a 150-hash
	// batch doesn't take ~24s and make the client drop the whole thing to uncached.
	fanOut(uncached(), func(hash string) {
		name, files, ok := h.warmCachedFiles(ctx, user.ID, hash)
		if !ok {
			return
		}
		mu.Lock()
		if name == "" {
			name, _ = items[idxOf[hash]]["name"].(string)
		}
		items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "files": files}
		mu.Unlock()
	})

	// Tier 1.5: complete on another node (per-box cache) — the local store can't see it,
	// so one batched lookup in the shared jobs DB; links route to the job's node.
	if still := uncached(); len(still) > 0 {
		if h.Jobs != nil {
			byHash, _ := h.Jobs.CachedByHashes(ctx, still)
			mu.Lock()
			for hash, job := range byHash {
				if len(job.Files) == 0 {
					continue
				}
				name := job.Name
				if name == "" {
					name, _ = items[idxOf[hash]]["name"].(string)
				}
				items[idxOf[hash]] = map[string]any{"hash": hash, "magnet": magnet.Build(hash, name), "status": "cached", "name": name, "files": h.buildFileEntries(user.ID, hash, job.Node, job.Files)}
			}
			mu.Unlock()
		}
	}

	// Tier 1.75: a durable Cairn is immediately playable even when its warm
	// cache copy was evicted. Prefer local and cross-node warm copies above.
	fanOut(uncached(), func(hash string) {
		name, files, ok := h.cairnCachedFiles(ctx, user.ID, hash)
		if !ok {
			return
		}
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
		if pURL, _, _, _ := h.Jobs.ReleaseLink(ctx, hash); pURL != "" {
			setStatus(hash, "acceleratable")
			return
		}
		if h.Users.IsInADLibrary(ctx, hash) {
			setStatus(hash, "acceleratable")
		}
	})

	if still := uncached(); len(still) > 0 {
		h.liveCacheCheck(ctx, user, byok, still, setStatus)
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
		if byok {
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

	if byok {
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
				for hash := range providers.TorBoxCached(ctx, k, hashes) {
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
