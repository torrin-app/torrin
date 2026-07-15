package post

import (
	"crypto/rand"
	"encoding/hex"
)

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

var fromDomains = []string{"gmail.com", "outlook.com", "yahoo.com", "gmx.com", "proton.me"}

func randMessageID() string { return "<" + randHex(16) + "@torrin>" }
func randSubject() string   { return randHex(12) }
func randName() string      { return randHex(10) }

func randFrom() string {
	b := make([]byte, 1)
	rand.Read(b)
	return randHex(8) + "@" + fromDomains[int(b[0])%len(fromDomains)]
}
