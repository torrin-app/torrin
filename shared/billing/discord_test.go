package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDonationMessage(t *testing.T) {
	cases := []struct {
		amount, currency, want string
	}{
		{"10.00", "USD", "$10.00"},
		{"75000.00", "NGN", "75000.00 NGN"},
		{"5.00", "", "5.00"},
		{"", "USD", "a contribution"},
	}
	for _, c := range cases {
		got := donationMessage(c.amount, c.currency)
		if !strings.Contains(got, c.want) {
			t.Errorf("donationMessage(%q,%q) = %q, want it to contain %q", c.amount, c.currency, got, c.want)
		}
	}
}

func TestDiscordPost(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	NewDiscord(srv.URL).Post(context.Background(), "hello")
	if got["content"] != "hello" {
		t.Errorf("posted content = %q, want hello", got["content"])
	}
}

func TestDiscordNilIsNoop(t *testing.T) {
	if NewDiscord("") != nil {
		t.Fatal("empty webhook should yield nil")
	}
	NewDiscord("").Post(context.Background(), "x")
}
