package clipboard

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

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
	if err := h.tmpl.ExecuteTemplate(w, "clipboard.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r)
	case http.MethodPost:
		h.handleCreate(w, r)
	default:
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
	}
}

func (h *Handler) HandleReorderAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid request", "")
		return
	}
	if err := h.svc.Reorder(r.Context(), req.IDs); err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}
	middleware.RespondJSON(w, http.StatusOK, true, "", "Reordered")
}

func (h *Handler) HandleItemAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clipboard/")
	parts := strings.Split(path, "/")
	id := parts[0]

	if id == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Item ID required", "")
		return
	}

	if r.Method == http.MethodDelete {
		h.handleDelete(w, r, id)
		return
	}

	middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	var items []Item
	var err error

	if query != "" {
		items, err = h.svc.Search(r.Context(), query)
	} else {
		items, err = h.svc.List(r.Context())
	}

	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	if items == nil {
		items = []Item{}
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", items)
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label    string `json:"label"`
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.Content == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Content required", "")
		return
	}

	item, err := h.svc.Create(r.Context(), strings.TrimSpace(req.Label), req.Content, req.Category)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusCreated, true, "", "", item)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.svc.Delete(r.Context(), id); err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusNotFound, false, "Item not found", "", err)
		return
	}

	middleware.RespondJSON(w, http.StatusOK, true, "", "Item deleted")
}