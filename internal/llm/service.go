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
