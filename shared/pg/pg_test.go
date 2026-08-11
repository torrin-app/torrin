package pg

import "testing"

func TestPoolConfigCapsHighRequest(t *testing.T) {
	cfg, err := poolConfig("postgres://torrin:torrin@localhost:5432/torrin?pool_max_conns=90")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != maxPoolConns {
		t.Fatalf("high request not capped: MaxConns=%d, want %d", cfg.MaxConns, maxPoolConns)
	}
}

func TestPoolConfigKeepsLowRequest(t *testing.T) {
	cfg, err := poolConfig("postgres://torrin:torrin@localhost:5432/torrin?pool_max_conns=3")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxConns != 3 {
		t.Fatalf("low request should be untouched: MaxConns=%d, want 3", cfg.MaxConns)
	}
}
