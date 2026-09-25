package useragent

import (
	"cmp"
	"os"
	"strconv"
	"strings"
)

const Default = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"

const (
	IndexerSearchDefault = "Prowlarr/1.30.2"
	IndexerGrabDefault   = "SABnzbd/5.0.4"
	SonarrDefault        = "Sonarr/4.0.15.2941"
	RadarrDefault        = "Radarr/5.26.2.10099"
)

var (
	Indexer     = cmp.Or(strings.TrimSpace(os.Getenv("INDEXER_USER_AGENT")), IndexerSearchDefault)
	IndexerGrab = cmp.Or(strings.TrimSpace(os.Getenv("INDEXER_GRAB_USER_AGENT")), IndexerGrabDefault)
	Sonarr      = cmp.Or(strings.TrimSpace(os.Getenv("SONARR_USER_AGENT")), SonarrDefault)
	Radarr      = cmp.Or(strings.TrimSpace(os.Getenv("RADARR_USER_AGENT")), RadarrDefault)
)

func GrabUAForCategory(category string) string {
	switch n := firstInt(category); {
	case n >= 5000 && n < 6000:
		return Sonarr
	case n >= 2000 && n < 3000:
		return Radarr
	default:
		return IndexerGrab
	}
}

func firstInt(s string) int {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			n, _ := strconv.Atoi(s[start:i])
			return n
		}
	}
	if start >= 0 {
		n, _ := strconv.Atoi(s[start:])
		return n
	}
	return 0
}
