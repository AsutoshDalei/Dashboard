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
	ModifiedLatex  string `json:"modified_latex"`
	SkillsToRemove []string `json:"skills_to_remove"`
	SkillsToAdd    []string `json:"skills_to_add"`
	ChangesSummary string `json:"changes_summary"`
}

type ExperienceEdit struct {
	Company         string         `json:"company"`
	MainItemReorder []int          `json:"main_item_reorder"`
	MainItems       []MainItemEdit `json:"main_items"`
}

type MainItemEdit struct {
	Rewrites map[string]string `json:"rewrites"`
}

func (m *MainItemEdit) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Rewrites = make(map[string]string)

	for k, v := range raw {
		if k == "rewrites" {
			var inner map[string]string
			if err := json.Unmarshal(v, &inner); err == nil {
				for ik, iv := range inner {
					m.Rewrites[ik] = iv
				}
			}
		} else {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				m.Rewrites[k] = s
			}
		}
	}
	return nil
}

type SkillsSwap struct {
	Remove []string `json:"remove"`
	Add    []string `json:"add"`
}

type ChatResponse struct {
	ResponseText   string   `json:"response_text"`
	ModifiedLatex  string   `json:"modified_latex"`
	SkillsToRemove []string `json:"skills_to_remove"`
	SkillsToAdd    []string `json:"skills_to_add"`
	ChangesSummary string   `json:"changes_summary"`
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

	if masterResumeLatex != "" {
		computeResumePlainText()
	}

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
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages,omitempty"`
	Prompt   string              `json:"prompt,omitempty"`
	Stream   bool                `json:"stream"`
	Format   *json.RawMessage    `json:"format,omitempty"`
}

type ollamaResponse struct {
	Message struct {
		Content  string `json:"content"`
		Thinking string `json:"thinking"`
	} `json:"message"`
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

var skillsOnlySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"skills_to_remove": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Skills from the resume that are irrelevant to this JD and should be removed"
		},
		"skills_to_add": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Skills from the JD that the candidate has and should be added to the resume"
		},
		"changes_summary": {
			"type": "string",
			"description": "One sentence summary of the changes made"
		}
	},
	"required": ["skills_to_remove", "skills_to_add", "changes_summary"]
}`)

type SkillsOnlyEdits struct {
	SkillsToRemove  []string `json:"skills_to_remove"`
	SkillsToAdd     []string `json:"skills_to_add"`
	ChangesSummary  string   `json:"changes_summary"`
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

	combined := system + "\n\n" + user
	slog.Warn("ollama prompt size", "chars", len(combined))
	if len(combined) > 28000 {
		slog.Warn("ollama prompt may exceed context window", "prompt_chars", len(combined))
	}

	reqBody := ollamaRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "user", Content: combined},
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
	if parsed.Message.Content == "" && parsed.Message.Thinking != "" {
		parsed.Message.Content = parsed.Message.Thinking
	}
	if parsed.Message.Content == "" {
		return "", fmt.Errorf("Ollama returned empty content (status %d): %s", resp.StatusCode, string(body))
	}
	return parsed.Message.Content, nil
}

func callOllamaStructured(prompt, model, host string, schema json.RawMessage) (string, error) {
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

	url := fmt.Sprintf("http://%s/api/generate", host)
	reqBody := ollamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
		Format: &schema,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to encode Ollama request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build Ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Ollama response: %w", err)
	}

	var parsed ollamaResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("Ollama returned non-JSON (status %d): %s", resp.StatusCode, string(body))
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("Ollama error: %s", parsed.Error)
	}
	if parsed.Response == "" {
		return "", fmt.Errorf("Ollama returned empty structured response (status %d): %s", resp.StatusCode, string(body))
	}
	return parsed.Response, nil
}

func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if idx := strings.Index(raw, "{"); idx > 0 {
		raw = raw[idx:]
	}
	if idx := strings.LastIndex(raw, "}"); idx >= 0 {
		raw = raw[:idx+1]
	}

	if strings.HasPrefix(raw, "{") {
		raw = fixJSON(raw)
		return raw
	}
	return ""
}

func fixJSON(s string) string {
	if !strings.Contains(s, `"experience_edits":[`) {
		return s
	}

	prefix := `"experience_edits":[`
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return s
	}
	start := idx + len(prefix)

	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if i+1 < len(s) && s[i+1] == ',' {
					s = s[:i+1] + "]" + s[i+1:]
					return s
				}
			}
		}
	}
	return s
}

func AnalyzeResume(jobDescription, provider, model, ollamaHost string) (*AnalyzeResponse, error) {
	if masterResumeLatex == "" || systemPrompt == "" || analyzePromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing template or prompt files")
	}

	userPrompt := strings.ReplaceAll(analyzePromptTmpl, "{{RESUME_TEXT}}", resumePlainText)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)

	raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
	if err != nil {
		return nil, fmt.Errorf("LLM analysis failed: %w", err)
	}

	slog.Warn("analyze llm response", "raw_len", len(raw), "raw_preview", truncate(raw, 200))

	rawJSON := extractJSON(raw)
	if rawJSON == "" {
		return nil, fmt.Errorf("LLM returned empty response after extraction. Original: %s", truncate(raw, 500))
	}

	var resp AnalyzeResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw (first 2000): %s", err, truncate(raw, 2000))
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

var resumePlainText string

func computeResumePlainText() {
	resumePlainText = stripLatex(masterResumeLatex)
	slog.Info("resume plain text", "chars", len(resumePlainText), "latex_chars", len(masterResumeLatex))
}

func stripLatex(latex string) string {
	s := latex
	s = strings.ReplaceAll(s, "\\textbf{", "")
	s = strings.ReplaceAll(s, "\\textit{", "")
	s = strings.ReplaceAll(s, "\\href{", "")
	s = strings.ReplaceAll(s, "\\section*{", "")
	s = strings.ReplaceAll(s, "\\begin{itemize}", "")
	s = strings.ReplaceAll(s, "\\end{itemize}", "")
	s = strings.ReplaceAll(s, "\\begin{center}", "")
	s = strings.ReplaceAll(s, "\\end{center}", "")
	s = strings.ReplaceAll(s, "\\vspace{", "")
	s = strings.ReplaceAll(s, "\\hfill", "")
	s = strings.ReplaceAll(s, "\\namesize", "")
	s = strings.ReplaceAll(s, "\\headersize", "")
	s = strings.ReplaceAll(s, "\\em", "")
	s = strings.ReplaceAll(s, "\\item", "\n-")
	s = strings.ReplaceAll(s, "\\--", " - ")
	s = strings.ReplaceAll(s, "\\%", "%")
	s = strings.ReplaceAll(s, "\\$", "$")
	s = strings.ReplaceAll(s, "\\&", "&")
	s = strings.ReplaceAll(s, "\\textasciitilde{}", "~")
	s = strings.ReplaceAll(s, "\\textasciicircum{}", "^")
	s = strings.ReplaceAll(s, "\\textbackslash{}", "\\")
	s = strings.ReplaceAll(s, "~--~", " -- ")

	var out strings.Builder
	inBraces := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '{' {
			inBraces++
			continue
		}
		if ch == '}' {
			if inBraces > 0 {
				inBraces--
			}
			continue
		}
		if inBraces == 0 {
			out.WriteByte(ch)
		}
	}
	result := out.String()

	result = strings.ReplaceAll(result, "  ", " ")
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	result = strings.ReplaceAll(result, "\n\n\n", "\n\n")

	lines := strings.Split(result, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "\\") {
			clean = append(clean, trimmed)
		}
	}

	return strings.TrimSpace(strings.Join(clean, "\n"))
}

func GenerateTailoredResume(jobDescription string, score float64, keywords []string, recommendations string, chatHistory []ChatMsg, provider, model, ollamaHost, companyName string) (*GenerateResponse, error) {
	if masterResumeLatex == "" || systemPrompt == "" || generatePromptTmpl == "" {
		return nil, fmt.Errorf("resume tailor not initialized: missing template or prompt files")
	}

	kwJSON, _ := json.Marshal(keywords)

	userPrompt := strings.ReplaceAll(generatePromptTmpl, "{{RESUME_LATEX}}", masterResumeLatex)
	userPrompt = strings.ReplaceAll(userPrompt, "{{JOB_DESCRIPTION}}", jobDescription)
	userPrompt = strings.ReplaceAll(userPrompt, "{{SCORE}}", fmt.Sprintf("%.1f", score))
	userPrompt = strings.ReplaceAll(userPrompt, "{{KEYWORDS}}", string(kwJSON))
	userPrompt = strings.ReplaceAll(userPrompt, "{{RECOMMENDATIONS}}", recommendations)

	var edits SkillsOnlyEdits
	if LLMProvider(provider) == ProviderOllama {
		raw, err := callOllamaStructured(userPrompt, model, ollamaHost, skillsOnlySchema)
		if err != nil {
			return nil, fmt.Errorf("LLM generation failed: %w", err)
		}
		slog.Warn("generate structured response", "raw", raw)
		if err := json.Unmarshal([]byte(raw), &edits); err != nil {
			return nil, fmt.Errorf("failed to parse structured response: %w\nRaw: %s", err, raw)
		}
	} else {
		raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
		if err != nil {
			return nil, fmt.Errorf("LLM generation failed: %w", err)
		}
		slog.Warn("generate llm response", "raw_len", len(raw), "raw_preview", truncate(raw, 200))
		rawJSON := extractJSON(raw)
		if rawJSON == "" {
			return nil, fmt.Errorf("LLM returned empty generate response. Original: %s", truncate(raw, 500))
		}
		if err := json.Unmarshal([]byte(rawJSON), &edits); err != nil {
			return nil, fmt.Errorf("failed to parse LLM response: %w\nRaw (first 2000): %s", err, truncate(raw, 2000))
		}
	}

	resp := &GenerateResponse{
		SkillsToRemove:  edits.SkillsToRemove,
		SkillsToAdd:     edits.SkillsToAdd,
		ChangesSummary:  edits.ChangesSummary,
	}

	if resp.ChangesSummary == "" {
		resp.ChangesSummary = "Skills optimized"
	}

	resp.ModifiedLatex = masterResumeLatex
	if len(resp.SkillsToRemove) > 0 || len(resp.SkillsToAdd) > 0 {
		swap := &SkillsSwap{Remove: resp.SkillsToRemove, Add: resp.SkillsToAdd}
		resp.ModifiedLatex = applySkillsSwap(masterResumeLatex, swap)
	}

	return resp, nil
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

	slog.Warn("reanalyze llm response", "raw_len", len(raw), "raw_preview", truncate(raw, 200))

	rawJSON := extractJSON(raw)
	if rawJSON == "" {
		return nil, fmt.Errorf("LLM returned empty reanalyze response. Original: %s", truncate(raw, 500))
	}
	var resp ReanalyzeResponse
	if err := json.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM reanalyze response: %w\nRaw (first 2000): %s", err, truncate(raw, 2000))
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

	var edits SkillsOnlyEdits
	if LLMProvider(provider) == ProviderOllama {
		raw, err := callOllamaStructured(userPrompt, model, ollamaHost, skillsOnlySchema)
		if err != nil {
			return nil, fmt.Errorf("LLM chat failed: %w", err)
		}
		slog.Warn("chat structured response", "raw", raw)
		if err := json.Unmarshal([]byte(raw), &edits); err != nil {
			return nil, fmt.Errorf("failed to parse structured response: %w\nRaw: %s", err, raw)
		}
	} else {
		raw, err := callLLM(systemPrompt, userPrompt, LLMProvider(provider), model, ollamaHost)
		if err != nil {
			return nil, fmt.Errorf("LLM chat failed: %w", err)
		}
		slog.Warn("chat llm response", "raw_len", len(raw), "raw_preview", truncate(raw, 200))
		rawJSON := extractJSON(raw)
		if rawJSON == "" {
			return nil, fmt.Errorf("LLM returned empty chat response. Original: %s", truncate(raw, 500))
		}
		if err := json.Unmarshal([]byte(rawJSON), &edits); err != nil {
			return nil, fmt.Errorf("failed to parse chat response: %w\nRaw (first 2000): %s", err, truncate(raw, 2000))
		}
	}

	resp := &ChatResponse{
		ResponseText:   "Applied your suggestions.",
		SkillsToRemove: edits.SkillsToRemove,
		SkillsToAdd:    edits.SkillsToAdd,
		ChangesSummary: edits.ChangesSummary,
	}

	resp.ModifiedLatex = currentLatex
	if len(resp.SkillsToRemove) > 0 || len(resp.SkillsToAdd) > 0 {
		swap := &SkillsSwap{Remove: resp.SkillsToRemove, Add: resp.SkillsToAdd}
		resp.ModifiedLatex = applySkillsSwap(currentLatex, swap)
	}

	if resp.ChangesSummary != "" {
		resp.ResponseText = resp.ChangesSummary
	}

	return resp, nil
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

func findSection(latex, sectionName string) string {
	start := strings.Index(latex, `\section*{`+sectionName+`}`)
	if start < 0 {
		return ""
	}
	remainder := latex[start:]
	nextSection := strings.Index(remainder[1:], `\section*{`)
	if nextSection < 0 {
		return remainder
	}
	return remainder[:nextSection+1]
}

func applyAllEdits(latex string, edits *GenerateResponse) string {
	if len(edits.SkillsToRemove) > 0 || len(edits.SkillsToAdd) > 0 {
		swap := &SkillsSwap{Remove: edits.SkillsToRemove, Add: edits.SkillsToAdd}
		return applySkillsSwap(latex, swap)
	}
	return latex
}

func applyAllChatEdits(latex string, edits *ChatResponse) string {
	if len(edits.SkillsToRemove) > 0 || len(edits.SkillsToAdd) > 0 {
		swap := &SkillsSwap{Remove: edits.SkillsToRemove, Add: edits.SkillsToAdd}
		return applySkillsSwap(latex, swap)
	}
	return latex
}

func applySkillsSwap(latex string, swap *SkillsSwap) string {
	skillsStart := strings.Index(latex, `\section*{TECHNICAL SKILLS}`)
	if skillsStart < 0 {
		slog.Warn("skills section not found in resume template")
		return latex
	}

	skillsEnd := strings.Index(latex[skillsStart+1:], `\section*{`)
	if skillsEnd < 0 {
		skillsEnd = len(latex) - skillsStart - 1
	}
	skillsEnd = skillsStart + 1 + skillsEnd

	skillsSection := latex[skillsStart:skillsEnd]
	modified := skillsSection

	for _, skill := range swap.Remove {
		modified = removeSkill(modified, skill)
	}
	for _, skill := range swap.Add {
		modified = addSkill(modified, skill)
	}

	return strings.Replace(latex, skillsSection, modified, 1)
}

func removeSkill(section, skill string) string {
	skill = strings.TrimSpace(skill)
	idx := strings.Index(section, skill)
	for idx >= 0 {
		before := idx
		after := idx + len(skill)
		for after < len(section) && (section[after] == ',' || section[after] == ' ' || section[after] == '.' || section[after] == ';') {
			after++
		}
		section = section[:before] + section[after:]
		idx = strings.Index(section, skill)
	}
	return section
}

func addSkill(section, skill string) string {
	lastColon := strings.LastIndex(section, "{: ")
	if lastColon < 0 {
		return section
	}
	lineEnd := strings.Index(section[lastColon:], `} \\`)
	if lineEnd < 0 {
		lineEnd = strings.Index(section[lastColon:], `}`)
	}
	if lineEnd < 0 {
		return section
	}
	lineEnd = lastColon + lineEnd
	insert := section[lastColon : lastColon+3]
	if !strings.HasSuffix(strings.TrimSpace(section[lastColon+3:lineEnd]), ".") {
		insert += ", "
	}
	insert += skill + "."
	section = section[:lastColon+3] + insert[3:] + section[lineEnd:]
	return section
}

func applyExperienceEdits(latex string, edits []ExperienceEdit) string {
	workStart := strings.Index(latex, `\section*{WORK EXPERIENCE}`)
	if workStart < 0 {
		slog.Warn("work experience section not found")
		return latex
	}
	nextSection := strings.Index(latex[workStart+1:], `\section*{`)
	if nextSection < 0 {
		return latex
	}
	workEnd := workStart + 1 + nextSection
	workSection := latex[workStart:workEnd]

	for _, edit := range edits {
		companyBlock := findCompanyBlock(workSection, edit.Company)
		if companyBlock == "" {
			slog.Warn("company not found in experience section", "company", edit.Company)
			continue
		}
		modified := applyCompanyEdit(companyBlock, &edit)
		workSection = strings.Replace(workSection, companyBlock, modified, 1)
	}

	return strings.Replace(latex, latex[workStart:workEnd], workSection, 1)
}

func findCompanyBlock(section, company string) string {
	patterns := []string{
		company + " --",
		company + " (R&D Division) --",
		company + " (R\\&D Division) --",
	}
	var matchIdx int = -1
	for _, p := range patterns {
		idx := strings.Index(section, p)
		if idx >= 0 && (matchIdx < 0 || idx < matchIdx) {
			matchIdx = idx
		}
	}
	if matchIdx < 0 {
		return ""
	}

	blockStart := strings.LastIndex(section[:matchIdx], `\textbf{`)
	if blockStart < 0 {
		blockStart = matchIdx
	}
	blockEnd := len(section)
	nextCompany := strings.Index(section[matchIdx+len(company)+3:], `\\`)
	if nextCompany >= 0 {
		candidate := matchIdx + len(company) + 3 + nextCompany
		vspace := strings.Index(section[candidate:], `\vspace{`)
		if vspace >= 0 {
			blockEnd = candidate + vspace
		}
	}

	return section[blockStart:blockEnd]
}

func applyCompanyEdit(block string, edit *ExperienceEdit) string {
	itemsStart := strings.Index(block, `\begin{itemize}`)
	if itemsStart < 0 {
		return block
	}
	itemsEnd := strings.LastIndex(block, `\end{itemize}`)
	if itemsEnd < 0 {
		return block
	}
	itemsEnd += len(`\end{itemize}`)

	itemsContent := block[itemsStart:itemsEnd]
	beforeItems := block[:itemsStart]
	afterItems := block[itemsEnd:]

	mainItems := splitMainItems(itemsContent)
	if len(mainItems) == 0 {
		return block
	}

	if len(edit.MainItemReorder) > 0 && len(edit.MainItemReorder) <= len(mainItems) {
		reordered := make([]string, len(mainItems))
		for i, idx := range edit.MainItemReorder {
			if idx >= 0 && idx < len(mainItems) {
				reordered[i] = mainItems[idx]
			} else {
				reordered[i] = mainItems[i]
			}
		}
		for i := range reordered {
			if reordered[i] == "" {
				reordered[i] = mainItems[i]
			}
		}
		mainItems = reordered
	}

	for i, item := range mainItems {
		if i < len(edit.MainItems) && len(edit.MainItems[i].Rewrites) > 0 {
			mainItems[i] = applyRewrites(item, edit.MainItems[i].Rewrites)
		}
	}

	rebuilt := beforeItems
	for i, item := range mainItems {
		if i == 0 {
			rebuilt += `\begin{itemize}` + "\n"
		}
		rebuilt += strings.TrimPrefix(item, `\begin{itemize}`)
		if i == len(mainItems)-1 {
			if !strings.HasSuffix(rebuilt, "\n") {
				rebuilt += "\n"
			}
			rebuilt += `\end{itemize}`
		}
	}
	rebuilt += afterItems

	return rebuilt
}

func splitMainItems(itemsBlock string) []string {
	itemsBlock = strings.TrimSpace(itemsBlock)
	itemsBlock = strings.TrimPrefix(itemsBlock, `\begin{itemize}`)
	itemsBlock = strings.TrimSuffix(itemsBlock, `\end{itemize}`)
	itemsBlock = strings.TrimSpace(itemsBlock)

	var items []string
	depth := 0
	start := -1
	pos := 0

	for pos < len(itemsBlock) {
		itemIdx := strings.Index(itemsBlock[pos:], `\item `)
		beginIdx := strings.Index(itemsBlock[pos:], `\begin{itemize}`)
		endIdx := strings.Index(itemsBlock[pos:], `\end{itemize}`)

		next := len(itemsBlock)
		var kind string
		if itemIdx >= 0 && pos+itemIdx < next {
			next = pos + itemIdx
			kind = "item"
		}
		if beginIdx >= 0 && pos+beginIdx < next {
			next = pos + beginIdx
			kind = "begin"
		}
		if endIdx >= 0 && pos+endIdx < next {
			next = pos + endIdx
			kind = "end"
		}

		if kind == "" {
			break
		}

		switch kind {
		case "item":
			if depth == 0 {
				if start >= 0 {
					items = append(items, strings.TrimSpace(itemsBlock[start:next]))
				}
				start = next
			}
			pos = next + len(`\item `)
		case "begin":
			depth++
			pos = next + len(`\begin{itemize}`)
		case "end":
			depth--
			pos = next + len(`\end{itemize}`)
		}
	}

	if start >= 0 {
		items = append(items, strings.TrimSpace(itemsBlock[start:]))
	}

	return items
}

func applyRewrites(mainItem string, rewrites map[string]string) string {
	innerStart := strings.Index(mainItem, `\begin{itemize}`)
	if innerStart < 0 {
		return mainItem
	}
	innerEnd := strings.LastIndex(mainItem, `\end{itemize}`)
	if innerEnd < 0 {
		return mainItem
	}
	innerEnd += len(`\end{itemize}`)

	before := mainItem[:innerStart]
	inner := mainItem[innerStart:innerEnd]
	after := mainItem[innerEnd:]

	subItems := splitSubItems(inner)

	for idxStr, newText := range rewrites {
		idx := 0
		fmt.Sscanf(idxStr, "%d", &idx)
		if idx >= 0 && idx < len(subItems) {
			subItems[idx] = `\item ` + newText
		}
	}

	rebuilt := before
	for i, si := range subItems {
		if i == 0 {
			rebuilt += `\begin{itemize}` + "\n"
		}
		rebuilt += si + "\n"
		if i == len(subItems)-1 {
			rebuilt += `\end{itemize}`
		}
	}
	rebuilt += after
	return rebuilt
}

func splitSubItems(innerBlock string) []string {
	innerBlock = strings.TrimSpace(innerBlock)
	innerBlock = strings.TrimPrefix(innerBlock, `\begin{itemize}`)
	innerBlock = strings.TrimSuffix(innerBlock, `\end{itemize}`)
	innerBlock = strings.TrimSpace(innerBlock)

	var items []string
	rest := innerBlock
	for {
		itemIdx := strings.Index(rest, `\item `)
		if itemIdx < 0 {
			break
		}
		rest = rest[itemIdx:]
		nextItem := strings.Index(rest[len(`\item `):], `\item `)
		endItem := strings.Index(rest[len(`\item `):], `\end{itemize}`)
		if nextItem < 0 {
			if endItem >= 0 {
				items = append(items, strings.TrimSpace(rest[:len(`\item `)+endItem]))
			} else {
				items = append(items, strings.TrimSpace(rest))
			}
			break
		}
		nextItem += len(`\item `)
		if endItem >= 0 && endItem+len(`\end{itemize}`) < nextItem {
			items = append(items, strings.TrimSpace(rest[:len(`\item `)+endItem+len(`\end{itemize}`)]))
			break
		}
		items = append(items, strings.TrimSpace(rest[:nextItem]))
		rest = rest[nextItem:]
	}
	return items
}

func applyProjectReorder(latex string, order []int) string {
	projStart := strings.Index(latex, `\section*{PROJECTS}`)
	if projStart < 0 {
		return latex
	}
	remainder := latex[projStart:]
	nextSection := strings.Index(remainder[1:], `\section*{`)
	if nextSection < 0 {
		return latex
	}
	projEnd := projStart + 1 + nextSection
	projSection := latex[projStart:projEnd]

	projects := splitProjects(projSection)
	if len(projects) < 2 || len(order) < 2 {
		return latex
	}

	reordered := make([]string, len(projects))
	for i, idx := range order {
		if idx >= 0 && idx < len(projects) {
			reordered[i] = projects[idx]
		} else {
			reordered[i] = projects[i]
		}
	}
	for i := range reordered {
		if reordered[i] == "" {
			reordered[i] = projects[i]
		}
	}

	projHeader := projSection
	if idx := strings.Index(projSection, `\textbf{`); idx >= 0 {
		projHeader = projSection[:idx]
	}

	rebuilt := projHeader
	for _, p := range reordered {
		rebuilt += p + "\n\n"
	}
	rebuilt = strings.TrimSpace(rebuilt)

	return strings.Replace(latex, projSection, rebuilt, 1)
}

func splitProjects(section string) []string {
	var projects []string
	rest := section
	for {
		itemIdx := strings.Index(rest, `\textbf{`)
		if itemIdx < 0 {
			break
		}
		rest = rest[itemIdx:]
		nextItem := strings.Index(rest[len(`\textbf{`):], `\textbf{`)
		if nextItem < 0 {
			projects = append(projects, strings.TrimSpace(rest))
			break
		}
		nextItem += len(`\textbf{`)
		projects = append(projects, strings.TrimSpace(rest[:nextItem]))
		rest = rest[nextItem:]
	}
	return projects
}