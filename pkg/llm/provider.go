package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content string `json:"content"`
	Done    bool   `json:"done"`
}

type Provider interface {
	Chat(ctx context.Context, messages []Message) (Response, error)
	ChatStream(ctx context.Context, messages []Message) (<-chan string, error)
	Generate(ctx context.Context, prompt string) (string, error)
}