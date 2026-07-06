package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchHosts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"status":"success","data":{"hosts":{
			"rapidgator":{"name":"rapidgator","type":"premium","domains":["rapidgator.net"],"status":true},
			"deadhost":{"name":"deadhost","type":"free","domains":["dead.example"],"status":false}}}}`))
	}))
	defer srv.Close()

	old := adBase
	adBase = srv.URL
	defer func() { adBase = old }()

	hosts, err := FetchHosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2", len(hosts))
	}
	// sorted by name: deadhost first, then rapidgator.
	if hosts[0].Name != "deadhost" || hosts[0].Up == nil || *hosts[0].Up {
		t.Errorf("deadhost = %+v, want down", hosts[0])
	}
	if hosts[1].Name != "rapidgator" || hosts[1].Up == nil || !*hosts[1].Up {
		t.Errorf("rapidgator = %+v, want up", hosts[1])
	}
}
