package llm

import (
	"context"
	"testing"

	"pi_dashboard/pkg/llm"
)

type mockProvider struct{}

func (m *mockProvider) Chat(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	return llm.Response{Content: "mock response", Done: true}, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, messages []llm.Message) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *mockProvider) Generate(ctx context.Context, prompt string) (string, error) {
	return "mock generated", nil
}

func TestNewService(t *testing.T) {
	svc := NewService(&mockProvider{})
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestChat(t *testing.T) {
	svc := NewService(&mockProvider{})
	resp, err := svc.Chat(context.Background(), []llm.Message{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "mock response" {
		t.Errorf("Content = %q, want %q", resp.Content, "mock response")
	}
}

func TestGenerate(t *testing.T) {
	svc := NewService(&mockProvider{})
	result, err := svc.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if result != "mock generated" {
		t.Errorf("result = %q, want %q", result, "mock generated")
	}
}