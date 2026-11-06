package handlers

import (
	"testing"

	"github.com/torrin-app/torrin/shared/auth"
)

func TestStoredProviderPassword(t *testing.T) {
	stored := []*auth.UsenetProvider{
		{Host: "news.newshosting.com", Username: "sam", Password: "nhpass"},
		{Host: "news.tweaknews.eu", Username: "sam", Password: "twpass"},
	}
	if got := storedProviderPassword(stored, "news.tweaknews.eu", "sam"); got != "twpass" {
		t.Errorf("match by host+username = %q, want twpass", got)
	}
	if got := storedProviderPassword(stored, "news.newshosting.com", "sam"); got != "nhpass" {
		t.Errorf("match = %q, want nhpass", got)
	}
	if got := storedProviderPassword(stored, "news.newshosting.com", "other"); got != "" {
		t.Errorf("username mismatch should not match, got %q", got)
	}
	if got := storedProviderPassword(nil, "x", "y"); got != "" {
		t.Errorf("empty stored = %q, want empty", got)
	}
}
