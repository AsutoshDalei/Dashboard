package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pi_dashboard/internal/job"
	"pi_dashboard/internal/llm"
	"pi_dashboard/internal/middleware"
)

type Handler struct {
	svc     *Service
	tmpl    *template.Template
	prompts *llm.Prompts
	jobs    *job.Manager
}

func NewHandler(svc *Service, tmpl *template.Template, prompts *llm.Prompts) *Handler {
	return &Handler{svc: svc, tmpl: tmpl, prompts: prompts, jobs: job.NewManager()}
}

type resumeAnalysisResponse struct {
	Score           float64  `json:"score"`
	Keywords        []string `json:"keywords"`
	Analysis        string   `json:"analysis"`
	Recommendations string   `json:"recommendations"`
	Archetype       string   `json:"archetype"`
}

func parseResumeAnalysis(raw string) resumeAnalysisResponse {
	var out resumeAnalysisResponse
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return out
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		out.Analysis = trimmed
		return out
	}
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
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send thinking event immediately
		fmt.Fprintf(w, "event: thinking\ndata: {}\n\n")
		flusher.Flush()

		ch, err := h.svc.ChatStream(r.Context(), sessionID, req.Message, systemPrompt)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonEscape(err.Error()))
			flusher.Flush()
			return
		}

		for chunk := range ch {
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonEscape(chunk))
			flusher.Flush()
		}
		fmt.Fprintf(w, "event: done\ndata: {}\n\n")
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

func (h *Handler) HandleChatSendAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobID   string `json:"job_id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.Message == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Message required", "")
		return
	}

	jobID := req.JobID
	if jobID == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "job_id required", "")
		return
	}

	sessionID := getSessionID(r)
	if sessionID == "" {
		middleware.RespondJSON(w, http.StatusUnauthorized, false, "Not authenticated", "")
		return
	}

	h.jobs.Create(jobID, "chat")

	systemPrompt := h.prompts.Get("chat_system")

	go func() {
		h.jobs.Update(jobID, job.StatusRunning, nil, "")
		resp, err := h.svc.Chat(context.Background(), sessionID, req.Message, systemPrompt)
		if err != nil {
			h.jobs.Update(jobID, job.StatusFailed, nil, err.Error())
			return
		}
		h.jobs.Update(jobID, job.StatusCompleted, map[string]string{"response": resp}, "")
	}()

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"job_id": jobID})
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
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		OllamaHost     string `json:"ollama_host"`
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

	params := &ProviderParams{Provider: req.Provider, Model: req.Model, Host: req.OllamaHost}
	result, err := h.svc.AnalyzeResume(r.Context(), req.JobDescription, systemPrompt, params)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	analysis := parseResumeAnalysis(result)
	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", analysis)
}

func (h *Handler) HandleResumeAnalyzeAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobID          string `json:"job_id"`
		JobDescription string `json:"job_description"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		OllamaHost     string `json:"ollama_host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.JobDescription == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Job description required", "")
		return
	}

	jobID := req.JobID
	if jobID == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "job_id required", "")
		return
	}

	h.jobs.Create(jobID, "analyze")

	systemPrompt := h.prompts.Get("resume_analyze")
	params := &ProviderParams{Provider: req.Provider, Model: req.Model, Host: req.OllamaHost}

	go func() {
		h.jobs.Update(jobID, job.StatusRunning, nil, "")
		result, err := h.svc.AnalyzeResume(context.Background(), req.JobDescription, systemPrompt, params)
		if err != nil {
			h.jobs.Update(jobID, job.StatusFailed, nil, err.Error())
			return
		}
		analysis := parseResumeAnalysis(result)
		h.jobs.Update(jobID, job.StatusCompleted, analysis, "")
	}()

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"job_id": jobID})
}

func (h *Handler) HandleResumeGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobDescription string `json:"job_description"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		OllamaHost     string `json:"ollama_host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.JobDescription == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Job description required", "")
		return
	}

	params := &ProviderParams{Provider: req.Provider, Model: req.Model, Host: req.OllamaHost}
	result, err := h.svc.GenerateSkills(r.Context(), req.JobDescription, params)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"modified_latex": result})
}

func (h *Handler) HandleResumeGenerateAsync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
		return
	}

	var req struct {
		JobID          string `json:"job_id"`
		JobDescription string `json:"job_description"`
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		OllamaHost     string `json:"ollama_host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
		return
	}

	if req.JobDescription == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "Job description required", "")
		return
	}

	jobID := req.JobID
	if jobID == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "job_id required", "")
		return
	}

	h.jobs.Create(jobID, "generate")

	params := &ProviderParams{Provider: req.Provider, Model: req.Model, Host: req.OllamaHost}

	go func() {
		h.jobs.Update(jobID, job.StatusRunning, nil, "")
		result, err := h.svc.GenerateSkills(context.Background(), req.JobDescription, params)
		if err != nil {
			h.jobs.Update(jobID, job.StatusFailed, nil, err.Error())
			return
		}
		h.jobs.Update(jobID, job.StatusCompleted, map[string]string{"modified_latex": result}, "")
	}()

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"job_id": jobID})
}

func (h *Handler) HandleJobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		middleware.RespondJSON(w, http.StatusBadRequest, false, "job_id required", "")
		return
	}

	j := h.jobs.Get(jobID)
	if j == nil {
		middleware.RespondJSON(w, http.StatusNotFound, false, "Job not found", "")
		return
	}

	middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", j)
}

var tectonicCompileSem = make(chan struct{}, 1)

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

	pdfData, err := compileLatexWithTectonic(req.LatexSource)
	if err != nil {
		middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	if _, err := w.Write(pdfData); err != nil {
		slog.Error("write resume pdf", "err", err)
	}
}

func compileLatexWithTectonic(latexSource string) ([]byte, error) {
	tectonicCompileSem <- struct{}{}
	defer func() { <-tectonicCompileSem }()

	tempDir, err := os.MkdirTemp("", "resume-compile-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	texPath := filepath.Join(tempDir, "resume.tex")
	if err := os.WriteFile(texPath, []byte(latexSource), 0o644); err != nil {
		return nil, fmt.Errorf("write tex: %w", err)
	}

	cmd := exec.Command("tectonic", texPath)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tectonic failed: %w\nOutput: %s", err, string(output))
	}

	pdfPath := filepath.Join(tempDir, "resume.pdf")
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}

	return pdfData, nil
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