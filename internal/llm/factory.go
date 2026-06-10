package llm

import (
	"fmt"

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

func NewProvider(cfg Config) (llm.Provider, error) {
	switch cfg.ProviderType {
	case "ollama":
		if cfg.OllamaHost == "" {
			cfg.OllamaHost = "172.16.7.115"
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
		return openrouter.New(openrouter.Config{
			APIKey: cfg.OpenRouterAPIKey,
			Model:  cfg.OpenRouterModel,
		}), nil

	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.ProviderType)
	}
}