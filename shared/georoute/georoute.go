package georoute

import (
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type relay struct {
	host string
	lat  float64
	lng  float64
}

var (
	relays     = parseRelays()
	relayHosts = relayHostSet()
)

func parseRelays() []relay {
	raw := os.Getenv("TORRIN_STREAM_RELAYS")
	if raw == "" {
		return defaultRelays
	}
	var out []relay
	for _, part := range strings.Split(raw, ",") {
		bits := strings.Split(strings.TrimSpace(part), ":")
		if len(bits) != 3 {
			continue
		}
		lat, err1 := strconv.ParseFloat(bits[1], 64)
		lng, err2 := strconv.ParseFloat(bits[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, relay{host: bits[0], lat: lat, lng: lng})
	}
	return out
}

func relayHostSet() map[string]bool {
	m := make(map[string]bool, len(relays))
	for _, r := range relays {
		m[r.host] = true
	}
	return m
}

func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	p := math.Pi / 180
	a := 0.5 - math.Cos((lat2-lat1)*p)/2 + math.Cos(lat1*p)*math.Cos(lat2*p)*(1-math.Cos((lng2-lng1)*p))/2
	return 12742 * math.Asin(math.Sqrt(a))
}

func nearestRelay(lat, lng float64) string {
	host := ""
	best := math.MaxFloat64
	for _, r := range relays {
		if d := haversineKm(lat, lng, r.lat, r.lng); d < best {
			best, host = d, r.host
		}
	}
	return host
}

func pickHost(r *http.Request) string {
	if len(relays) == 0 {
		return ""
	}
	lat, err1 := strconv.ParseFloat(r.Header.Get("CF-IPLatitude"), 64)
	lng, err2 := strconv.ParseFloat(r.Header.Get("CF-IPLongitude"), 64)
	if err1 != nil || err2 != nil {
		return ""
	}
	return nearestRelay(lat, lng)
}

func URL(r *http.Request, raw string) string {
	if raw == "" {
		return raw
	}
	host := pickHost(r)
	if host == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if relayHosts[u.Host] && u.Host != host {
		u.Host = host
		return u.String()
	}
	return raw
}
