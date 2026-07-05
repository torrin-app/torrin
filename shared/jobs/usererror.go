package jobs

import "strings"

func UserError(msg string) string {
	l := strings.ToLower(msg)
	switch {
	case strings.Contains(l, "content blocked") || strings.Contains(l, "safety policy"):
		return "blocked by content policy"
	case strings.Contains(l, "exceeds your plan") || strings.Contains(l, "plan limit") || strings.Contains(l, "too large"):
		return msg
	case strings.Contains(l, "season") && strings.Contains(l, "incomplete"):
		return "season incomplete on usenet — some episodes aren't available"
	case strings.Contains(l, "segment") || strings.Contains(l, "no video files") ||
		strings.Contains(l, "par2") || strings.Contains(l, "postproc") ||
		strings.Contains(l, "recovery block") || strings.Contains(l, "no usable links"):
		return "download incomplete — couldn't fully fetch or repair this release"
	case strings.Contains(l, "usenet provider") || strings.Contains(l, "usenet indexer") ||
		strings.Contains(l, "no matching nzb"):
		return "not available on usenet"
	case strings.Contains(l, "dial tcp") || strings.Contains(l, "http://") ||
		strings.Contains(l, "https://") || strings.Contains(l, "jsonrpc") ||
		strings.Contains(l, "connection refused") || strings.Contains(l, "connect usenet"):
		return "download failed — please retry"
	case strings.Contains(l, "alldebrid") || strings.Contains(l, "link_") ||
		strings.Contains(l, "host_unavailable"):
		return "source temporarily unavailable — please retry"
	}
	return msg
}
