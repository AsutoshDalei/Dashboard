package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
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
		http:   &http.Client{Timeout: 300 * time.Second},
	}
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []llm.Message `json:"messages"`
	Stream         bool          `json:"stream"`
	ResponseFormat any           `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta *struct {
			Content string `json:"content"`
		} `json:"delta,omitempty"`
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

	slog.Debug("openrouter request", "model", c.model, "payload_size", len(payload))

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
		slog.Error("openrouter error response", "status", resp.StatusCode, "body", string(body))
		if parsed.Error != nil && parsed.Error.Message != "" {
			return llm.Response{}, fmt.Errorf("openrouter error (status %d): %s", resp.StatusCode, parsed.Error.Message)
		}
		return llm.Response{}, fmt.Errorf("openrouter status %d: %s", resp.StatusCode, string(body))
	}

	if len(parsed.Choices) == 0 {
		slog.Error("openrouter no choices", "body", string(body))
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

func (c *Client) ChatWithSchema(ctx context.Context, messages []llm.Message, schema any) (llm.Response, error) {
	reqBody := chatRequest{
		Model:          c.model,
		Messages:       messages,
		Stream:         false,
		ResponseFormat: schema,
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

func (c *Client) ChatStream(ctx context.Context, messages []llm.Message) (<-chan string, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter do: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var parsed chatResponse
		if json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Message != "" {
			return nil, fmt.Errorf("openrouter error: %s", parsed.Error.Message)
		}
		return nil, fmt.Errorf("openrouter status %d", resp.StatusCode)
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var chunk chatResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta
			if delta != nil && delta.Content != "" {
				ch <- delta.Content
			}
		}
	}()

	return ch, nil
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