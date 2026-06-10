package llm

import (
	"context"
	"fmt"

	"pi_dashboard/pkg/llm"
)

type Service struct {
	provider llm.Provider
}

func NewService(provider llm.Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) Chat(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	return s.provider.Chat(ctx, messages)
}

func (s *Service) Generate(ctx context.Context, prompt string) (string, error) {
	return s.provider.Generate(ctx, prompt)
}

func (s *Service) GenerateSQL(ctx context.Context, naturalLanguage string, schemaDoc string) (string, error) {
	messages := []llm.Message{
		{Role: "system", Content: schemaDoc},
		{Role: "user", Content: naturalLanguage},
	}
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("sql generation: %w", err)
	}
	return resp.Content, nil
}

func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string) (string, error) {
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: jobDescription},
	}
	resp, err := s.provider.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("resume analysis: %w", err)
	}
	return resp.Content, nil
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