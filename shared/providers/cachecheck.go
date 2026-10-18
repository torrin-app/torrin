package providers

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/torrin-app/torrin/shared/availcache"
	"github.com/torrin-app/torrin/shared/breaker"
)

const maxCacheCheckBody = 1 << 20

// breaker reads the response body, so bound that read at the transport boundary
// and retain read errors that its legacy convenience wrapper does not expose.
type checkBody struct {
	io.Reader
	io.Closer
	err error
}

func (b *checkBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err != nil && err != io.EOF {
		b.err = err
	}
	return n, err
}

type checkTransport struct {
	base http.RoundTripper
	body *checkBody
}

func (t *checkTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil {
		t.body = &checkBody{Reader: io.LimitReader(resp.Body, maxCacheCheckBody+1), Closer: resp.Body}
		resp.Body = t.body
	}
	return resp, err
}
func cacheCheckRequest(ctx context.Context, client *http.Client, provider string, req *http.Request) ([]byte, string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	transport := &checkTransport{base: base}
	bounded := *client
	bounded.Transport = transport
	status, body, err := breaker.RoundTrip(provider, &bounded, req)
	if err != nil {
		return nil, availcache.ErrorReason(err)
	}
	if reason := availcache.HTTPReason(status); reason != "" {
		return nil, reason
	}
	if transport.body != nil && transport.body.err != nil {
		reason := availcache.ErrorReason(transport.body.err)
		if reason == "check_failed" {
			reason = "network_error"
		}
		return nil, reason
	}
	if len(body) > maxCacheCheckBody {
		return nil, "invalid_response"
	}
	return body, ""
}

func cachedCheckDetailed(ctx context.Context, provider, key string, hashes []string, query func(context.Context, []string) availcache.CheckResult) availcache.CheckResult {
	hashes = availcache.CanonicalHashes(hashes)
	if key == "" {
		return availcache.Failure(hashes, "not_configured")
	}
	if len(hashes) > 500 {
		return availcache.Failure(hashes, "invalid_response")
	}
	for _, hash := range hashes {
		if len(hash) > 128 {
			return availcache.Failure(hashes, "invalid_response")
		}
	}
	if availCache == nil {
		return query(ctx, hashes)
	}
	return availCache.CheckCachedDetailed(ctx, availcache.Prefix(provider, key), hashes, query)
}
func legacyCached(result availcache.CheckResult) map[string]bool {
	if len(result.Cached) == 0 {
		return nil
	}
	return result.Cached
}
