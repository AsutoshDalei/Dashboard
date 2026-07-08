package llm

import (
	"context"
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
