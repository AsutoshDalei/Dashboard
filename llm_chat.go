package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"
)

//go:embed humanizer.txt
var humanizerPrompt string

const chatPromptsFile = "data/chat_prompts.json"
const chatTimeout = 60 * time.Second

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type DefaultPrompt struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type PromptStore struct {
	Prompts []DefaultPrompt `json:"prompts"`
}

type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ChatSendRequest struct {
	Message string `json:"message"`
}

type chatSession struct {
	Messages []ChatMessage
}

var (
	promptMu    sync.RWMutex
	promptStore = &PromptStore{Prompts: []DefaultPrompt{}}

	sessionMu sync.RWMutex
	sessions  = make(map[string]*chatSession)
)

func init() {
	loadPrompts()
}

func loadPrompts() {
	promptMu.Lock()
	defer promptMu.Unlock()

	data, err := os.ReadFile(chatPromptsFile)
	if err != nil {
		if os.IsNotExist(err) {
			promptStore.Prompts = []DefaultPrompt{}
			return
		}
		log.Printf("Error reading prompts file: %v", err)
		return
	}

	if err := json.Unmarshal(data, promptStore); err != nil {
		log.Printf("Error parsing prompts file: %v", err)
		promptStore.Prompts = []DefaultPrompt{}
	}
}

func savePrompts() error {
	data, err := json.MarshalIndent(promptStore, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling prompts: %w", err)
	}

	dir := filepath.Dir(chatPromptsFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("error creating data dir: %w", err)
	}

	tmp := chatPromptsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("error writing prompts temp file: %w", err)
	}
	if err := os.Rename(tmp, chatPromptsFile); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("error replacing prompts file: %w", err)
	}
	return nil
}

func generateChatID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

func getOrCreateSession(r *http.Request) string {
	cookie, _ := r.Cookie("chat_session_id")
	if cookie != nil && cookie.Value != "" {
		sessionMu.RLock()
		_, exists := sessions[cookie.Value]
		sessionMu.RUnlock()
		if exists {
			return cookie.Value
		}
	}
	id := generateChatID()
	sessionMu.Lock()
	sessions[id] = &chatSession{Messages: []ChatMessage{}}
	sessionMu.Unlock()
	return id
}

func handleChatTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "chat.html", nil); err != nil {
		log.Printf("Template rendering error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleChatSkill(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"skill": SkillInfo{
			Name:        "Humanizer",
			Description: "Expert writing editor that removes AI patterns and makes text sound naturally human-written.",
		},
	})
}

func handleChatPrompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	promptMu.RLock()
	defer promptMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"prompts": promptStore.Prompts,
	})
}

type addPromptRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func handleChatPromptsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req addPromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	if req.Title == "" || req.Content == "" {
		respondJSON(w, http.StatusBadRequest, false, "Title and content are required", "")
		return
	}

	promptMu.Lock()
	p := DefaultPrompt{
		ID:      generateChatID(),
		Title:   req.Title,
		Content: req.Content,
	}
	promptStore.Prompts = append(promptStore.Prompts, p)
	if err := savePrompts(); err != nil {
		promptMu.Unlock()
		log.Printf("Error saving prompts: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, "Failed to save prompt", "")
		return
	}
	promptMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"prompt":  p,
	})
}

func handleChatPromptsDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		respondJSON(w, http.StatusBadRequest, false, "Prompt ID required", "")
		return
	}

	promptMu.Lock()
	found := false
	newPrompts := make([]DefaultPrompt, 0, len(promptStore.Prompts))
	for _, p := range promptStore.Prompts {
		if p.ID == id {
			found = true
			continue
		}
		newPrompts = append(newPrompts, p)
	}
	if !found {
		promptMu.Unlock()
		respondJSON(w, http.StatusNotFound, false, "Prompt not found", "")
		return
	}
	promptStore.Prompts = newPrompts
	if err := savePrompts(); err != nil {
		promptMu.Unlock()
		log.Printf("Error saving prompts: %v", err)
		respondJSON(w, http.StatusInternalServerError, false, "Failed to delete prompt", "")
		return
	}
	promptMu.Unlock()

	respondJSON(w, http.StatusOK, true, "", "Prompt deleted successfully")
}

func handleChatClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := getOrCreateSession(r)
	sessionMu.Lock()
	sessions[sessionID] = &chatSession{Messages: []ChatMessage{}}
	sessionMu.Unlock()

	respondJSON(w, http.StatusOK, true, "", "Chat cleared")
}

func handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		respondJSON(w, http.StatusBadRequest, false, "Message is required", "")
		return
	}

	sessionID := getOrCreateSession(r)

	sessionMu.Lock()
	s, ok := sessions[sessionID]
	if !ok {
		s = &chatSession{Messages: []ChatMessage{}}
		sessions[sessionID] = s
	}

	history := make([]ChatMessage, len(s.Messages))
	copy(history, s.Messages)
	s.Messages = append(s.Messages, ChatMessage{Role: "user", Content: req.Message})
	sessionMu.Unlock()

	messages := []ChatMessage{
		{Role: "system", Content: humanizerPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, ChatMessage{Role: "user", Content: req.Message})

	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		respondJSON(w, http.StatusBadGateway, false, "OpenRouter is not configured. Set OPENROUTER_API_KEY.", "")
		return
	}

	models := resolveChatModels()
	if len(models) == 0 {
		respondJSON(w, http.StatusBadGateway, false, "OpenRouter is not configured. Set OPENROUTER_CHAT_MODEL or OPENROUTER_MODEL.", "")
		return
	}

	orReq := openRouterRequest{
		Model:    models[0],
		Messages: toOpenRouterMessages(messages),
		Stream:   true,
	}
	if len(models) > 1 {
		orReq.Models = models
	}

	payload, err := json.Marshal(orReq)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, false, "Failed to encode request", "")
		return
	}

	orHTTPReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, openRouterEndpoint, bytes.NewReader(payload))
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, false, "Failed to build request", "")
		return
	}
	orHTTPReq.Header.Set("Authorization", "Bearer "+apiKey)
	orHTTPReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: chatTimeout}
	resp, err := client.Do(orHTTPReq)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, false, "OpenRouter request failed: "+err.Error(), "")
		return
	}
	defer resp.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondJSON(w, http.StatusInternalServerError, false, "Streaming not supported", "")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var fullContent strings.Builder
	var inThinking bool
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue
		}

for _, choice := range streamResp.Choices {
			content := choice.Delta.Content
			if content == "" {
				continue
			}

			if inThinking {
				closeIdx := strings.Index(content, "</thinking>")
				if closeIdx >= 0 {
					inThinking = false
					content = content[closeIdx+len("</thinking>"):]
				} else {
					continue
				}
			}

			openIdx := strings.Index(content, "<thinking>")
			if openIdx >= 0 {
				before := content[:openIdx]
				after := content[openIdx+len("<thinking>"):]
				content = before
				inThinking = true
				closeIdx := strings.Index(after, "</thinking>")
				if closeIdx >= 0 {
					content += after[closeIdx+len("</thinking>"):]
					inThinking = false
				}
			}

			if content == "" {
				continue
			}

			fullContent.WriteString(content)

			eventData, _ := json.Marshal(map[string]string{"token": content})
			fmt.Fprintf(w, "data: %s\n\n", eventData)
			flusher.Flush()

			if choice.FinishReason != nil {
				break
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading stream: %v", err)
	}

	assistantContent := fullContent.String()
	if assistantContent != "" {
		sessionMu.Lock()
		if s, ok := sessions[sessionID]; ok {
			s.Messages = append(s.Messages, ChatMessage{Role: "assistant", Content: assistantContent})
		}
		sessionMu.Unlock()
	}

	eventData, _ := json.Marshal(map[string]string{"done": assistantContent})
	fmt.Fprintf(w, "data: %s\n\n", eventData)
	flusher.Flush()

	http.SetCookie(w, &http.Cookie{
		Name:     "chat_session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400,
		SameSite: http.SameSiteLaxMode,
	})
}

func resolveChatModels() []string {
	raw := strings.TrimSpace(os.Getenv("OPENROUTER_CHAT_MODEL"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
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

func toOpenRouterMessages(src []ChatMessage) []openRouterMessage {
	out := make([]openRouterMessage, len(src))
	for i, m := range src {
		out[i] = openRouterMessage{Role: m.Role, Content: m.Content}
	}
	return out
}