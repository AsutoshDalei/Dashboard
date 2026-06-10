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

func (h *Handler) HandleUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var app Application
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	result, err := h.svc.Upsert(r.Context(), app)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", result)
}

func (h *Handler) HandleCheck(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("company")
	if name == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "company required", "")
		return
	}

	exists, count, status, appliedDates, err := h.svc.Check(r.Context(), name)
	if err != nil || !exists {
		middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
			"exists": false,
		})
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
		"exists":        true,
		"organization":  name,
		"count":         count,
		"status":        status,
		"applied_dates": appliedDates,
	})
}

func (h *Handler) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "query required", "")
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

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
		"suggestions": results,
	})
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.svc.Stats(r.Context())
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
		"stats": stats,
	})
}

func (h *Handler) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			days = n
		}
	}

	freq := r.URL.Query().Get("freq")
	switch freq {
	case "day":
		days = 14
	case "week":
		days = 90
	case "month":
		days = 365
	default:
		days = 365
	}

	entries, err := h.svc.Timeline(r.Context(), days)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
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

	days, err := h.svc.ContributionHeatmap(r.Context(), year)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{
		"days": days,
	})
}

func (h *Handler) HandleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Mode  string `json:"mode"`
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Query = strings.TrimSpace(req.Query)

	if req.Query == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "query required", "")
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
		middleware.RespondJSON(w, http.StatusBadRequest, false, err.Error(), "")
		return
	}

	result, err := h.svc.ExecuteQuery(r.Context(), sqlText)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusBadRequest, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", result)
}