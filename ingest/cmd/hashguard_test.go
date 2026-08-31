package main

import "testing"

func TestHoldHashSingleFlight(t *testing.T) {
	h := "c7317c445450a6505965c67ab5052050b9ad5d10"

	if !holdHash(h) {
		t.Fatal("first hold should succeed")
	}
	if holdHash(h) {
		t.Fatal("second hold on the same hash must fail (a job is already processing it)")
	}

	releaseHash(h)
	if !holdHash(h) {
		t.Fatal("hold should succeed again after release")
	}
	releaseHash(h)
}

func TestHoldHashEmptyAlwaysAllowed(t *testing.T) {
	if !holdHash("") {
		t.Fatal("empty info_hash must never be gated")
	}
	if !holdHash("") {
		t.Fatal("empty info_hash must never be gated on repeat")
	}
}
