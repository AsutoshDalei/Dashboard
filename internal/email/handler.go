package email

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"pi_dashboard/internal/middleware"
)

type Handler struct {
	svc    *Service
	tmpl   *template.Template
	config *GmailConfig
	repo   *Repository
}

type GmailConfig struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	ClientSecret string
	TokenURI     string
	Expiry       string
}

func NewHandler(svc *Service, tmpl *template.Template, gmailConfig *GmailConfig, repo *Repository) *Handler {
	return &Handler{svc: svc, tmpl: tmpl, config: gmailConfig, repo: repo}
}

func (h *Handler) HandleTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "email.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	manifest, err := loadTemplateManifest()
	if err != nil {
		middleware.RespondJSON(w, http.StatusInternalServerError, false, "Failed to load templates", "")
		return
	}

	var templates []EmailTemplate
	for key, meta := range manifest.Templates {
		templates = append(templates, EmailTemplate{
			Key:      key,
			Name:     meta.Name,
			Subject:  meta.Subject,
			UsesRole: meta.UsesRole,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"templates": templates})
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
	req.Email = strings.ToLower(req.Email)

	if req.SenderKey == "" {
		req.SenderKey = "university"
	}
	if req.TemplateKey == "" {
		req.TemplateKey = "referral"
	}

	provider := NewGmailProvider(
		h.config.AccessToken,
		h.config.RefreshToken,
		h.config.ClientID,
		h.config.ClientSecret,
		h.config.TokenURI,
		h.config.Expiry,
	)
	senderLabel, err := h.svc.Send(req.Email, req.Name, req.Company, strings.ToLower(strings.TrimSpace(req.SenderKey)), req.TemplateKey, req.Role, provider)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	_ = h.repo.RecordSend(r.Context(), req.Name, req.Email, req.TemplateKey)

	middleware.RespondJSON(w, http.StatusOK, true, "", "Email sent via "+senderLabel)
}

func (h *Handler) HandleCheckEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}
	emailAddr := strings.ToLower(r.URL.Query().Get("email"))
	if emailAddr == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Missing email parameter", "")
		return
	}
	rec, err := h.repo.FindByEmail(r.Context(), emailAddr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(EmailCheckResponse{Exists: false})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(EmailCheckResponse{
		Exists:   true,
		Name:     rec.Name,
		Template: rec.Template,
		SentAt:   rec.SentAt.Format("Jan 02, 2006 3:04 PM"),
	})
}

func (h *Handler) HandleSendAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req EmailAPIRequest
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
	req.Email = strings.ToLower(req.Email)

	if req.SenderKey == "" {
		req.SenderKey = "university"
	}
	if req.TemplateKey == "" {
		req.TemplateKey = "referral"
	}
	if req.Safety == "" {
		req.Safety = "safe"
	}

	existing, _ := h.repo.FindByEmail(r.Context(), req.Email)
	if existing != nil && req.Safety == "safe" {
		middleware.RespondJSON(w, http.StatusConflict, false,
			fmt.Sprintf("Email already sent to %s on %s via %s",
				req.Email, existing.SentAt.Format("Jan 02, 2006"), existing.Template), "")
		return
	}

	provider := NewGmailProvider(
		h.config.AccessToken, h.config.RefreshToken,
		h.config.ClientID, h.config.ClientSecret,
		h.config.TokenURI, h.config.Expiry,
	)
	senderLabel, err := h.svc.Send(req.Email, req.Name, req.Company,
		strings.ToLower(strings.TrimSpace(req.SenderKey)), req.TemplateKey, req.Role, provider)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	_ = h.repo.RecordSend(r.Context(), req.Name, req.Email, req.TemplateKey)

	middleware.RespondJSON(w, http.StatusOK, true, "", "Email sent via "+senderLabel)
}