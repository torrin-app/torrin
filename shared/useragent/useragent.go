package useragent

import (
	"cmp"
	"os"
	"strings"
)

const Default = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"

var Indexer = cmp.Or(strings.TrimSpace(os.Getenv("INDEXER_USER_AGENT")), Default)
