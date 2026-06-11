package workspace

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"pi_dashboard/internal/llm"
	"pi_dashboard/internal/middleware"
)

type Handler struct {
	svc     *Service
	tmpl    *template.Template
	prompts *llm.Prompts
}

func NewHandler(svc *Service, tmpl *template.Template, prompts *llm.Prompts) *Handler {
	return &Handler{svc: svc, tmpl: tmpl, prompts: prompts}
}

type resumeAnalysisResponse struct {
	Score           float64  `json:"score"`
	Keywords        []string `json:"keywords"`
	Analysis        string   `json:"analysis"`
	Recommendations string   `json:"recommendations"`
	Archetype       string   `json:"archetype"`
}

func parseResumeAnalysis(raw string) resumeAnalysisResponse {
	out := resumeAnalysisResponse{Analysis: strings.TrimSpace(raw)}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return out
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		trimmed = strings.TrimSpace(trimmed[start : end+1])
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err == nil {
		if out.Score > 5 && out.Score <= 100 {
			out.Score = out.Score / 20
		}
		if out.Score < 0 {
			out.Score = 0
		}
		if out.Score > 5 {
			out.Score = 5
		}
		return out
	}
	return out
}

func (h *Handler) resumePDFBytes() ([]byte, error) {
	filename := strings.TrimSpace(os.Getenv("RESUME_FILENAME"))
	if filename == "" {
		filename = "ASUTOSH_DALEI_RESUME.pdf"
	}

	candidates := []string{}
	if path := strings.TrimSpace(os.Getenv("RESUME_PATH")); path != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, filepath.Join("pi_bundle", filename))
	candidates = append(candidates, filename)
	candidates = append(candidates, filepath.Join("pi_bundle", "ASUTOSH_DALEI_RESUME.pdf"))
	candidates = append(candidates, "ASUTOSH_DALEI_RESUME.pdf")

	seen := map[string]bool{}
	for _, path := range candidates {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
	}
	return nil, fmt.Errorf("resume pdf not found")
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

	stream := r.URL.Query().Get("stream") == "true"

	var req struct {
		Message string `json:"message"`
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

	systemPrompt := h.prompts.Get("chat_system")

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		ch, err := h.svc.ChatStream(r.Context(), sessionID, req.Message, systemPrompt)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
			return
		}

		for chunk := range ch {
			fmt.Fprintf(w, "data: %s\n\n", jsonEscape(chunk))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	resp, err := h.svc.Chat(r.Context(), sessionID, req.Message, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"response": resp})
}

func jsonEscape(s string) string {
	b := strings.Builder{}
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '"':
			b.WriteString("\\\"")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.JobDescription == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Job description required", "")
		return
	}

	systemPrompt := h.prompts.Get("resume_analyze")

	result, err := h.svc.AnalyzeResume(r.Context(), req.JobDescription, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	analysis := parseResumeAnalysis(result)
	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", analysis)
}

func (h *Handler) HandleResumeGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobDescription string `json:"job_description"`
		Analysis       string `json:"analysis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	systemPrompt := h.prompts.Get("resume_generate")

	result, err := h.svc.GenerateResume(r.Context(), req.JobDescription, req.Analysis, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"result": result, "modified_latex": result})
}

func (h *Handler) HandleResumeCompile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		LatexSource string `json:"latex_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}
	if strings.TrimSpace(req.LatexSource) == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "LaTeX source required", "")
		return
	}

	pdfData, err := h.resumePDFBytes()
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	if _, err := w.Write(pdfData); err != nil {
		slog.Error("write resume pdf", "err", err)
	}
}

func (h *Handler) HandleResumeReanalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		LatexSource    string `json:"latex_source"`
		JobDescription string `json:"job_description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}
	if strings.TrimSpace(req.LatexSource) == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "LaTeX source required", "")
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]any{"new_score": 0.0})
}

func (h *Handler) HandleResumeChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		Message        string `json:"message"`
		CurrentLatex   string `json:"current_latex"`
		JobDescription string `json:"job_description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Message required", "")
		return
	}
	if strings.TrimSpace(req.CurrentLatex) == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Current LaTeX required", "")
		return
	}

	sessionID := getSessionID(r)
	if sessionID == "" {
		middleware.RespondJSON(w, http.StatusUnauthorized, false, "Not authenticated", "")
		return
	}

	systemPrompt := h.prompts.Get("resume_chat")
	resp, err := h.svc.Chat(r.Context(), sessionID, req.Message, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{
		"response_text":  resp,
		"modified_latex": req.CurrentLatex,
	})
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
		Query string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	systemPrompt := h.prompts.Get("sql_assistant")

	result, err := h.svc.GenerateSQL(r.Context(), req.Query, systemPrompt)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"sql": result})
}