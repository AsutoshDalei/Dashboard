package workspace

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"

	"pi_dashboard/internal/middleware"
)

type Handler struct {
	svc  *Service
	tmpl *template.Template
}

func NewHandler(svc *Service, tmpl *template.Template) *Handler {
	return &Handler{svc: svc, tmpl: tmpl}
}

func (h *Handler) HandleChatTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "chat.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Message      string `json:"message"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	sessionID := getSessionID(r)
	if sessionID == "" {
		middleware.RespondJSON(w, http.StatusUnauthorized, false, "Not authenticated", "")
		return
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "You are a helpful assistant."
	}

	resp, err := h.svc.Chat(r.Context(), sessionID, req.Message, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"response": resp})
}

func (h *Handler) HandleChatClear(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(r)
	h.svc.ClearChat(sessionID)
	middleware.RespondJSON(w, http.StatusOK, true, "", "Chat cleared")
}

func (h *Handler) HandleResumeTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "resume.html", nil); err != nil {
		slog.Error("template", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) HandleResumeAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobDescription string `json:"job_description"`
		SystemPrompt   string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.JobDescription == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Job description required", "")
		return
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "Analyze this resume against the job description. Provide score, keywords, analysis, and recommendations."
	}

	result, err := h.svc.AnalyzeResume(r.Context(), req.JobDescription, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"result": result})
}

func (h *Handler) HandleResumeGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobDescription string `json:"job_description"`
		Analysis       string `json:"analysis"`
		SystemPrompt   string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	systemPrompt := req.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = "Generate a tailored resume based on the analysis."
	}

	result, err := h.svc.GenerateResume(r.Context(), req.JobDescription, req.Analysis, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"result": result})
}

func (h *Handler) HandleJobMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobDescription string `json:"job_description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	result, err := h.svc.AnalyzeJobMatch(r.Context(), req.JobDescription)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"result": result})
}

func getSessionID(r *http.Request) string {
	cookie, err := r.Cookie("session_token")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *Handler) HandleDraftEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Name    string `json:"name"`
		Company string `json:"company"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	result, err := h.svc.DraftEmail(r.Context(), req.Name, req.Company)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"draft": result})
}

func (h *Handler) HandleDraftCoverLetter(w http.ResponseWriter, r *http.Request) {
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

	result, err := h.svc.DraftCoverLetter(r.Context(), req.Company)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"draft": result})
}

func (h *Handler) HandleSQLAssistant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Query      string `json:"query"`
		SchemaDoc  string `json:"schema_doc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	schemaDoc := req.SchemaDoc
	if schemaDoc == "" {
		schemaDoc = "You are a PostgreSQL query generator. Return ONLY the SQL statement."
	}

	result, err := h.svc.GenerateSQL(r.Context(), req.Query, schemaDoc)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"sql": result})
}