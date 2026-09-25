package server

import (
	"net/http"
	"strings"
)

var macMetaMethods = map[string]bool{
	http.MethodPut:    true,
	http.MethodDelete: true,
	"MKCOL":           true,
	"MOVE":            true,
	"COPY":            true,
	"PROPPATCH":       true,
}

func isMacMeta(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func macNoop(w http.ResponseWriter, method string) {
	switch method {
	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	case "PROPPATCH":
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:"/>`))
	default:
		w.WriteHeader(http.StatusCreated)
	}
}
