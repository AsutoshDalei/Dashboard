package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type LLMProvider string

const (
	ProviderOpenRouter LLMProvider = "openrouter"
	ProviderOllama     LLMProvider = "ollama"
)

type AnalyzeRequest struct {
	JobDescription string `json:"job_description"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	OllamaHost     string `json:"ollama_host"`
}

type AnalyzeResponse struct {
	Score          float64  `json:"score"`
	Keywords       []string `json:"keywords"`
	Analysis       string   `json:"analysis"`
	Recommendations string  `json:"recommendations"`
	Archetype      string   `json:"archetype"`
}

type GenerateRequest struct {
	JobDescription string     `json:"job_description"`
	Score          float64    `json:"score"`
	Keywords       []string   `json:"keywords"`
	Recommendations string    `json:"recommendations"`
	ChatHistory    []ChatMsg  `json:"chat_history"`
	Provider       string     `json:"provider"`
	Model          string     `json:"model"`
	OllamaHost     string     `json:"ollama_host"`
	CompanyName    string     `json:"company_name"`
}

type ChatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type GenerateResponse struct {
	ModifiedLatex   string   `json:"modified_latex"`
	ChangesSummary  string   `json:"changes_summary"`
	KeywordsInjected []string `json:"keywords_injected"`
	SkillsRemoved   []string `json:"skills_removed"`
	SkillsAdded     []string `json:"skills_added"`
}

type ReanalyzeResponse struct {
	NewScore       float64 `json:"new_score"`
	NewAnalysis    string  `json:"new_analysis"`
	Improvement    string  `json:"improvement"`
	RemainingGaps  string  `json:"remaining_gaps"`
}

type ChatRequest struct {
	Message         string     `json:"message"`
	ChatHistory     []ChatMsg  `json:"chat_history"`
	CurrentLatex    string     `json:"current_latex"`
	JobDescription  string     `json:"job_description"`
	Provider        string     `json:"provider"`
	Model           string     `json:"model"`
	OllamaHost      string     `json:"ollama_host"`
}

type ChatResponse struct {
	ResponseText   string `json:"response_text"`
	ModifiedLatex  string `json:"modified_latex"`
	ChangesSummary string `json:"changes_summary"`
}

var (
	masterResumeLatex string
	systemPrompt      string
	analyzePromptTmpl string
	generatePromptTmpl string
	reanalyzePromptTmpl string
	chatPromptTmpl    string
)

func initResumeTailor() {
	baseDir := os.Getenv("RESUME_TAILOR_PATH")
	if baseDir == "" {
		candidates := []string{
			"pi_bundle/resume_tailor",
			"../pi_bundle/resume_tailor",
			filepath.Join(filepath.Dir(os.Args[0]), "resume_tailor"),
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				baseDir = c
				break
			}
		}
	}
	if baseDir == "" {
		slog.Warn("resume_tailor directory not found; resume tailoring will be unavailable")
		return
	}

	loadFile := func(path string) string {
		full := filepath.Join(baseDir, path)
		b, err := os.ReadFile(full)
		if err != nil {
			slog.Warn("failed to load resume tailor file", "path", full, "err", err)
			return ""
		}
		return string(b)
	}

	masterResumeLatex = loadFile("resume_template.tex")
	systemPrompt = loadFile("prompts/SYSTEM_PROMPT.md")
	analyzePromptTmpl = loadFile("prompts/ANALYZE_PROMPT.md")
	generatePromptTmpl = loadFile("prompts/GENERATE_PROMPT.md")
	reanalyzePromptTmpl = loadFile("prompts/REANALYZE_PROMPT.md")
	chatPromptTmpl = loadFile("prompts/CHAT_PROMPT.md")

	if masterResumeLatex == "" || systemPrompt == "" {
		slog.Warn("resume tailor: missing core files; feature will be unavailable")
	}

	slog.Info("resume tailor initialized", "base_dir", baseDir,
		"resume_size", len(masterResumeLatex),
		"prompts_loaded", systemPrompt != "" && analyzePromptTmpl != "" && generatePromptTmpl != "")
}

func callLLM(system, user string, provider LLMProvider, model, ollamaHost string) (string, error) {
	switch provider {
	case ProviderOpenRouter:
		return callOpenRouterWithPrompt(system, user)
	case ProviderOllama:
		return callOllama(system, user, model, ollamaHost)
	default:
		return "", fmt.Errorf("unknown LLM provider: %s", provider)
	}
}

func callOpenRouterWithPrompt(system, user string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return "", fmt.Errorf("OpenRouter API key not configured")
	}

	models := resolveOpenRouterModels()
	if len(models) == 0 {
		return "", fmt.Errorf("OpenRouter model not configured")
	}

	reqBody := openRouterRequest{
		Model: models[0],
		Messages: []openRouterMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	}
	if len(models) > 1 {
		reqBody.Models = models
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to encode request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenRouter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("OpenRouter returned non-JSON (status %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("OpenRouter error: %s", parsed.Error.Message)
		}
		return "", fmt.Errorf("OpenRouter error (status %d)", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("OpenRouter returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

type ollamaRequest struct {
	Model    string            `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Stream   bool              `json:"stream"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Error string `json:"error,omitempty"`
}

func callOllama(system, user, model, host string) (string, error) {
	if host == "" {
		host = os.Getenv("OLLAMA_HOST")
	}
	if host == "" {
		return "", fmt.Errorf("OLLAMA_HOST not configured in .env")
	}
	if !strings.Contains(host, ":") {
		host = host + ":11434"
	}
	if model == "" {
		model = os.Getenv("OLLAMA_MODEL")
	}
	if model == "" {
		return "", fmt.Errorf("OLLAMA_MODEL not configured in .env")
	}

	url := fmt.Sprintf("http://%s/api/chat", host)
	reqBody := ollamaRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to encode Ollama request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build Ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Ollama response: %w", err)
	}

	slog.Debug("ollama raw response", "body", string(body))

	var parsed ollamaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("Ollama returned non-JSON (status %d): %s", resp.StatusCode, string(body))
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("Ollama error: %s", parsed.Error)
	}
	if parsed.Message.Content == "" {
		return "", fmt.Errorf("Ollama returned empty content (status %d): %s", resp.StatusCode, string(body))
	}
	return parsed.Message.Content, nil
}

func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "```") {
		lines := strings.SplitN(raw, "\n", 2)
		if len(lines) == 2 {
			first := strings.TrimSpace(lines[0])
			if first == "```" || first == "```json" || first == "```json\n" {
				raw = strings.TrimSpace(lines[1])
			}
		}
	}
	if idx := strings.LastIndex(raw, "```"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	raw = strings.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '{' {
		return raw
	}
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[idx:]
		if end := strings.LastIndex(raw, "}"); end >= 0 {
			raw = raw[:end+1]
		}
	}
	return strings.TrimSpace(raw)
}

func AnalyzeResume(jobDescription, provider, model, ollamaHost string) (*AnalyzeResponse, error) {
	if masterResumeLatex == "" || systemPrompt == "" || analyzePromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing template or prompt files")
	}

	userPrompt := strings.ReplaceAll(analyzePromptTmpl, "{{RESUME_LATEX}}", masterResumeLatex)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)

	raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	slog.Debug("analyze llm response", "raw_len", len(raw), "raw_preview", truncate(raw, 200))

	raw = extractJSON(raw)
	if raw == "" {
		return nil, fmt.Errorf("LLM returned empty response after extraction")
	}

	var resp AnalyzeResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw: %s", err, raw)
	}

	if resp.Score < 1 || resp.Score > 5 {
		resp.Score = 3.0
	}
	if len(resp.Keywords) == 0 {
		resp.Keywords = []string{}
	}

	return &resp, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func GenerateTailoredResume(jobDescription string, score float64, keywords []string, recommendations string, chatHistory []ChatMsg, provider, model, ollamaHost, companyName string) (*GenerateResponse, error) {
	if masterResumeLatex == "" || systemPrompt == "" || generatePromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing template or prompt files")
	}

	kwJSON, _ := json.Marshal(keywords)
	chatJSON, _ := json.Marshal(chatHistory)

	userPrompt := strings.ReplaceAll(generatePromptTmpl, "{{RESUME_LATEX}}", masterResumeLatex)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)
	userPrompt = strings.ReplaceAll(userPrompt, "{{SCORE}}", fmt.Sprintf("%.1f", score))
	userPrompt = strings.ReplaceAll(userPrompt, "{{KEYWORDS}}", string(kwJSON))
	userPrompt = strings.ReplaceAll(userPrompt, "{{RECOMMENDATIONS}}", recommendations)
	userPrompt = strings.ReplaceAll(userPrompt, "{{CHAT_HISTORY}}", string(chatJSON))

	raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	raw = extractJSON(raw)
	var resp GenerateResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw: %s", err, raw)
	}

	if resp.ModifiedLatex == "" {
		return nil, fmt.Errorf("LLM returned empty modified_latex")
	}

	return &resp, nil
}

func ReanalyzeResume(modifiedLatex, jobDescription, provider, model, ollamaHost string) (*ReanalyzeResponse, error) {
	if systemPrompt == "" || reanalyzePromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing prompt files")
	}

	userPrompt := strings.ReplaceAll(reanalyzePromptTmpl, "{{RESUME_LATEX}}", modifiedLatex)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)

	raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
	if err != nil {
		return nil, fmt.Errorf("LLM reanalysis failed: %w", err)
	}

	raw = extractJSON(raw)
	var resp ReanalyzeResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw: %s", err, raw)
	}

	if resp.NewScore < 1 || resp.NewScore > 5 {
		resp.NewScore = 3.0
	}

	return &resp, nil
}

func ChatRefine(message string, chatHistory []ChatMsg, currentLatex, jobDescription, provider, model, ollamaHost string) (*ChatResponse, error) {
	if systemPrompt == "" || chatPromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing prompt files")
	}

	chatJSON, _ := json.Marshal(chatHistory)

	userPrompt := strings.ReplaceAll(chatPromptTmpl, "{{RESUME_LATEX}}", currentLatex)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)
	userPrompt = strings.ReplaceAll(userPrompt, "{{USER_MESSAGE}}", message)
	userPrompt = strings.ReplaceAll(userPrompt, "{{CHAT_HISTORY}}", string(chatJSON))

	raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
	if err != nil {
		return nil, fmt.Errorf("LLM chat failed: %w", err)
	}

	raw = extractJSON(raw)
	var resp ChatResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw: %s", err, raw)
	}

	return &resp, nil
}

func CompileLatexToPDF(latexSource string) ([]byte, error) {
	tectonicCompileSem <- struct{}{}
	defer func() { <-tectonicCompileSem }()

	tempDir, err := os.MkdirTemp("", "resume-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	texPath := filepath.Join(tempDir, "resume.tex")
	if err := os.WriteFile(texPath, []byte(latexSource), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write tex file: %w", err)
	}

	cmd := exec.Command("tectonic", texPath)
	cmd.Dir = tempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tectonic compilation failed: %w\nOutput: %s", err, string(output))
	}

	pdfPath := filepath.Join(tempDir, "resume.pdf")
	pdfData, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generated PDF: %w", err)
	}

	return pdfData, nil
}

func countWords(latexSource string) int {
	clean := latexSource
	clean = strings.ReplaceAll(clean, "\\", " ")
	clean = strings.ReplaceAll(clean, "{", " ")
	clean = strings.ReplaceAll(clean, "}", " ")
	clean = strings.ReplaceAll(clean, "%", " ")

	words := strings.Fields(clean)
	return len(words)
}