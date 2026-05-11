package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type ctxKey int

const reqIDCtxKey ctxKey = 1

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" {
			var b [16]byte
			if _, err := rand.Read(b[:]); err != nil {
				id = "req-unknown"
			} else {
				id = hex.EncodeToString(b[:])
			}
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), reqIDCtxKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func requestIDFrom(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := r.Context().Value(reqIDCtxKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// respondJSONAPI logs logErr (if non-nil) and returns clientErr to the client, with request_id on failure.
func respondJSONAPI(w http.ResponseWriter, r *http.Request, status int, success bool, clientErr, message string, logErr error) {
	if logErr != nil {
		slog.Error("api", "err", logErr, "request_id", requestIDFrom(r), "path", r.URL.Path, "status", status)
	}
	msg := clientErr
	if !success && logErr != nil && msg == "" {
		msg = "Something went wrong. Try again or check server logs."
	}
	respondJSONWithRequestID(w, status, success, msg, message, requestIDFrom(r))
}

func respondJSONWithRequestID(w http.ResponseWriter, status int, success bool, errMessage, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]interface{}{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	if requestID != "" && !success {
		resp["request_id"] = requestID
	}
	_ = json.NewEncoder(w).Encode(resp)
}
