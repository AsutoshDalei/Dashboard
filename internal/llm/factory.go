package llm

import (
	"fmt"
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
		return openrouter.New(openrouter.Config{
			APIKey: cfg.OpenRouterAPIKey,
			Model:  models[0],
		}), nil

	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", cfg.ProviderType)
	}
}