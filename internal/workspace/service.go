package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	llm "pi_dashboard/internal/llm"
	pkgllm "pi_dashboard/pkg/llm"
)

type Session struct {
	ID        string
	Messages  []pkgllm.Message
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

func (s *ChatStore) GetOrCreate(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		return sess
	}
	sess := &Session{
		ID:        sessionID,
		Messages:  []pkgllm.Message{},
		CreatedAt: time.Now(),
	}
	s.sessions[sessionID] = sess
	return sess
}

func (s *ChatStore) AddMessage(sessionID string, msg pkgllm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.Messages = append(sess.Messages, msg)
	}
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
	provider       pkgllm.Provider
	chatStore      *ChatStore
	resumeText     string
	resumeMarkdown string
	prompts        *llm.Prompts
}

func NewService(provider pkgllm.Provider, resumeText string, resumeMarkdown string, prompts *llm.Prompts) *Service {
	return &Service{
		provider:       provider,
		chatStore:      NewChatStore(),
		resumeText:     resumeText,
		resumeMarkdown: resumeMarkdown,
		prompts:        prompts,
	}
}

func (s *Service) Chat(ctx context.Context, sessionID string, message string, systemPrompt string) (string, error) {
	session := s.chatStore.GetOrCreate(sessionID)

	fullSystemPrompt := systemPrompt
	if s.resumeMarkdown != "" {
		fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
	}

	messages := []pkgllm.Message{
		{Role: "system", Content: fullSystemPrompt},
	}
	messages = append(messages, session.Messages...)
	messages = append(messages, pkgllm.Message{Role: "user", Content: message})

	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}

	s.chatStore.AddMessage(sessionID, pkgllm.Message{Role: "user", Content: message})
	s.chatStore.AddMessage(sessionID, pkgllm.Message{Role: "assistant", Content: resp.Content})

	return resp.Content, nil
}

func (s *Service) ChatStream(ctx context.Context, sessionID string, message string, systemPrompt string) (<-chan string, error) {
	session := s.chatStore.GetOrCreate(sessionID)

	fullSystemPrompt := systemPrompt
	if s.resumeMarkdown != "" {
		fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
	}

	messages := []pkgllm.Message{
		{Role: "system", Content: fullSystemPrompt},
	}
	messages = append(messages, session.Messages...)
	messages = append(messages, pkgllm.Message{Role: "user", Content: message})

	ch, err := s.provider.ChatStream(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("chat stream: %w", err)
	}

	wrapped := make(chan string)
	go func() {
		defer close(wrapped)
		var full string
		for chunk := range ch {
			full += chunk
			wrapped <- chunk
		}
		s.chatStore.AddMessage(sessionID, pkgllm.Message{Role: "user", Content: message})
		s.chatStore.AddMessage(sessionID, pkgllm.Message{Role: "assistant", Content: full})
	}()

	return wrapped, nil
}

func (s *Service) ClearChat(sessionID string) {
	s.chatStore.Clear(sessionID)
}

func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string) (string, error) {
	messages := []pkgllm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Job Description:\n%s\n\nResume:\n%s", jobDescription, s.resumeText)},
	}
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("resume analyze: %w", err)
	}
	return resp.Content, nil
}

func (s *Service) GenerateResume(ctx context.Context, jobDescription string, analysis string, systemPrompt string) (string, error) {
	messages := []pkgllm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Job Description: %s\n\nAnalysis: %s\n\nResume: %s", jobDescription, analysis, s.resumeText)},
	}
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("resume generate: %w", err)
	}
	return resp.Content, nil
}

func (s *Service) GenerateSQL(ctx context.Context, naturalLanguage string, schemaDoc string) (string, error) {
	messages := []pkgllm.Message{
		{Role: "system", Content: schemaDoc},
		{Role: "user", Content: naturalLanguage},
	}
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("sql: %w", err)
	}
	return resp.Content, nil
}

func (s *Service) DraftEmail(ctx context.Context, name, company string) (string, error) {
	template := s.prompts.Format("email_draft", map[string]string{"company": company, "name": name})
	resp, err := s.provider.Generate(ctx, template)
	if err != nil {
		return "", fmt.Errorf("email draft: %w", err)
	}
	return resp, nil
}

func (s *Service) DraftCoverLetter(ctx context.Context, company string) (string, error) {
	template := s.prompts.Format("coverletter_draft", map[string]string{"company": company})
	prompt := fmt.Sprintf("%s\n\nResume context:\n%s", template, s.resumeText)
	resp, err := s.provider.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("cover letter draft: %w", err)
	}
	return resp, nil
}

func (s *Service) AnalyzeJobMatch(ctx context.Context, jobDescription string) (string, error) {
	prompt := fmt.Sprintf("%s\n\nResume:\n%s\n\nJob Description:\n%s", s.prompts.Get("job_match"), s.resumeText, jobDescription)
	resp, err := s.provider.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("job match: %w", err)
	}
	return resp, nil
}

type SkillsResponse struct {
	Skills []SkillCategory `json:"skills"`
}

type SkillCategory struct {
	Category string `json:"category"`
	Items    string `json:"items"`
}

func (s *Service) GenerateSkills(ctx context.Context, jobDescription string) (string, error) {
	currentSkills := extractSkills(s.resumeText)

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

	messages := []pkgllm.Message{
		{Role: "system", Content: s.prompts.Get("resume_generate")},
		{Role: "user", Content: fmt.Sprintf("Job Description:\n%s\n\nCurrent Skills Section:\n%s\n\nUpdate the skills to better match the job description. Add relevant keywords that are genuinely applicable. Remove irrelevant skills. Return the complete updated skills list.", jobDescription, currentSkills)},
	}

	resp, err := s.provider.ChatWithSchema(ctx, messages, schema)
	if err != nil {
		return "", fmt.Errorf("generate skills: %w", err)
	}

	return rebuildResume(s.resumeText, resp.Content)
}

func extractSkills(resume string) string {
	re := regexp.MustCompile(`(?s)\\section\*\{TECHNICAL SKILLS\}(.*?)(?:\\section\*|$)`)
	matches := re.FindStringSubmatch(resume)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func rebuildResume(resume string, skillsJSON string) (string, error) {
	var result SkillsResponse
	if err := json.Unmarshal([]byte(skillsJSON), &result); err != nil {
		return "", fmt.Errorf("parse skills JSON: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("\\begin{itemize}\n")
	for _, cat := range result.Skills {
		sb.WriteString(fmt.Sprintf("    \\item \\textbf{%s:} %s.\n", cat.Category, cat.Items))
	}
	sb.WriteString("\\end{itemize}")

	newSkills := sb.String()

	re := regexp.MustCompile(`(?s)(\\section\*\{TECHNICAL SKILLS\})\s*.*?(\\section\*|$)`)
	updated := re.ReplaceAllString(resume, fmt.Sprintf("${1}\n%s\n\n${2}", newSkills))

	return updated, nil
}