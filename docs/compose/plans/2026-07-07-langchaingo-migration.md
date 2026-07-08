# langchaingo Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all native LLM HTTP clients with langchaingo, adopt langchaingo memory for workspace chat, and use structured output for resume analysis.

**Architecture:** Replace `pkg/ollama`, `pkg/openrouter`, and `pkg/llm` with langchaingo's `llms/ollama` and `llms/openai` providers. Use `memory.ConversationBuffer` for workspace chat sessions. Use `openai.WithResponseFormat` for structured JSON output.

**Tech Stack:** Go, github.com/tmc/langchaingo v0.1.14

## Global Constraints

- Go 1.25.7 (per go.mod)
- CGO_ENABLED=0 for cross-compilation
- Environment variables: `LLM_PROVIDER`, `OLLAMA_HOST`, `OLLAMA_MODEL`, `OPENROUTER_API_KEY`, `OPENROUTER_MODEL`
- No changes to build.sh or GitHub workflows
- Must pass existing tests

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `go.mod` | Add langchaingo dependency |
| Delete | `pkg/ollama/client.go` | Remove native Ollama client |
| Delete | `pkg/openrouter/client.go` | Remove native OpenRouter client |
| Delete | `pkg/llm/provider.go` | Remove custom Provider interface |
| Rewrite | `internal/llm/factory.go` | Create langchaingo LLM instances |
| Rewrite | `internal/llm/service.go` | Use `llms.Model` interface |
| Rewrite | `internal/workspace/service.go` | Use langchaingo memory + chains |
| Rewrite | `internal/tracker/service.go` | Use `llms.Model` directly |
| Modify | `main.go` | Update provider initialization |
| Modify | `internal/llm/service_test.go` | Update tests |

---

### Task 1: Add langchaingo Dependency

**Covers:** S3

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated)

**Interfaces:**
- Consumes: (none)
- Produces: `github.com/tmc/langchaingo` available for import

- [ ] **Step 1: Add dependency**

Run: `go get github.com/tmc/langchaingo@v0.1.14`

- [ ] **Step 2: Tidy modules**

Run: `go mod tidy`

- [ ] **Step 3: Verify import works**

Run: `go build ./...`
Expected: Compiles (will fail on deleted packages later, that's expected)

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add langchaingo v0.1.14"
```

---

### Task 2: Rewrite LLM Factory

**Covers:** S3, S4, S5

**Files:**
- Rewrite: `internal/llm/factory.go`

**Interfaces:**
- Consumes: `github.com/tmc/langchaingo/llms`, `github.com/tmc/langchaingo/llms/ollama`, `github.com/tmc/langchaingo/llms/openai`
- Produces: `llms.Model` (single provider or fallback wrapper)

- [ ] **Step 1: Write the new factory**

```go
package llm

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

type Config struct {
	ProviderType     string
	OllamaHost       string
	OllamaModel      string
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// fallbackLLM tries each client in order until one succeeds.
type fallbackLLM struct {
	clients []llms.Model
}

func (f *fallbackLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	var lastErr error
	for i, c := range f.clients {
		resp, err := c.Call(ctx, prompt, options...)
		if err == nil {
			return resp, nil
		}
		slog.Warn("fallback model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return "", lastErr
}

func (f *fallbackLLM) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	var lastErr error
	for i, c := range f.clients {
		resp, err := c.GenerateContent(ctx, messages, options...)
		if err == nil {
			return resp, nil
		}
		slog.Warn("fallback model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return nil, lastErr
}

func (f *fallbackLLM) CreateEmbedding(ctx context.Context, inputTexts []string) ([][]float32, error) {
	return nil, fmt.Errorf("embedding not supported by fallback provider")
}

func NewProvider(cfg Config) (llms.Model, error) {
	switch cfg.ProviderType {
	case "ollama":
		if cfg.OllamaHost == "" {
			cfg.OllamaHost = "172.16.7.112:11434"
		} else if !strings.Contains(cfg.OllamaHost, ":") {
			cfg.OllamaHost = cfg.OllamaHost + ":11434"
		}
		if cfg.OllamaModel == "" {
			cfg.OllamaModel = "gemma4"
		}
		return ollama.New(
			ollama.WithModel(cfg.OllamaModel),
			ollama.WithServerURL(cfg.OllamaHost),
		)

	case "openrouter":
		if cfg.OpenRouterAPIKey == "" {
			return nil, fmt.Errorf("OPENROUTER_API_KEY required")
		}
		if cfg.OpenRouterModel == "" {
			return nil, fmt.Errorf("OPENROUTER_MODEL required")
		}
		models := ResolveModels(cfg.OpenRouterModel)
		if len(models) == 0 {
			return nil, fmt.Errorf("OPENROUTER_MODEL invalid")
		}
		clients := make([]llms.Model, 0, len(models))
		for _, m := range models {
			client, err := openai.New(
				openai.WithModel(m),
				openai.WithBaseURL("https://openrouter.ai/api/v1"),
				openai.WithToken(cfg.OpenRouterAPIKey),
			)
			if err != nil {
				return nil, fmt.Errorf("openrouter client %s: %w", m, err)
			}
			clients = append(clients, client)
		}
		if len(clients) == 1 {
			return clients[0], nil
		}
		return &fallbackLLM{clients: clients}, nil

	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.ProviderType)
	}
}

func ResolveModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/llm/...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add internal/llm/factory.go
git commit -m "refactor: rewrite LLM factory to use langchaingo"
```

---

### Task 3: Rewrite LLM Service

**Covers:** S3, S9

**Files:**
- Rewrite: `internal/llm/service.go`

**Interfaces:**
- Consumes: `github.com/tmc/langchaingo/llms`
- Produces: `*Service` with `Chat`, `Generate`, `GenerateSQL`, `AnalyzeResume`, `GenerateCoverLetter`, `DraftEmail`, `AnalyzeJobMatch`

- [ ] **Step 1: Write the new service**

```go
package llm

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
)

type Service struct {
	llm llms.Model
}

func NewService(llm llms.Model) *Service {
	return &Service{llm: llm}
}

func (s *Service) Chat(ctx context.Context, messages []llms.MessageContent) (string, error) {
	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("chat: no response")
	}
	return resp.Choices[0].Content, nil
}

func (s *Service) Generate(ctx context.Context, prompt string) (string, error) {
	return s.llm.Call(ctx, prompt)
}

func (s *Service) GenerateSQL(ctx context.Context, naturalLanguage string, schemaDoc string) (string, error) {
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(schemaDoc)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(naturalLanguage)}},
	}
	return s.Chat(ctx, messages)
}

func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string) (string, error) {
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(systemPrompt)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(jobDescription)}},
	}
	return s.Chat(ctx, messages)
}

func (s *Service) GenerateCoverLetter(ctx context.Context, companyName string, resumeText string) (string, error) {
	prompt := fmt.Sprintf("Write a professional cover letter for %s.\n\nResume context:\n%s", companyName, resumeText)
	return s.Generate(ctx, prompt)
}

func (s *Service) DraftEmail(ctx context.Context, name, company string) (string, error) {
	prompt := fmt.Sprintf("Draft a professional email expressing interest in ML Engineer and Applied Scientist roles at %s. Address it to %s.", company, name)
	return s.Generate(ctx, prompt)
}

func (s *Service) AnalyzeJobMatch(ctx context.Context, resumeText, jobDescription string) (string, error) {
	prompt := fmt.Sprintf("Analyze how well the following resume matches this job description.\n\nResume:\n%s\n\nJob Description:\n%s", resumeText, jobDescription)
	return s.Generate(ctx, prompt)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/llm/...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add internal/llm/service.go
git commit -m "refactor: rewrite LLM service to use langchaingo llms.Model"
```

---

### Task 4: Rewrite Workspace Service

**Covers:** S3, S6, S7, S8, S9

**Files:**
- Rewrite: `internal/workspace/service.go`

**Interfaces:**
- Consumes: `github.com/tmc/langchaingo/llms`, `github.com/tmc/langchaingo/memory`, `github.com/tmc/langchaingo/chains`
- Produces: `*Service` with chat, streaming, resume analysis, skills generation

- [ ] **Step 1: Write the new workspace service**

```go
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

	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
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

	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		slog.Error("chat error", "session_id", sessionID, "err", err)
		return "", fmt.Errorf("chat: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("chat: no response")
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
		_, err := s.llm.GenerateContent(ctx, messages,
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

	resp, err := provider.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("resume analyze: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("resume analyze: no response")
	}
	return resp.Choices[0].Content, nil
}

func (s *Service) GenerateResume(ctx context.Context, jobDescription string, analysis string, systemPrompt string) (string, error) {
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(systemPrompt)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Job Description: %s\n\nAnalysis: %s\n\nResume: %s", jobDescription, analysis, s.resumeText))}},
	}
	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("resume generate: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("resume generate: no response")
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

	schema := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "skills_update",
			"strict": true,
			"schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{
					"skills": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": map[string]any{
								"category": map[string]any{
									"type":        "string",
									"description": "Skill category name (e.g. ML & AI, Languages)",
								},
								"items": map[string]any{
									"type":        "string",
									"description": "Comma-separated skill items for this category",
								},
							},
							"required":             []string{"category", "items"},
							"additionalProperties": false,
						},
					},
				},
				"required":             []string{"skills"},
				"additionalProperties": false,
			},
		},
	}

	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(s.prompts.Get("resume_generate"))}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Job Description:\n%s\n\nCurrent Skills Section:\n%s\n\nUpdate the skills to better match the job description. Add relevant keywords that are genuinely applicable. Remove irrelevant skills. Return the complete updated skills list.", jobDescription, currentSkills))}},
	}

	var options []llms.CallOption
	if params.Provider == "openrouter" {
		options = append(options, llms.WithJSONMode())
	}

	resp, err := provider.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", fmt.Errorf("generate skills: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("generate skills: no response")
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
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/workspace/...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/service.go
git commit -m "refactor: rewrite workspace service with langchaingo memory and streaming"
```

---

### Task 5: Rewrite Tracker Service

**Covers:** S3, S9

**Files:**
- Rewrite: `internal/tracker/service.go`

**Interfaces:**
- Consumes: `github.com/tmc/langchaingo/llms`
- Produces: `*Service` with `NaturalLanguageQuery`

- [ ] **Step 1: Write the new tracker service**

```go
package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"pi_dashboard/internal/llm"

	"github.com/tmc/langchaingo/llms"
)

type Service struct {
	repo    *Repository
	llm     llms.Model
	prompts *llm.Prompts
}

func NewService(repo *Repository, llmModel llms.Model, prompts *llm.Prompts) *Service {
	return &Service{
		repo:    repo,
		llm:     llmModel,
		prompts: prompts,
	}
}

func (s *Service) Upsert(ctx context.Context, app Application) (*UpsertResult, error) {
	result, err := s.repo.Upsert(ctx, app)
	if err != nil {
		return nil, err
	}

	if app.Count > 0 {
		activityDate := ""
		if app.AppliedDates != nil && strings.TrimSpace(*app.AppliedDates) != "" {
			activityDate = strings.TrimSpace(*app.AppliedDates)
		} else {
			activityDate = time.Now().In(time.Local).Format("2006-01-02")
		}
		action := "upsert"
		if app.Status != nil && strings.TrimSpace(*app.Status) != "" {
			action = strings.TrimSpace(*app.Status)
		}
		if err := s.repo.LogActivity(ctx, result.Organization, app.Count, activityDate, action); err != nil {
			slog.Warn("log tracker activity", "err", err)
		}
	}

	return result, nil
}

func (s *Service) Check(ctx context.Context, name string) (string, bool, int, string, *string, error) {
	return s.repo.CheckExists(ctx, name)
}

func (s *Service) Suggest(ctx context.Context, query string, limit int) ([]map[string]string, error) {
	return s.repo.Suggest(ctx, query, limit)
}

func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	now := time.Now().In(time.Local)
	today := now.Format("2006-01-02")
	weekStart := now.AddDate(0, 0, -int(now.Weekday())).Format("2006-01-02")
	return s.repo.Stats(ctx, today, weekStart)
}

func (s *Service) Timeline(ctx context.Context, days int, freq string) ([]TimelineEntry, error) {
	today := time.Now().In(time.Local).Format("2006-01-02")
	return s.repo.Timeline(ctx, days, freq, today)
}

func (s *Service) ContributionHeatmap(ctx context.Context, year int, month int) ([]ContributionDay, error) {
	return s.repo.ContributionHeatmap(ctx, year, month)
}

func (s *Service) DateRange(ctx context.Context) (string, string, error) {
	return s.repo.DateRange(ctx)
}

func (s *Service) LogActivity(ctx context.Context, org string, delta int, date string, action string) error {
	return s.repo.LogActivity(ctx, org, delta, date, action)
}

var sqlFenceRe = regexp.MustCompile("(?is)```(?:sql)?\\s*(.*?)```")
var writeKeywordRe = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|truncate|create|grant|revoke|copy|vacuum|call|reindex|refresh)\b`)

func (s *Service) NaturalLanguageQuery(ctx context.Context, nl string) (string, error) {
	schemaDoc := s.prompts.Get("sql_assistant")
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(schemaDoc)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(nl)}},
	}
	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("nl query: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("nl query: no response")
	}
	return extractSQL(resp.Choices[0].Content), nil
}

func extractSQL(raw string) string {
	s := strings.TrimSpace(raw)
	if m := sqlFenceRe.FindStringSubmatch(s); len(m) == 2 {
		s = strings.TrimSpace(m[1])
	}
	if idx := strings.Index(s, ";"); idx >= 0 {
		rest := strings.TrimSpace(s[idx+1:])
		if rest == "" {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.TrimSpace(s)
}

func ValidateReadOnlySQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return fmt.Errorf("empty query")
	}
	if idx := strings.Index(trimmed, ";"); idx >= 0 {
		rest := strings.TrimSpace(trimmed[idx+1:])
		if rest != "" {
			return fmt.Errorf("only single statement allowed")
		}
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return fmt.Errorf("only SELECT or WITH queries permitted")
	}
	if writeKeywordRe.MatchString(lower) {
		return fmt.Errorf("query contains disallowed keyword")
	}
	return nil
}

func (s *Service) ExecuteQuery(ctx context.Context, sql string) (*QueryResult, error) {
	rows, err := s.repo.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var out [][]any
	truncated := false
	maxRows := 500

	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i := range vals {
			vals[i] = normalizeValue(vals[i])
		}
		out = append(out, vals)
	}

	return &QueryResult{
		SQL:       sql,
		Columns:   columns,
		Rows:      out,
		RowCount:  len(out),
		Truncated: truncated,
	}, nil
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	default:
		return val
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/tracker/...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add internal/tracker/service.go
git commit -m "refactor: rewrite tracker service to use langchaingo llms.Model"
```

---

### Task 6: Update main.go

**Covers:** S3, S10

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: Updated `internal/llm.NewProvider`, `workspace.NewService`, `tracker.NewService`
- Produces: Application initializes with langchaingo providers

- [ ] **Step 1: Update imports and initialization**

Replace the LLM initialization section in `main.go`:

```go
// Old imports to remove:
// pkgllm "pi_dashboard/pkg/llm"

// New import to add:
"github.com/tmc/langchaingo/llms"

// Replace this section:
llmCfg := llm.Config{
    ProviderType:     cfg.LLMProvider,
    OllamaHost:       cfg.OllamaHost,
    OllamaModel:      cfg.OllamaModel,
    OpenRouterAPIKey: cfg.OpenRouterAPIKey,
    OpenRouterModel:  cfg.OpenRouterModel,
}
var llmProvider pkgllm.Provider
llmProvider, err = llm.NewProvider(llmCfg)
if err != nil {
    slog.Warn("llm provider", "err", err)
}

// With this:
llmCfg := llm.Config{
    ProviderType:     cfg.LLMProvider,
    OllamaHost:       cfg.OllamaHost,
    OllamaModel:      cfg.OllamaModel,
    OpenRouterAPIKey: cfg.OpenRouterAPIKey,
    OpenRouterModel:  cfg.OpenRouterModel,
}
var llmProvider llms.Model
llmProvider, err = llm.NewProvider(llmCfg)
if err != nil {
    slog.Warn("llm provider", "err", err)
}

// Update tracker service initialization:
trackerSvc := tracker.NewService(trackerRepo, llmProvider, prompts)

// Update workspace service initialization:
workspaceSvc := workspace.NewService(llmProvider, string(resumeText), string(resumeMarkdown), prompts)
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "refactor: update main.go to use langchaingo providers"
```

---

### Task 7: Delete Old Packages

**Covers:** S3

**Files:**
- Delete: `pkg/ollama/client.go`
- Delete: `pkg/openrouter/client.go`
- Delete: `pkg/llm/provider.go`

**Interfaces:**
- Consumes: (none)
- Produces: Old packages removed

- [ ] **Step 1: Delete old packages**

```bash
rm -rf pkg/ollama pkg/openrouter pkg/llm
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: Compiles (no references to deleted packages)

- [ ] **Step 3: Run tests**

Run: `go test ./... -v -count=1`
Expected: Tests pass

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: remove native LLM packages, fully migrated to langchaingo"
```

---

### Task 8: Update Tests

**Covers:** S3

**Files:**
- Modify: `internal/llm/service_test.go`

**Interfaces:**
- Consumes: Updated `internal/llm.Service`
- Produces: Tests pass with langchaingo

- [ ] **Step 1: Read existing tests**

Run: `cat internal/llm/service_test.go`

- [ ] **Step 2: Update tests to use langchaingo types**

Update any tests that reference `pkgllm.Message` or `pkgllm.Response` to use `llms.MessageContent` and `llms.ContentResponse`.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/llm/... -v -count=1`
Expected: Tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/llm/service_test.go
git commit -m "test: update LLM service tests for langchaingo"
```

---

### Task 9: Final Verification

**Covers:** All

**Files:**
- (none - verification only)

**Interfaces:**
- Consumes: All previous tasks
- Produces: Confirmed working build

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v -count=1`
Expected: Tests pass

- [ ] **Step 3: Cross-compile verification**

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null`
Expected: Compiles

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "chore: langchaingo migration complete"
```
