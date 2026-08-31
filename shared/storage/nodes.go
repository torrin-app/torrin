package storage

import "strings"

func ParseNodes(spec, region, access, secret, bucket, publicURL, signingKey string) map[string]*Client {
	out := map[string]*Client{}
	for _, part := range strings.Split(spec, ",") {
		node, endpoint, ok := strings.Cut(strings.TrimSpace(part), "=")
		if endpoint = strings.TrimSpace(endpoint); !ok || endpoint == "" {
			continue
		}
		out[strings.TrimSpace(node)] = NewClient(endpoint, region, access, secret, bucket, publicURL, signingKey)
	}
	return out
}
