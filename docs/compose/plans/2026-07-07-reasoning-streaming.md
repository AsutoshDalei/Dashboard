# Reasoning + Streaming Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable streaming chat with reasoning support in the UI, with thinking happening behind the scenes.

**Architecture:** Replace async job polling with SSE streaming. Add langchaingo reasoning options for chat, resume analysis, and resume generation.

**Tech Stack:** Go, langchaingo, JavaScript EventSource API

## Global Constraints

- Reasoning content logged via slog.Debug, never sent to frontend
- Thinking indicator shown during reasoning phase
- Streaming tokens appended to message div in real-time
- Must work with both Ollama and OpenRouter providers

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/workspace/service.go` | Add reasoning support to Chat and ChatStream |
| Modify | `internal/workspace/handler.go` | Add thinking SSE events |
| Modify | `templates/chat.html` | Replace async polling with EventSource streaming |

---

### Task 1: Add Reasoning to Workspace Service

**Covers:** S3, S5

**Files:**
- Modify: `internal/workspace/service.go`

**Interfaces:**
- Consumes: `github.com/tmc/langchaingo/llms`
- Produces: Updated `Chat` and `ChatStream` with reasoning options

- [ ] **Step 1: Update Chat method to include reasoning**

Add reasoning options to the `Chat` method. The thinking content will be captured but not returned.

```go
func (s *Service) Chat(ctx context.Context, sessionID string, message string, systemPrompt string) (string, error) {
    session := s.chatStore.GetOrCreate(sessionID, s.llm, systemPrompt)

    fullSystemPrompt := systemPrompt
    if s.resumeMarkdown != "" {
        fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
    }

    messages := []llms.MessageContent{
        {Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(fullSystemPrompt)}},
    }

    // Add history from memory
    history, _ := session.Memory.LoadMemoryVariables(ctx, nil)
    if hist, ok := history["history"]; ok {
        if histStr, ok := hist.(string); ok && histStr != "" {
            messages = append(messages, llms.MessageContent{
                Role:  llms.ChatMessageTypeHuman,
                Parts: []llms.ContentPart{llms.TextPart("Previous conversation:\n" + histStr)},
            })
        }
    }

    messages = append(messages, llms.MessageContent{
        Role:  llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{llms.TextPart(message)},
    })

    slog.Debug("chat request", "session_id", sessionID, "message_count", len(messages), "user_message_len", len(message))

    // Enable reasoning - thinking happens but isn't returned to caller
    resp, err := s.llm.GenerateContent(ctx, messages,
        llms.WithThinkingMode(llms.ThinkingModeEnabled),
    )
    if err != nil {
        slog.Error("chat error", "session_id", sessionID, "err", err)
        return "", fmt.Errorf("chat: %w", err)
    }

    if len(resp.Choices) == 0 {
        return "", fmt.Errorf("chat: no response")
    }

    content := resp.Choices[0].Content

    // Log thinking if present (for debugging)
    if resp.Choices[0].Thinking != "" {
        slog.Debug("chat reasoning", "session_id", sessionID, "thinking_len", len(resp.Choices[0].Thinking))
    }

    // Save to memory
    session.Memory.SaveContext(ctx,
        map[string]any{"input": message},
        map[string]any{"output": content},
    )

    return content, nil
}
```

- [ ] **Step 2: Update ChatStream method with reasoning streaming**

```go
func (s *Service) ChatStream(ctx context.Context, sessionID string, message string, systemPrompt string) (<-chan string, error) {
    session := s.chatStore.GetOrCreate(sessionID, s.llm, systemPrompt)

    fullSystemPrompt := systemPrompt
    if s.resumeMarkdown != "" {
        fullSystemPrompt = systemPrompt + "\n\nHere is the user's resume for reference:\n\n" + s.resumeMarkdown
    }

    messages := []llms.MessageContent{
        {Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(fullSystemPrompt)}},
    }

    // Add history from memory
    history, _ := session.Memory.LoadMemoryVariables(ctx, nil)
    if hist, ok := history["history"]; ok {
        if histStr, ok := hist.(string); ok && histStr != "" {
            messages = append(messages, llms.MessageContent{
                Role:  llms.ChatMessageTypeHuman,
                Parts: []llms.ContentPart{llms.TextPart("Previous conversation:\n" + histStr)},
            })
        }
    }

    messages = append(messages, llms.MessageContent{
        Role:  llms.ChatMessageTypeHuman,
        Parts: []llms.ContentPart{llms.TextPart(message)},
    })

    ch := make(chan string)
    go func() {
        defer close(ch)
        var full string
        _, err := s.llm.GenerateContent(ctx, messages,
            llms.WithThinkingMode(llms.ThinkingModeEnabled),
            llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
                full += string(chunk)
                ch <- string(chunk)
                return nil
            }),
        )
        if err != nil {
            slog.Error("chat stream error", "session_id", sessionID, "err", err)
            return
        }

        // Save to memory after streaming completes
        session.Memory.SaveContext(ctx,
            map[string]any{"input": message},
            map[string]any{"output": full},
        )
    }()

    return ch, nil
}
```

- [ ] **Step 3: Add reasoning to AnalyzeResume**

```go
func (s *Service) AnalyzeResume(ctx context.Context, jobDescription string, systemPrompt string, params *ProviderParams) (string, error) {
    provider, err := s.getProvider(params)
    if err != nil {
        return "", fmt.Errorf("get provider: %w", err)
    }

    messages := []llms.MessageContent{
        {Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(systemPrompt)}},
        {Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Resume:\n%s\n\nJob Description:\n%s", s.resumeMarkdown, jobDescription))}},
    }

    var options []llms.CallOption
    options = append(options, llms.WithThinkingMode(llms.ThinkingModeEnabled))
    if params.Provider == "openrouter" {
        options = append(options, llms.WithJSONMode())
    }

    resp, err := provider.GenerateContent(ctx, messages, options...)
    if err != nil {
        return "", fmt.Errorf("resume analyze: %w", err)
    }
    if len(resp.Choices) == 0 {
        return "", fmt.Errorf("resume analyze: no response")
    }

    // Log thinking for debugging
    if resp.Choices[0].Thinking != "" {
        slog.Debug("resume analysis reasoning", "thinking_len", len(resp.Choices[0].Thinking))
    }

    return resp.Choices[0].Content, nil
}
```

- [ ] **Step 4: Add reasoning to GenerateSkills**

```go
func (s *Service) GenerateSkills(ctx context.Context, jobDescription string, params *ProviderParams) (string, error) {
    currentSkills := extractSkills(s.resumeText)
    provider, err := s.getProvider(params)
    if err != nil {
        return "", fmt.Errorf("get provider: %w", err)
    }

    messages := []llms.MessageContent{
        {Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(s.prompts.Get("resume_generate"))}},
        {Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(fmt.Sprintf("Job Description:\n%s\n\nCurrent Skills Section:\n%s\n\nUpdate the skills to better match the job description. Add relevant keywords that are genuinely applicable. Remove irrelevant skills. Return the complete updated skills list.", jobDescription, currentSkills))}},
    }

    var options []llms.CallOption
    options = append(options, llms.WithThinkingMode(llms.ThinkingModeEnabled))
    if params.Provider == "openrouter" {
        options = append(options, llms.WithJSONMode())
    }

    resp, err := provider.GenerateContent(ctx, messages, options...)
    if err != nil {
        return "", fmt.Errorf("generate skills: %w", err)
    }

    if len(resp.Choices) == 0 {
        return "", fmt.Errorf("generate skills: no response")
    }

    // Log thinking for debugging
    if resp.Choices[0].Thinking != "" {
        slog.Debug("skills generation reasoning", "thinking_len", len(resp.Choices[0].Thinking))
    }

    return rebuildResume(s.resumeText, resp.Choices[0].Content)
}
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./internal/workspace/...`
Expected: Compiles

- [ ] **Step 6: Commit**

```bash
git add internal/workspace/service.go
git commit -m "feat: add reasoning support to workspace service"
```

---

### Task 2: Add Thinking Events to Handler

**Covers:** S4, S6

**Files:**
- Modify: `internal/workspace/handler.go`

**Interfaces:**
- Consumes: Updated `ChatStream` from workspace service
- Produces: SSE events with thinking phase

- [ ] **Step 1: Update HandleChatSend for thinking events**

```go
func (h *Handler) HandleChatSend(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        middleware.RespondJSON(w, http.StatusMethodNotAllowed, false, "Method not allowed", "")
        return
    }

    stream := r.URL.Query().Get("stream") == "true"

    var req struct {
        Message string `json:"message"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        middleware.RespondJSON(w, http.StatusBadRequest, false, "Invalid JSON", "")
        return
    }

    sessionID := getSessionID(r)
    if sessionID == "" {
        middleware.RespondJSON(w, http.StatusUnauthorized, false, "Not authenticated", "")
        return
    }

    systemPrompt := h.prompts.Get("chat_system")

    if stream {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        w.Header().Set("X-Accel-Buffering", "no")
        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "streaming not supported", http.StatusInternalServerError)
            return
        }

        // Send thinking event immediately
        fmt.Fprintf(w, "event: thinking\ndata: {}\n\n")
        flusher.Flush()

        ch, err := h.svc.ChatStream(r.Context(), sessionID, req.Message, systemPrompt)
        if err != nil {
            fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonEscape(err.Error()))
            flusher.Flush()
            return
        }

        for chunk := range ch {
            fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonEscape(chunk))
            flusher.Flush()
        }
        fmt.Fprintf(w, "event: done\ndata: {}\n\n")
        flusher.Flush()
        return
    }

    resp, err := h.svc.Chat(r.Context(), sessionID, req.Message, systemPrompt)
    if err != nil {
        middleware.RespondJSONAPI(w, r, http.StatusInternalServerError, false, "", "", err)
        return
    }

    middleware.RespondJSONWithData(w, http.StatusOK, true, "", "", map[string]string{"response": resp})
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/workspace/...`
Expected: Compiles

- [ ] **Step 3: Commit**

```bash
git add internal/workspace/handler.go
git commit -m "feat: add thinking SSE events to chat handler"
```

---

### Task 3: Update Frontend for Streaming

**Covers:** S4, S6

**Files:**
- Modify: `templates/chat.html`

**Interfaces:**
- Consumes: SSE events from `/api/chat/send?stream=true`
- Produces: Real-time streaming UI with thinking indicator

- [ ] **Step 1: Replace sendMessage function with EventSource**

Replace the `sendMessage` function and related async code:

```javascript
function sendMessage() {
    var text = chatInput.value.trim();
    if (!text || sendBtn.disabled) return;
    chatInput.value = '';
    chatInput.style.height = 'auto';
    addMessage('user', text);
    chatHistory.push({role: 'user', content: text});
    saveChatHistory();
    showLoader();
    sendBtn.disabled = true;
    stopBtn.classList.remove('hidden');

    // Use EventSource for streaming
    var params = new URLSearchParams({stream: 'true'});
    var body = JSON.stringify({message: text});
    
    // EventSource doesn't support POST, so use fetch with ReadableStream
    fetch('/api/chat/send?stream=true', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: body
    }).then(function(response) {
        var reader = response.body.getReader();
        var decoder = new TextDecoder();
        var buffer = '';
        var currentEvent = '';
        var messageDiv = null;
        var fullResponse = '';

        function processChunk() {
            return reader.read().then(function(result) {
                if (result.done) {
                    finishStream();
                    return;
                }

                buffer += decoder.decode(result.value, {stream: true});
                var lines = buffer.split('\n');
                buffer = lines.pop() || '';

                for (var i = 0; i < lines.length; i++) {
                    var line = lines[i];
                    if (line.startsWith('event: ')) {
                        currentEvent = line.substring(7).trim();
                    } else if (line.startsWith('data: ')) {
                        var data = line.substring(6);
                        handleEvent(currentEvent, data);
                    }
                }

                return processChunk();
            });
        }

        function handleEvent(event, data) {
            if (event === 'thinking') {
                // Already showing loader, thinking indicator visible
                return;
            }
            if (event === 'message') {
                if (!messageDiv) {
                    removeLoader();
                    messageDiv = addMessage('assistant', '');
                }
                try {
                    var parsed = JSON.parse(data);
                    fullResponse += parsed;
                    updateMessageContent(messageDiv, fullResponse);
                } catch(e) {
                    fullResponse += data;
                    updateMessageContent(messageDiv, fullResponse);
                }
            }
            if (event === 'error') {
                removeLoader();
                addMessage('assistant', 'Error: ' + data);
                sendBtn.disabled = false;
                stopBtn.classList.add('hidden');
                return;
            }
            if (event === 'done') {
                finishStream();
            }
        }

        function finishStream() {
            removeLoader();
            sendBtn.disabled = false;
            stopBtn.classList.add('hidden');
            if (fullResponse) {
                chatHistory.push({role: 'assistant', content: fullResponse});
                saveChatHistory();
            }
        }

        return processChunk();
    }).catch(function(err) {
        removeLoader();
        sendBtn.disabled = false;
        stopBtn.classList.add('hidden');
        addMessage('assistant', 'Error: Connection failed');
    });
}

function updateMessageContent(div, content) {
    var markdownDiv = div.querySelector('.markdown-content');
    if (markdownDiv) {
        markdownDiv.innerHTML = renderMarkdown(content);
    }
    chatMessages.scrollTop = chatMessages.scrollHeight;
}
```

- [ ] **Step 2: Remove async job polling code**

Remove:
- `generateJobId()` function
- `pollJobStatus()` function
- `pendingJobId` variable
- `chat_pending_job_id` localStorage usage

- [ ] **Step 3: Update stop button to abort fetch**

```javascript
var currentAbortController = null;

stopBtn.addEventListener('click', function() {
    if (currentAbortController) {
        currentAbortController.abort();
        currentAbortController = null;
    }
    removeLoader();
    sendBtn.disabled = false;
    stopBtn.classList.add('hidden');
});
```

- [ ] **Step 4: Verify HTML is valid**

Check that all script changes are syntactically correct.

- [ ] **Step 5: Commit**

```bash
git add templates/chat.html
git commit -m "feat: replace async polling with SSE streaming in chat"
```

---

### Task 4: Final Verification

**Covers:** All

**Files:**
- (none - verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: Compiles

- [ ] **Step 2: Run tests**

Run: `go test ./... -v -count=1`
Expected: Tests pass

- [ ] **Step 3: Cross-compile**

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null`
Expected: Compiles

- [ ] **Step 4: Commit if needed**

```bash
git add -A
git commit -m "chore: reasoning and streaming chat complete"
```
