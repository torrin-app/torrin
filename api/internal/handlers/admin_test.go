package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		512:        "512 B",
		1024:       "1.0 KB",
		1536:       "1.5 KB",
		1073741824: "1.0 GB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestAdminAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	call := func(adminKey, header string) int {
		s := &Server{Deps{AdminKey: adminKey}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/stats", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		s.adminAuth(ok).ServeHTTP(rec, req)
		return rec.Code
	}

	if got := call("", "Bearer x"); got != 403 {
		t.Errorf("unconfigured: got %d, want 403", got)
	}
	if got := call("secret", "Bearer wrong"); got != 401 {
		t.Errorf("wrong key: got %d, want 401", got)
	}
	if got := call("secret", ""); got != 401 {
		t.Errorf("no header: got %d, want 401", got)
	}
	if got := call("secret", "Bearer secret"); got != 200 {
		t.Errorf("right key: got %d, want 200", got)
	}
}

func TestAdminWalletRoutesRequireAuth(t *testing.T) {
	s := &Server{Deps{AdminKey: "secret"}}
	mux := http.NewServeMux()
	s.registerAdminRoutes(mux)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/admin/users/u1/wallet"},
		{"POST", "/api/admin/users/u1/wallet"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != 401 {
			t.Errorf("%s %s without key = %d, want 401", c.method, c.path, rec.Code)
		}
	}
}
