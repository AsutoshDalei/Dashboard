package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	authRateMu     sync.Mutex
	authRateByHost = map[string][]time.Time{}
)

const authRateWindow = time.Minute
const authRateMax = 5

func clientIP(r *http.Request) string {
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		if i := strings.Index(xf, ","); i >= 0 {
			xf = strings.TrimSpace(xf[:i])
		}
		if host := strings.TrimSpace(xf); host != "" {
			return host
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func allowAuthAttempt(r *http.Request) bool {
	ip := clientIP(r)
	now := time.Now()
	cutoff := now.Add(-authRateWindow)

	authRateMu.Lock()
	defer authRateMu.Unlock()

	times := authRateByHost[ip]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= authRateMax {
		authRateByHost[ip] = kept
		return false
	}
	kept = append(kept, now)
	authRateByHost[ip] = kept
	return true
}
