package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	llm "pi_dashboard/internal/llm"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/memory"
)

type Session struct {
	ID        string
	Memory    *memory.ConversationBuffer
	CreatedAt time.Time
}

type ChatStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewChatStore() *ChatStore {
	return &ChatStore{
		sessions: make(map[string]*Session),
	}
}

func (s *ChatStore) GetOrCreate(sessionID string, llmModel llms.Model, systemPrompt string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		return sess
	}
	mem := memory.NewConversationBuffer()
	sess := &Session{
		ID:        sessionID,
		Memory:    mem,
		CreatedAt: time.Now(),
	}
	s.sessions[sessionID] = sess
	return sess
}

func (s *ChatStore) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *ChatStore) Prune(validSessions map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.sessions {
		if !validSessions[id] {
			delete(s.sessions, id)
		}
	}
}

type Service struct {
	llm            llms.Model
	chatStore      *ChatStore
	resumeText     string
	resumeMarkdown string
	prompts        *llm.Prompts
}

func NewService(llmModel llms.Model, resumeText string, resumeMarkdown string, prompts *llm.Prompts) *Service {
	return &Service{
		llm:            llmModel,
		chatStore:      NewChatStore(),
		resumeText:     resumeText,
		resumeMarkdown: resumeMarkdown,
		prompts:        prompts,
	}
}

type ProviderParams struct {
	Provider string
	Model    string
	Host     string
}

func (s *Service) getProvider(params *ProviderParams) (llms.Model, error) {
	switch params.Provider {
	case "ollama":
		host := params.Host
		if host == "" {
			host = os.Getenv("OLLAMA_HOST")
		}
		if host == "" {
			host = "172.16.7.112:11434"
		} else if !strings.Contains(host, ":") {
			host = host + ":11434"
		}
		model := params.Model
		if model == "" {
			model = os.Getenv("OLLAMA_MODEL")
		}
		if model == "" {
			model = "gemma4"
		}
		return ollama.New(
			ollama.WithModel(model),
			ollama.WithServerURL(host),
		)
	case "openrouter":
		model := params.Model
		if model == "" {
			model = os.Getenv("OPENROUTER_MODEL")
		}
		if model == "" {
			model = "deepseek/deepseek-v4-flash"
		}
		apiKey := os.Getenv("OPENROUTER_API_KEY")
		return openai.New(
			openai.WithModel(model),
			openai.WithBaseURL("https://openrouter.ai/api/v1"),
			openai.WithToken(apiKey),
		)
	default:
		return s.llm, nil
	}
}

func (s *Service) Chat(ctx context.Context, sessionID string, message string, systemPrompt string) (string, error) {
	session := s.chatStore.GetOrCreate(sessionID, s.llm, systemPrompt)

	fullSystemPrompt := systemPrompt
	if s.resumeMarkdown != "" {
		fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
	}

	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(fullSystemPrompt)}},
	}

	// Add history from memory
	history, _ := session.Memory.LoadMemoryVariables(ctx, nil)
	if hist, ok := history["history"]; ok {
		if histStr, ok := hist.(string); ok && histStr != "" {
			messages = append(messages, llms.MessageContent{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart("Previous conversation:\n" + histStr)},
			})
		}
	}

	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(message)},
	})

	slog.Debug("chat request", "session_id", sessionID, "message_count", len(messages), "user_message_len", len(message))

	resp, err := s.llm.GenerateContent(ctx, messages, llms.WithThinkingMode(llms.ThinkingModeAuto))
	if err != nil {
		slog.Error("chat error", "session_id", sessionID, "err", err)
		return "", fmt.Errorf("chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("chat: no response")
	}

	if resp.Choices[0].ReasoningContent != "" {
		slog.Debug("chat reasoning", "thinking_len", len(resp.Choices[0].ReasoningContent))
	}

	content := resp.Choices[0].Content

	// Save to memory
	session.Memory.SaveContext(ctx,
		map[string]any{"input": message},
		map[string]any{"output": content},
	)

	return content, nil
}

func (s *Service) ChatStream(ctx context.Context, sessionID string, message string, systemPrompt string) (<-chan string, error) {
	session := s.chatStore.GetOrCreate(sessionID, s.llm, systemPrompt)

	fullSystemPrompt := systemPrompt
	if s.resumeMarkdown != "" {
		fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
	}

	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(fullSystemPrompt)}},
	}

	// Add history from memory
	history, _ := session.Memory.LoadMemoryVariables(ctx, nil)
	if hist, ok := history["history"]; ok {
		if histStr, ok := hist.(string); ok && histStr != "" {
			messages = append(messages, llms.MessageContent{
				Role:  llms.ChatMessageTypeHuman,
				Parts: []llms.ContentPart{llms.TextPart("Previous conversation:\n" + histStr)},
			})
		}
	}

	messages = append(messages, llms.MessageContent{
		Role:  llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{llms.TextPart(message)},
	})

	ch := make(chan string)
	go func() {
		defer close(ch)
		var full string
		resp, err := s.llm.GenerateContent(ctx, messages,
			llms.WithThinkingMode(llms.ThinkingModeAuto),
			llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
				full += string(chunk)
				ch <- string(chunk)
				return nil
			}),
		)
		if err != nil {
			slog.Error("chat stream error", "session_id", sessionID, "err", err)
			return
		}

		if len(resp.Choices) > 0 && resp.Choices[0].ReasoningContent != "" {
			slog.Debug("chat stream reasoning", "thinking_len", len(resp.Choices[0].ReasoningContent))
		}

		// Save to memory after streaming completes
		session.Memory.SaveContext(ctx,
			map[string]any{"input": message},
			map[string]any{"output": full},
		)
	}()

	return ch, nil
}

func (s *Service) ClearChat(sessionID string) {
	s.chatStore.Clear(sessionID)
}

var resumeAnalysisSchema = map[string]any{
	"type": "json_schema",
	"json_schema": map[string]any{
		"name":   "resume_analysis",
		"strict": true,
		"schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{
				"score": map[string]any{
					"type":        "number",
					"description": "ATS match score from 0 to 5",
				},
				"keywords": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Missing or underrepresented hard skills and keywords from the JD",
				},
				"analysis": map[string]any{
					"type":        "string",
					"description": "Structured comparison of required/preferred skills, strengths, gaps, and red flags",
				},
				"recommendations": map[string]any{
					"type":        "string",
					"description": "Specific resume tailoring strategy and application priority",
				},
				"archetype": map[string]any{
					"type":        "string",
					"description": "Best-fit role type for this candidate given the JD",
				},
			},
			"required":             []string{"score", "keywords", "analysis", "recommendations", "archetype"},
			"additionalProperties": false,
		},
	},
}

func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string, params *ProviderParams) (string, error) {
	provider, err := s.getProvider(params)
	if err != nil {
		return "", fmt.Errorf("get provider: %w", err)
	}

	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(systemPrompt)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Resume:\n%s\n\nJob Description:\n%s", s.resumeMarkdown, jobDescription))}},
	}

	var options []llms.CallOption
	if params.Provider == "openrouter" {
		// OpenRouter supports response_format via OpenAI API
		options = append(options, llms.WithJSONMode())
	}
	options = append(options, llms.WithThinkingMode(llms.ThinkingModeAuto))

	resp, err := provider.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("resume analyze: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("resume analyze: no response")
	}
	if resp.Choices[0].ReasoningContent != "" {
		slog.Debug("resume analyze reasoning", "thinking_len", len(resp.Choices[0].ReasoningContent))
	}
	return resp.Choices[0].Content, nil
}

func (s *Service) GenerateResume(ctx context.Context, jobDescription string, analysis string, systemPrompt string) (string, error) {
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(systemPrompt)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Job Description: %s\n\nAnalysis: %s\n\nResume: %s", jobDescription, analysis, s.resumeText))}},
	}
	resp, err := s.llm.GenerateContent(ctx, messages, llms.WithThinkingMode(llms.ThinkingModeAuto))
	if err != nil {
		return "", fmt.Errorf("resume generate: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("resume generate: no response")
	}
	if resp.Choices[0].ReasoningContent != "" {
		slog.Debug("resume generate reasoning", "thinking_len", len(resp.Choices[0].ReasoningContent))
	}
	return resp.Choices[0].Content, nil
}

func (s *Service) GenerateSQL(ctx context.Context, naturalLanguage string, schemaDoc string) (string, error) {
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(schemaDoc)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(naturalLanguage)}},
	}
	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("sql: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("sql: no response")
	}
	return resp.Choices[0].Content, nil
}

func (s *Service) DraftEmail(ctx context.Context, name, company string) (string, error) {
	template := s.prompts.Format("email_draft", map[string]string{"company": company, "name": name})
	return s.llm.Call(ctx, template)
}

func (s *Service) DraftCoverLetter(ctx context.Context, company string) (string, error) {
	template := s.prompts.Format("coverletter_draft", map[string]string{"company": company})
	prompt := fmt.Sprintf("%s\n\nResume context:\n%s", template, s.resumeText)
	return s.llm.Call(ctx, prompt)
}

func (s *Service) AnalyzeJobMatch(ctx context.Context, jobDescription string) (string, error) {
	prompt := fmt.Sprintf("%s\n\nResume:\n%s\n\nJob Description:\n%s", s.prompts.Get("job_match"), s.resumeText, jobDescription)
	return s.llm.Call(ctx, prompt)
}

type SkillsResponse struct {
	Skills []SkillCategory `json:"skills"`
}

type SkillCategory struct {
	Category string `json:"category"`
	Items    string `json:"items"`
}

func (s *Service) GenerateSkills(ctx context.Context, jobDescription string, params *ProviderParams) (string, error) {
	currentSkills := extractSkills(s.resumeText)
	provider, err := s.getProvider(params)
	if err != nil {
		return "", fmt.Errorf("get provider: %w", err)
	}

	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(s.prompts.Get("resume_generate"))}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Job Description:\n%s\n\nCurrent Skills Section:\n%s\n\nUpdate the skills to better match the job description. Add relevant keywords that are genuinely applicable. Remove irrelevant skills. Return the complete updated skills list.", jobDescription, currentSkills))}},
	}

	var options []llms.CallOption
	if params.Provider == "openrouter" {
		options = append(options, llms.WithJSONMode())
	}
	options = append(options, llms.WithThinkingMode(llms.ThinkingModeAuto))

	resp, err := provider.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("generate skills: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("generate skills: no response")
	}

	if resp.Choices[0].ReasoningContent != "" {
		slog.Debug("generate skills reasoning", "thinking_len", len(resp.Choices[0].ReasoningContent))
	}

	return rebuildResume(s.resumeText, resp.Choices[0].Content)
}

func extractSkills(resume string) string {
	re := regexp.MustCompile(`(?s)\\section\*\{TECHNICAL SKILLS\}(.*?)(?:\\section\*|$)`)
	matches := re.FindStringSubmatch(resume)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func escapeLatex(s string) string {
	replacer := strings.NewReplacer(
		`&`, `\&`,
		`%`, `\%`,
		`$`, `\$`,
		`#`, `\#`,
		`_`, `\_`,
		`{`, `\{`,
		`}`, `\}`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	)
	return replacer.Replace(s)
}

func sanitizeJSON(s string) string {
	var result strings.Builder
	inString := false
	escaped := false
	for _, r := range s {
		if escaped {
			result.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && inString {
			result.WriteRune(r)
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			result.WriteRune(r)
			continue
		}
		if inString && (r == '\n' || r == '\r') {
			result.WriteString("\\n")
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

func rebuildResume(resume string, skillsJSON string) (string, error) {
	cleaned := sanitizeJSON(skillsJSON)
	var result SkillsResponse
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return "", fmt.Errorf("parse skills JSON: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("\\begin{itemize}\n")
	for _, cat := range result.Skills {
		sb.WriteString(fmt.Sprintf("    \\item \\textbf{%s:} %s.\n", escapeLatex(cat.Category), escapeLatex(cat.Items)))
	}
	sb.WriteString("\\end{itemize}")

	newSkills := sb.String()

	re := regexp.MustCompile(`(?s)(\\section\*\{TECHNICAL SKILLS\})\s*.*?(\\section\*|$)`)
	updated := re.ReplaceAllString(resume, fmt.Sprintf("${1}\n%s\n\n${2}", newSkills))

	return updated, nil
}
