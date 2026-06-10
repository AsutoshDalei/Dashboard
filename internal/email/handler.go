package email

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
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
	if err := h.tmpl.ExecuteTemplate(w, "email.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req EmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.Name == "" || req.Company == "" || req.Email == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Missing required fields", "")
		return
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid email address", "")
		return
	}

	if req.SenderKey == "" {
		req.SenderKey = "university"
	}

	provider := NewGmailProvider("")
	senderLabel, err := h.svc.Send(req.Email, req.Name, req.Company, strings.ToLower(strings.TrimSpace(req.SenderKey)), provider)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSON(w, http.StatusOK, true, "", "Email sent via "+senderLabel)
}