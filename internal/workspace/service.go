package workspace

import (
	"context"
	"fmt"
	"sync"
	"time"

	"pi_dashboard/pkg/llm"
)

type Session struct {
	ID        string
	Messages  []llm.Message
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
		Messages:  []llm.Message{},
		CreatedAt: time.Now(),
	}
	s.sessions[sessionID] = sess
	return sess
}

func (s *ChatStore) AddMessage(sessionID string, msg llm.Message) {
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
	provider   llm.Provider
	chatStore  *ChatStore
	resumeText string
}

func NewService(provider llm.Provider, resumeText string) *Service {
	return &Service{
		provider:   provider,
		chatStore:  NewChatStore(),
		resumeText: resumeText,
	}
}

func (s *Service) Chat(ctx context.Context, sessionID string, message string, systemPrompt string) (string, error) {
	session := s.chatStore.GetOrCreate(sessionID)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, session.Messages...)
	messages = append(messages, llm.Message{Role: "user", Content: message})

	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("chat: %w", err)
	}

	s.chatStore.AddMessage(sessionID, llm.Message{Role: "user", Content: message})
	s.chatStore.AddMessage(sessionID, llm.Message{Role: "assistant", Content: resp.Content})

	return resp.Content, nil
}

func (s *Service) ClearChat(sessionID string) {
	s.chatStore.Clear(sessionID)
}

func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string) (string, error) {
	messages := []llm.Message{
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
	messages := []llm.Message{
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
	messages := []llm.Message{
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
	prompt := fmt.Sprintf("Draft a professional email expressing interest in ML Engineer and Applied Scientist roles at %s. Address it to %s.", company, name)
	resp, err := s.provider.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("email draft: %w", err)
	}
	return resp, nil
}

func (s *Service) DraftCoverLetter(ctx context.Context, company string) (string, error) {
	prompt := fmt.Sprintf("Write a professional cover letter for %s.\n\nResume context:\n%s", company, s.resumeText)
	resp, err := s.provider.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("cover letter draft: %w", err)
	}
	return resp, nil
}

func (s *Service) AnalyzeJobMatch(ctx context.Context, jobDescription string) (string, error) {
	prompt := fmt.Sprintf("Analyze how well the following resume matches this job description. Provide a match score, matching skills, missing skills, and recommendations.\n\nResume:\n%s\n\nJob Description:\n%s", s.resumeText, jobDescription)
	resp, err := s.provider.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("job match: %w", err)
	}
	return resp, nil
}