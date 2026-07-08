package llm

import (
	"context"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

type mockModel struct{}

func (m *mockModel) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return "mock generated", nil
}

func (m *mockModel) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: "mock response"},
		},
	}, nil
}

func TestNewService(t *testing.T) {
	svc := NewService(&mockModel{})
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestChat(t *testing.T) {
	svc := NewService(&mockModel{})
	resp, err := svc.Chat(context.Background(), []llms.MessageContent{
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart("hello")}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp != "mock response" {
		t.Errorf("result = %q, want %q", resp, "mock response")
	}
}

func TestGenerate(t *testing.T) {
	svc := NewService(&mockModel{})
	result, err := svc.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result != "mock generated" {
		t.Errorf("result = %q, want %q", result, "mock generated")
	}
}
