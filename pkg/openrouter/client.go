package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pi_dashboard/pkg/llm"
)

const endpoint = "https://openrouter.ai/api/v1/chat/completions"

type Config struct {
	APIKey string
	Model  string
}

type Client struct {
	apiKey string
	model  string
	http   *http.Client
}

func New(cfg Config) *Client {
	return &Client{
		apiKey: cfg.APIKey,
		model:  cfg.Model,
		http:   &http.Client{Timeout: 60 * time.Second},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) Chat(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Response{}, fmt.Errorf("openrouter marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, fmt.Errorf("openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return llm.Response{}, fmt.Errorf("openrouter do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("openrouter read: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("openrouter decode: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return llm.Response{}, fmt.Errorf("openrouter error: %s", parsed.Error.Message)
		}
		return llm.Response{}, fmt.Errorf("openrouter status %d", resp.StatusCode)
	}

	if len(parsed.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("openrouter no choices")
	}

	return llm.Response{
		Content: parsed.Choices[0].Message.Content,
		Done:    true,
	}, nil
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	resp, err := c.Chat(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func ResolveModels(primaryEnv, fallbackEnv string) []string {
	raw := strings.TrimSpace(primaryEnv)
	if raw == "" {
		raw = strings.TrimSpace(fallbackEnv)
	}
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