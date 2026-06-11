package workspace

import (
	"context"
	"fmt"
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
	provider   pkgllm.Provider
	chatStore  *ChatStore
	resumeText string
	prompts    *llm.Prompts
}

func NewService(provider pkgllm.Provider, resumeText string, prompts *llm.Prompts) *Service {
	return &Service{
		provider:   provider,
		chatStore:  NewChatStore(),
		resumeText: resumeText,
		prompts:    prompts,
	}
}

func (s *Service) Chat(ctx context.Context, sessionID string, message string, systemPrompt string) (string, error) {
	session := s.chatStore.GetOrCreate(sessionID)

	messages := []pkgllm.Message{
		{Role: "system", Content: systemPrompt},
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

	messages := []pkgllm.Message{
		{Role: "system", Content: systemPrompt},
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