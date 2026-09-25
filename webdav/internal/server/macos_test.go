package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsMacMeta(t *testing.T) {
	cases := map[string]bool{
		"/.Trashes/501/foo":                       true,
		"/folder/._Episode.mkv":                   true,
		"/folder/.DS_Store":                       true,
		"/.Spotlight-V100":                        true,
		"/.fseventsd/000":                         true,
		"/.TemporaryItems/foo":                    true,
		"/Agent.Kim.Reactivated.S01E03/video.mkv": false,
		"/A.Shop.for.Killers.S02E04/sample.mp4":   false,
		"/":                                       false,
		"/Some Release Folder/":                   false,
	}
	for path, want := range cases {
		if got := isMacMeta(path); got != want {
			t.Errorf("isMacMeta(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestMacNoop(t *testing.T) {
	cases := map[string]int{
		"MKCOL":           http.StatusCreated,
		http.MethodPut:    http.StatusCreated,
		"MOVE":            http.StatusCreated,
		http.MethodDelete: http.StatusNoContent,
		"PROPPATCH":       http.StatusMultiStatus,
	}
	for method, want := range cases {
		w := httptest.NewRecorder()
		macNoop(w, method)
		if w.Code != want {
			t.Errorf("macNoop(%s) = %d, want %d", method, w.Code, want)
		}
	}
}
