package ollama

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

type Config struct {
	Host  string
	Model string
}

type Client struct {
	host  string
	model string
	http  *http.Client
}

func New(cfg Config) *Client {
	host := strings.TrimRight(cfg.Host, "/")
	if !strings.HasPrefix(host, "http") {
		host = "http://" + host
	}
	return &Client{
		host:  host,
		model: cfg.Model,
		http:  &http.Client{Timeout: 60 * time.Second},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []llm.Message `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

func (c *Client) Chat(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	reqBody := chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("ollama read: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llm.Response{}, fmt.Errorf("ollama decode: %w", err)
	}

	return llm.Response{
		Content: parsed.Message.Content,
		Done:    parsed.Done,
	}, nil
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	reqBody := generateRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama do: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ollama read: %w", err)
	}

	var parsed generateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}

	return parsed.Response, nil
}