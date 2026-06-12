package coverletter

import (
	"encoding/json"
	"fmt"
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
	if err := h.tmpl.ExecuteTemplate(w, "coverletter.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Company string `json:"company"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.Company == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Company name required", "")
		return
	}

	companyName := strings.TrimSpace(req.Company)

	pdfData, err := h.svc.GeneratePDF(companyName)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	safeName := strings.TrimSpace(companyName)
	safeName = strings.ReplaceAll(safeName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	safeName = strings.Trim(safeName, "._-")
	if safeName == "" {
		safeName = "Company"
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ASUTOSH_DALEI_COVERLETTER_%s.pdf\"", safeName))

	if _, err := w.Write(pdfData); err != nil {
		slog.Error("write pdf", "err", err)
	}
}
