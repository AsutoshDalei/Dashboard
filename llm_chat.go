package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ledongthuc/pdf"
)

const chatPromptsFile = "data/chat_prompts.json"
const chatTimeout = 60 * time.Second

const maxSessionHistory = 50
const maxSessions = 100
const maxMessageLen = 5000
const rateLimitPerMinute = 30

var resumeText string

func init() {
	loadResumeText()
	loadPrompts()
}

func loadResumeText() {
	paths := []string{
		"ASUTOSH_DALEI_RESUME.pdf",
		filepath.Join("..", "ASUTOSH_DALEI_RESUME.pdf"),
		filepath.Join("pi_bundle", "ASUTOSH_DALEI_RESUME.pdf"),
	}
	for _, p := range paths {
		text, err := extractPDFText(p)
		if err == nil {
			resumeText = text
			slog.Info("Resume loaded", "path", p, "length", len(text))
			return
		}
	}
	slog.Warn("No resume PDF found; chat will operate without resume context")
	resumeText = ""
}

func extractPDFText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}

	reader, err := pdf.NewReader(f, stat.Size())
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return strings.TrimSpace(buf.String()), nil
}

func buildSystemPrompt() string {
	prompt := `You are Humanizer, an expert writing editor built into a job application assistant. Your task is to draft short, human-sounding responses to application questions grounded in the user's resume below.

You MUST enforce all of the following rules on every single response without exception.

## CORE DIRECTIVES

1. Identify AI Patterns: Thoroughly scan your draft for the architectural, stylistic, and linguistic "tells" listed below. Eliminate every one.
2. Rewrite, Do Not Delete: Replace AI-isms with clean, natural alternatives. Cover everything the original would cover.
3. Preserve Intent & Meaning: Keep the core message, factual data, and semantic intent completely intact.
4. Output Format: Do NOT output a Draft/Audit/Final/Summary. Directly output only the final humanized response. Keep it to 2-3 short paragraphs unless asked for more.

## VOICE CALIBRATION

- Write as if you are the user speaking naturally in a conversation. Use contractions. Vary sentence length. Keep the tone warm and direct.
- React to facts instead of neutrally reporting them. Have subtle opinions.
- Mix short sharp statements with longer explanatory clauses.
- Include slight natural imperfections — avoid perfectly symmetrical algorithmic paragraph structures.

## PATTERNS TO ELIMINATE

### A. Content & Structural
1. No inflation of significance: Never use "stands as", "serves as", "a testament to", "vital/crucial/pivotal role", "underscores/highlights importance", "setting the stage", "key turning point", "evolving landscape", "focal point", "indelible mark", "deeply rooted".
2. No "-ing" participial clauses tacked onto sentences: Never end sentences with "highlighting...", "underscoring...", "emphasizing...", "ensuring...", "reflecting...", "fostering...", "showcasing...".
3. No promotional register: Never use "boasts", "vibrant", "rich", "profound", "groundbreaking", "renowned", "breathtaking", "must-visit", "stunning".
4. No formulaic "Despite X, the future looks bright" closings.

### B. Language & Grammar
5. BANNED WORDS (zero tolerance): Actually, additionally, align with, crucial, delve, emphasizing, enduring, enhance, fostering, garner, highlight (as verb), interplay, intricate, intricacies, key (as adjective), landscape (abstract noun), pivotal, showcase, tapestry (abstract noun), testament, underscore (as verb), valuable, vibrant.
6. No copula avoidance: Do not replace simple "is/are" with "serves as", "stands as", "represents", "boasts", "features", "offers".
7. No "Not only X, but also Y" constructions.
8. No Rule of Three: Never mechanically group ideas into neat triplets.
9. No elegant variation: Do not cycle synonyms for the same noun. If a word fits, repeat it naturally.
10. No false ranges: Never use "from X to Y" where X and Y are not logical endpoints.
11. No subjectless passive fragments: Never write "No configuration needed" — write "You don't need configuration".

### C. Style & Typography
12. HARD BAN ON EM DASHES (—) AND EN DASHES (–): Zero tolerance. Replace with periods, commas, colons, or restructure the sentence.
13. No mechanical boldface mid-sentence.
14. No emojis in headings or text.
15. No title case in descriptions. Use sentence case.

### D. Communication Artifacts
16. No conversational filler: Never open with "Certainly!", "Great question!", "I hope this helps!", "Let's dive in", or any chatbot bookends.
17. No cutoff disclaimers: Never write "maintains a low profile", "prefers to stay out of the spotlight", "it is believed that".
18. No servile/sycophantic tone: Do not over-praise the user's input or act overly enthusiastic.

### E. Pacing & Padding
19. Compress wordy fillers:
    - "In order to" → "To"
    - "Due to the fact that" → "Because"
    - "At this point in time" → "Now"
    - "In the event that" → "If"
    - "has the ability to" → "can"
20. No excessive hedging: No "it could potentially possibly be argued that".
21. No generic upbeat conclusions.
22. No persuasive authority tropes: No "at its core", "in reality", "what really matters", "the heart of the matter".
23. No signposting: Do not announce what you are about to say. Just say it.
24. No fragmented heading echoes: No generic transition sentence directly under a heading.

## DETECTION SANITY CHECKS

Do not strip good writing just because it's clean. Preserve:
- Complex specific real-world details.
- Expressed ambivalence or mixed feelings.
- Varying sentence lengths.
- Genuine personal asides.

`

	if resumeText != "" {
		prompt += "\n=== USER'S RESUME ===\n"
		prompt += resumeText
		prompt += "\n=== END RESUME ===\n\n"
	}

	prompt += `Use the resume above to tailor every answer. Reference specific experiences, companies, and skills from it. If the user asks a question that cannot be answered from the resume, politely say so and suggest rephrasing. Keep responses to 2–3 short paragraphs.`
	return prompt
}

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
	Messages   []ChatMessage
	lastAccess time.Time
	rateCount  int
	rateReset  time.Time
}

var (
	promptMu    sync.RWMutex
	promptStore = &PromptStore{Prompts: []DefaultPrompt{}}

	sessionMu sync.RWMutex
	sessions  = make(map[string]*chatSession)
)

func loadPrompts() {
	promptMu.Lock()
	defer promptMu.Unlock()

	data, err := os.ReadFile(chatPromptsFile)
	if err != nil {
		if os.IsNotExist(err) {
			promptStore.Prompts = []DefaultPrompt{}
			return
		}
		slog.Warn("Error reading prompts file", "err", err)
		return
	}

	if err := json.Unmarshal(data, promptStore); err != nil {
		slog.Warn("Error parsing prompts file", "err", err)
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

func getSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func deleteChatSession(token string) {
	sessionMu.Lock()
	delete(sessions, token)
	sessionMu.Unlock()
}

func pruneChatSessions(validTokens map[string]bool) {
	sessionMu.Lock()
	for tok := range sessions {
		if !validTokens[tok] {
			delete(sessions, tok)
		}
	}
	sessionMu.Unlock()
}

func checkRateLimit(s *chatSession) bool {
	now := time.Now()
	if now.After(s.rateReset) {
		s.rateCount = 0
		s.rateReset = now.Add(time.Minute)
	}
	s.rateCount++
	return s.rateCount <= rateLimitPerMinute
}

func handleChatTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "chat.html", nil); err != nil {
		slog.Error("Template rendering error", "err", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func handleChatSkill(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"skill": SkillInfo{
			Name:        "Application Assistant",
			Description: "Drafts short, human-sounding job application responses grounded in your resume.",
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
	json.NewEncoder(w).Encode(map[string]any{
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
		slog.Error("Error saving prompts", "err", err)
		respondJSON(w, http.StatusInternalServerError, false, "Failed to save prompt", "")
		return
	}
	promptMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
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
		slog.Error("Error saving prompts", "err", err)
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

	sessionID := getSessionToken(r)
	if sessionID == "" {
		respondJSON(w, http.StatusBadRequest, false, "Not authenticated", "")
		return
	}
	sessionMu.Lock()
	sessions[sessionID] = &chatSession{Messages: []ChatMessage{}, lastAccess: time.Now()}
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
	if len(req.Message) > maxMessageLen {
		respondJSON(w, http.StatusBadRequest, false, fmt.Sprintf("Message too long (max %d characters)", maxMessageLen), "")
		return
	}

	sessionID := getSessionToken(r)
	if sessionID == "" {
		respondJSON(w, http.StatusBadRequest, false, "Not authenticated", "")
		return
	}

	sessionMu.Lock()
	s, ok := sessions[sessionID]
	if !ok {
		if len(sessions) >= maxSessions {
			sessionMu.Unlock()
			respondJSON(w, http.StatusServiceUnavailable, false, "Too many active sessions. Clear your chat and try again.", "")
			return
		}
		s = &chatSession{Messages: []ChatMessage{}, lastAccess: time.Now()}
		sessions[sessionID] = s
	}
	s.lastAccess = time.Now()

	if !checkRateLimit(s) {
		sessionMu.Unlock()
		respondJSON(w, http.StatusTooManyRequests, false, "Rate limit exceeded. Please wait before sending another message.", "")
		return
	}

	history := make([]ChatMessage, 0, len(s.Messages))
	if len(s.Messages) > maxSessionHistory {
		start := len(s.Messages) - maxSessionHistory
		history = append(history, s.Messages[start:]...)
	} else {
		history = append(history, s.Messages...)
	}
	s.Messages = append(s.Messages, ChatMessage{Role: "user", Content: req.Message})
	sessionMu.Unlock()

	systemPrompt := buildSystemPrompt()

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
	}
	messages = append(messages, history...)
	messages = append(messages, ChatMessage{Role: "user", Content: req.Message})

	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		respondJSON(w, http.StatusInternalServerError, false, "OpenRouter is not configured. Set OPENROUTER_API_KEY.", "")
		return
	}

	models := resolveChatModels()
	if len(models) == 0 {
		respondJSON(w, http.StatusInternalServerError, false, "OpenRouter is not configured. Set OPENROUTER_CHAT_MODEL or OPENROUTER_MODEL.", "")
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
		select {
		case <-r.Context().Done():
			return
		default:
		}

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
		slog.Warn("Error reading stream", "err", err)
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
}

func resolveChatModels() []string {
	return resolveModels("OPENROUTER_CHAT_MODEL", "OPENROUTER_MODEL")
}

func toOpenRouterMessages(src []ChatMessage) []openRouterMessage {
	out := make([]openRouterMessage, len(src))
	for i, m := range src {
		out[i] = openRouterMessage{Role: m.Role, Content: m.Content}
	}
	return out
}