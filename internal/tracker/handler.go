package tracker

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pi_dashboard/internal/middleware"
)

type Handler struct {
	svc  *Service
	tmpl *template.Template
}

func NewHandler(svc *Service, tmpl *template.Template) *Handler {
	return &Handler{svc: svc, tmpl: tmpl}
}

func (h *Handler) HandleTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "tracker.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) HandleUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
		return
	}

	var app Application
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid JSON"})
		return
	}

	result, err := h.svc.Upsert(r.Context(), app)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":        true,
		"action":         result.Action,
		"organization":   result.Organization,
		"previous_count": result.PreviousCount,
		"added":          result.Added,
		"new_count":      result.NewCount,
	})
}

func (h *Handler) HandleCheck(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("company")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "company required"})
		return
	}

	organization, exists, count, status, appliedDates, err := h.svc.Check(r.Context(), name)
	if err != nil || !exists {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "exists": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"exists":        true,
		"organization":  organization,
		"count":         count,
		"status":        status,
		"applied_dates": appliedDates,
	})
}

func (h *Handler) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "query required"})
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	results, err := h.svc.Suggest(r.Context(), query, limit)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"suggestions": results,
	})
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.Stats(r.Context())
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"stats":   stats,
	})
}

func (h *Handler) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	days := 0
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}

	freq := r.URL.Query().Get("freq")

	entries, err := h.svc.Timeline(r.Context(), days, freq)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"buckets": entries,
	})
}

func (h *Handler) HandleContribution(w http.ResponseWriter, r *http.Request) {
	year := time.Now().Year()
	if y := r.URL.Query().Get("year"); y != "" {
		if n, err := strconv.Atoi(y); err == nil {
			year = n
		}
	}

	month := 0
	if m := r.URL.Query().Get("month"); m != "" {
		if n, err := strconv.Atoi(m); err == nil && n > 0 && n <= 12 {
			month = n
		}
	}

	days, err := h.svc.ContributionHeatmap(r.Context(), year, month)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"days":    days,
	})
}

func (h *Handler) HandleContributionRange(w http.ResponseWriter, r *http.Request) {
	firstMonth, lastMonth, err := h.svc.DateRange(r.Context())
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"first_month": firstMonth,
		"last_month":  lastMonth,
	})
}

func (h *Handler) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "error": "Method not allowed"})
		return
	}

	var req struct {
		Mode  string `json:"mode"`
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid JSON"})
		return
	}

	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Query = strings.TrimSpace(req.Query)

	if req.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "query required"})
		return
	}
	if req.Mode == "" {
		req.Mode = "nl"
	}

	var sqlText string
	if req.Mode == "nl" {
		var err error
		sqlText, err = h.svc.NaturalLanguageQuery(r.Context(), req.Query)
		if err != nil {
			middleware.RespondJSONAPI(w, r, http.StatusBadGateway, false, "", "", err)
			return
		}
	} else {
		sqlText = extractSQL(req.Query)
	}

	if err := ValidateReadOnlySQL(sqlText); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	result, err := h.svc.ExecuteQuery(r.Context(), sqlText)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusBadRequest, false, "", "", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"sql":       result.SQL,
		"columns":   result.Columns,
		"rows":      result.Rows,
		"row_count": result.RowCount,
		"truncated": result.Truncated,
	})
}
