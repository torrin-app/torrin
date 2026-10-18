# Readiness check diagnostics

Use the existing authenticated `GET /v0/store/magnets/check` endpoint with
`diagnostics=1` to explain a result that remains `unknown`. The `/torz/check`
alias accepts the same option. Existing `status`, file and authorization fields
remain compatible, and the option makes no additional provider requests.

Each item adds this optional object:

```json
{
  "diagnostics": {
    "version": 1,
    "budget_exhausted": false,
    "checks": [
      {"tier": "warm_cache", "outcome": "error", "reason": "database_error"},
      {"tier": "system_alldebrid", "outcome": "error", "reason": "rate_limited"},
      {"tier": "personal_premiumize", "outcome": "skipped", "reason": "not_configured"}
    ]
  }
}
```

The example abbreviates the fixed list of twelve tiers. The response records
local warm-cache lookup and storage verification, cross-node cache, durable
archive, release links, the shared library, system AllDebrid/Real-Debrid and
personal AllDebrid/Real-Debrid/Premiumize/TorBox/Offcloud. It exposes no credential,
provider response body, storage address or raw error. `not_run` means the tier
was not visited; it does not claim the tier is disabled or that its check passed.
Earlier positive results may stop later checks, as before.

| Outcome | Meaning |
| --- | --- |
| `hit` | The tier returned existing cached-file evidence. Episode/file selection still applies. |
| `candidate` | Preparation potential or a release pointer; not an existing playable file. |
| `miss` | This tier made a specific negative observation, such as an explicit provider `false`, an absent database row or a missing selected file. |
| `skipped` | Not configured, outside the plan, not applicable or not visited. |
| `unknown` | Partial/positive-only response, or a legacy repository cannot distinguish lookup failure from absence. |
| `error` | Timeout, cancellation, authentication, rate limit, network, HTTP, malformed response, database or storage failure. |

A `miss` covers only its named tier and requested hash/episode. It never means
that a title is globally unavailable. `budget_exhausted` records the request's
20-second deadline separately from a timeout inside an individual operation.
The diagnostic object intentionally has no global-negative or completeness flag.

## Cache correction

Previously, a partial map was read with `map[hash]` without testing membership.
An omitted hash therefore became `false` and was memoized. Only explicitly known
observations are now stored. Missing values stay unresolved and can be retried;
valid independent positive/negative entries remain usable. A failed provider
success flag, truncated response or failed body read cannot supply cache misses.

The corrected path uses a versioned cache-key namespace. Existing entries are
left in place to expire, but old fabricated negatives are not read as confirmed
observations. Database entries retain their original expiry when loaded into
memory. No schema migration, credential change or cache purge is needed.

## Deployment

This patch must run on the Torrin API service to explain its internal failures.
Adding a parser in a downstream application cannot reconstruct information an
older Torrin service omitted. Clients must tolerate the diagnostic object being
absent and report that as diagnostics unavailable; they must not infer a miss.

Apply the patch, run the repository build/vet/tests, and deploy through the
existing Torrin release process. Verify one bounded authenticated hash batch
with the query option, then inspect fixed reason counts before restarting a
catalogue-wide recheck. Do not initiate downloads to obtain diagnostic reasons.
