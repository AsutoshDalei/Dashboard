package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"pi_dashboard/pkg/llm"
	"pi_dashboard/pkg/ollama"
	"pi_dashboard/pkg/openrouter"
)

type Config struct {
	ProviderType     string
	OllamaHost       string
	OllamaModel      string
	OpenRouterAPIKey string
	OpenRouterModel  string
}

// fallbackProvider tries each client in order until one succeeds.
type fallbackProvider struct {
	clients []llm.Provider
}

func (f *fallbackProvider) Chat(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	var lastErr error
	for i, c := range f.clients {
		resp, err := c.Chat(ctx, messages)
		if err == nil {
			return resp, nil
		}
		slog.Warn("openrouter model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return llm.Response{}, lastErr
}

func (f *fallbackProvider) ChatStream(ctx context.Context, messages []llm.Message) (<-chan string, error) {
	var lastErr error
	for i, c := range f.clients {
		ch, err := c.ChatStream(ctx, messages)
		if err == nil {
			return ch, nil
		}
		slog.Warn("openrouter stream model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return nil, lastErr
}

func (f *fallbackProvider) ChatWithSchema(ctx context.Context, messages []llm.Message, schema any) (llm.Response, error) {
	var lastErr error
	for i, c := range f.clients {
		resp, err := c.ChatWithSchema(ctx, messages, schema)
		if err == nil {
			return resp, nil
		}
		slog.Warn("openrouter schema model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return llm.Response{}, lastErr
}

func (f *fallbackProvider) Generate(ctx context.Context, prompt string) (string, error) {
	var lastErr error
	for i, c := range f.clients {
		resp, err := c.Generate(ctx, prompt)
		if err == nil {
			return resp, nil
		}
		slog.Warn("openrouter generate model failed, trying next", "index", i, "err", err)
		lastErr = err
	}
	return "", lastErr
}

func NewProvider(cfg Config) (llm.Provider, error) {
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
		return ollama.New(ollama.Config{
			Host:  cfg.OllamaHost,
			Model: cfg.OllamaModel,
		}), nil

	case "openrouter":
		if cfg.OpenRouterAPIKey == "" {
			return nil, fmt.Errorf("OPENROUTER_API_KEY required")
		}
		if cfg.OpenRouterModel == "" {
			return nil, fmt.Errorf("OPENROUTER_MODEL required")
		}
		models := openrouter.ResolveModels(cfg.OpenRouterModel, "")
		if len(models) == 0 {
			return nil, fmt.Errorf("OPENROUTER_MODEL invalid")
		}
		clients := make([]llm.Provider, 0, len(models))
		for _, m := range models {
			clients = append(clients, openrouter.New(openrouter.Config{
				APIKey: cfg.OpenRouterAPIKey,
				Model:  m,
			}))
		}
		return &fallbackProvider{clients: clients}, nil

	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.ProviderType)
	}
}