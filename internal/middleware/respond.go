package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

func RespondJSON(w http.ResponseWriter, status int, success bool, errMessage, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]any{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func RespondJSONAPI(w http.ResponseWriter, r *http.Request, status int, success bool, clientErr, message string, logErr error) {
	if logErr != nil {
		slog.Error("api", "err", logErr, "request_id", GetRequestID(r), "path", r.URL.Path, "status", status)
	}
	msg := clientErr
	if !success && logErr != nil && msg == "" {
		msg = "Something went wrong. Try again or check server logs."
	}
	RespondJSONWithRequestID(w, status, success, msg, message, GetRequestID(r))
}

func RespondJSONWithRequestID(w http.ResponseWriter, status int, success bool, errMessage, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]any{
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

func RespondJSONWithData(w http.ResponseWriter, status int, success bool, errMessage, message string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]any{
		"success": success,
	}
	if errMessage != "" {
		resp["error"] = errMessage
	}
	if message != "" {
		resp["message"] = message
	}
	if data != nil {
		resp["data"] = data
	}
	_ = json.NewEncoder(w).Encode(resp)
}