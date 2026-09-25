package handlers

import "testing"

func TestValidIMDB(t *testing.T) {
	ok := []string{"tt0388629", "tt12345", "tt123456789"}
	for _, s := range ok {
		if !validIMDB(s) {
			t.Errorf("%s should be valid", s)
		}
	}
	bad := []string{"", "tt", "tt123", "0388629", "tt12345678901", "ttabcde", "tt 12345", "TT12345", "tt12a45"}
	for _, s := range bad {
		if validIMDB(s) {
			t.Errorf("%s should be invalid", s)
		}
	}
}
