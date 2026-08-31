package main

import "sync"

var activeHashes sync.Map

func holdHash(infoHash string) bool {
	if infoHash == "" {
		return true
	}
	_, busy := activeHashes.LoadOrStore(infoHash, true)
	return !busy
}

func releaseHash(infoHash string) {
	if infoHash != "" {
		activeHashes.Delete(infoHash)
	}
}
