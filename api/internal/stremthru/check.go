package stremthru

import (
	"net/http"
	"strings"
	"sync"

	"github.com/torrin-app/torrin/shared/auth"
	"github.com/torrin-app/torrin/shared/plans"
	"github.com/torrin-app/torrin/shared/providers"
)

func (h *Handler) checkMagnets(w http.ResponseWriter, r *http.Request, user *auth.User) {
	var magnets []string
	for _, p := range r.URL.Query()["magnet"] {
		for _, m := range strings.Split(p, ",") {
			if m = strings.TrimSpace(m); m != "" {
				magnets = append(magnets, m)
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
		items = append(items, map[string]any{"hash": hash, "magnet": m, "status": "unknown", "name": displayName(m), "files": []any{}})
		valid = append(valid, entry{hash, m})
	}

	byok := plans.CanBYOK(user.PlanID)

	uncached := func() []string {
		var hs []string
		for _, e := range valid {
			if items[idxOf[e.hash]]["status"] == "unknown" {
				hs = append(hs, e.hash)
			}
		}
		return hs
	}

	// Tier 1: our shared cache. Fan out (each is a storage round-trip) so a 150-hash
	// batch doesn't take ~24s and make the client drop the whole thing to uncached.
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 32)
	for _, e := range valid {
		wg.Add(1)
		sem <- struct{}{}
		go func(e entry) {
			defer wg.Done()
			defer func() { <-sem }()
			name, files, ok := h.cachedFiles(r.Context(), e.hash)
			if !ok {
				return
			}
			if name == "" {
				name = displayName(e.magnet)
			}
			mu.Lock()
			items[idxOf[e.hash]] = map[string]any{"hash": e.hash, "magnet": e.magnet, "status": "cached", "name": name, "files": files}
			mu.Unlock()
		}(e)
	}
	wg.Wait()

	// Tier 1b: known release links (hdencode/scene-rls) are fetchable via AD + unrar.
	for _, hash := range uncached() {
		if pURL, _, _, _ := h.Jobs.ReleaseLink(r.Context(), hash); pURL != "" {
			items[idxOf[hash]]["status"] = "acceleratable"
		}
	}

	// Tier 2: system AD library (torrin's own shared pool) — fast DB lookup.
	for _, hash := range uncached() {
		if h.Users.IsInADLibrary(r.Context(), hash) {
			items[idxOf[hash]]["status"] = "acceleratable"
		}
	}

	// Tier 3: live TorrentClaw check across every provider the user can stream from
	// (system AD/RD + their BYOK). Runs on TorrentClaw's side.
	if h.TC != nil {
		if still := uncached(); len(still) > 0 {
			type pk struct{ provider, key string }
			var checks []pk
			if h.SysADKey != "" {
				checks = append(checks, pk{"alldebrid", h.SysADKey})
			}
			if h.SysRDKey != "" {
				checks = append(checks, pk{"real-debrid", h.SysRDKey})
			}
			if byok {
				if k, _ := h.Users.GetRDKey(r.Context(), user.ID); k != "" {
					checks = append(checks, pk{"real-debrid", k})
				}
				if k, _ := h.Users.GetADKey(r.Context(), user.ID); k != "" {
					checks = append(checks, pk{"alldebrid", k})
				}
				if k, _ := h.Users.GetTBKey(r.Context(), user.ID); k != "" {
					checks = append(checks, pk{"torbox", k})
				}
				if k, _ := h.Users.GetPMKey(r.Context(), user.ID); k != "" {
					checks = append(checks, pk{"premiumize", k})
				}
			}
			var cmu sync.Mutex
			var cwg sync.WaitGroup
			for _, c := range checks {
				cwg.Add(1)
				go func(c pk) {
					defer cwg.Done()
					cached := h.TC.CheckCache(r.Context(), c.provider, c.key, still)
					cmu.Lock()
					for hash, ok := range cached {
						if ok {
							if idx, found := idxOf[strings.ToLower(hash)]; found && items[idx]["status"] == "unknown" {
								items[idx]["status"] = "acceleratable"
							}
						}
					}
					cmu.Unlock()
				}(c)
			}
			cwg.Wait()
		}
	}

	// Tier 4: the user's Premiumize cache, checked directly.
	if pm := uncached(); len(pm) > 0 && byok {
		if k, _ := h.Users.GetPMKey(r.Context(), user.ID); k != "" {
			for hash, ok := range providers.PremiumizeCached(r.Context(), k, pm) {
				if ok {
					if idx, found := idxOf[hash]; found {
						items[idx]["status"] = "acceleratable"
					}
				}
			}
		}
	}

	// Tier 5: the user's TorBox cache, checked directly.
	if tb := uncached(); len(tb) > 0 && byok {
		if k, _ := h.Users.GetTBKey(r.Context(), user.ID); k != "" {
			for hash := range providers.TorBoxCached(r.Context(), k, tb) {
				if idx, found := idxOf[hash]; found {
					items[idx]["status"] = "acceleratable"
				}
			}
		}
	}

	stJSON(w, 200, map[string]any{"data": map[string]any{"items": items}})
}
