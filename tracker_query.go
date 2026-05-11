package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const openRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"
const maxQueryRows = 500
const queryTimeout = 15 * time.Second

func resolveOpenRouterModels() []string {
	raw := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("OPENROUTER_MODELS"))
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

const applicationsSchemaDoc = `You are a PostgreSQL query generator for a personal job-application tracker.
The only table you may query is:

TABLE applications (
  id                SERIAL PRIMARY KEY,
  organization      VARCHAR(255),    -- company name
  job_role          TEXT,            -- role title or posting URL
  location          VARCHAR(255),
  contacts          VARCHAR(255),
  applied_dates     DATE,            -- first-applied date (local)
  remarks           TEXT,
  status            VARCHAR(50),     -- Applied / Rejected / Interview / Offer / Accepted
  category          VARCHAR(100),    -- Fulltime / Internship / Contract
  count             INTEGER,         -- total applications submitted to this organization
  username_password TEXT,             -- misc notes
  created_at        TIMESTAMP
)

Rules:
- Only SELECT or WITH (CTE) read queries are permitted. Never generate INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE, CREATE, GRANT, REVOKE, COPY, VACUUM, CALL, DO, or MERGE.
- Use PostgreSQL syntax.
- Prefer explicit column lists over SELECT *.
- Return ONLY the SQL statement. Do not include markdown fences, prose, commentary, or multiple statements.`

type QueryRequest struct {
	Mode  string `json:"mode"`
	Query string `json:"query"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Models   []string            `json:"models,omitempty"`
	Messages []openRouterMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func callOpenRouter(ctx context.Context, prompt string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		return "", fmt.Errorf("OpenRouter is not configured. Set OPENROUTER_API_KEY.")
	}

	models := resolveOpenRouterModels()
	if len(models) == 0 {
		return "", fmt.Errorf("OpenRouter is not configured. Set OPENROUTER_MODEL to a model slug (or comma-separated fallback list).")
	}
	reqBody := openRouterRequest{
		Model: models[0],
		Messages: []openRouterMessage{
			{Role: "system", Content: applicationsSchemaDoc},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}
	if len(models) > 1 {
		reqBody.Models = models
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to encode OpenRouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to build OpenRouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OpenRouter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read OpenRouter response: %w", err)
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("OpenRouter returned non-JSON response (status %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("OpenRouter error: %s", parsed.Error.Message)
		}
		return "", fmt.Errorf("OpenRouter error (status %d)", resp.StatusCode)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("OpenRouter returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

var sqlFenceRe = regexp.MustCompile("(?is)```(?:sql)?\\s*(.*?)```")

func extractSQL(raw string) string {
	s := strings.TrimSpace(raw)
	if m := sqlFenceRe.FindStringSubmatch(s); len(m) == 2 {
		s = strings.TrimSpace(m[1])
	}
	if idx := strings.Index(s, ";"); idx >= 0 {
		rest := strings.TrimSpace(s[idx+1:])
		if rest == "" {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.TrimSpace(s)
}

var writeKeywordRe = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|truncate|create|grant|revoke|copy|vacuum|call|reindex|refresh)\b`)

var leadingCommentRe = regexp.MustCompile(`(?s)^\s*(--[^\n]*\n|/\*.*?\*/)`)

func stripLeadingComments(sql string) string {
	for {
		next := leadingCommentRe.ReplaceAllString(sql, "")
		next = strings.TrimLeft(next, " \t\r\n")
		if next == sql {
			return next
		}
		sql = next
	}
}

func validateReadOnlySQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return fmt.Errorf("empty query")
	}

	if idx := strings.Index(trimmed, ";"); idx >= 0 {
		rest := strings.TrimSpace(trimmed[idx+1:])
		if rest != "" {
			return fmt.Errorf("only a single statement is allowed")
		}
		trimmed = strings.TrimSpace(trimmed[:idx])
	}

	body := stripLeadingComments(trimmed)
	lower := strings.ToLower(body)
	if !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return fmt.Errorf("only SELECT or WITH (CTE) queries are permitted")
	}

	if writeKeywordRe.MatchString(lower) {
		return fmt.Errorf("query contains a disallowed keyword; only read-only SELECT/WITH queries are permitted")
	}

	return nil
}

func normalizeRowValue(v interface{}) interface{} {
	switch val := v.(type) {
	case nil:
		return nil
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	default:
		return val
	}
}

func respondQueryError(w http.ResponseWriter, status int, sqlText, errMessage string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"sql":     sqlText,
		"error":   errMessage,
	})
}

func respondQueryErrorInternal(w http.ResponseWriter, r *http.Request, status int, sqlText string, logErr error) {
	slog.Error("tracker query", "err", logErr, "request_id", requestIDFrom(r), "sql", sqlText)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    false,
		"sql":        sqlText,
		"error":      "Query execution failed. Check server logs.",
		"request_id": requestIDFrom(r),
	})
}

func runReadOnlyApplicationQuery(ctx context.Context, sqlText string) (pgx.Rows, func(), error) {
	if dbPoolReader != nil {
		rows, err := dbPoolReader.Query(ctx, sqlText)
		if err != nil {
			return nil, nil, err
		}
		return rows, func() { rows.Close() }, nil
	}

	tx, err := dbPool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = tx.Rollback(ctx)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '5000'"); err != nil {
		cleanup()
		return nil, nil, err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
		cleanup()
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, sqlText)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return rows, func() {
		rows.Close()
		_ = tx.Rollback(ctx)
	}, nil
}

func handleApplicationsQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		respondJSON(w, http.StatusBadRequest, false, "query is required", "")
		return
	}
	if req.Mode == "" {
		req.Mode = "nl"
	}
	if req.Mode != "nl" && req.Mode != "sql" {
		respondJSON(w, http.StatusBadRequest, false, "mode must be 'nl' or 'sql'", "")
		return
	}

	var sqlText string
	if req.Mode == "nl" {
		llmCtx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		raw, err := callOpenRouter(llmCtx, req.Query)
		if err != nil {
			respondQueryErrorInternal(w, r, http.StatusBadGateway, "", err)
			return
		}
		sqlText = extractSQL(raw)
		if sqlText == "" {
			respondQueryError(w, http.StatusBadGateway, "", "Model did not return a SQL statement.")
			return
		}
	} else {
		sqlText = extractSQL(req.Query)
	}

	if err := validateReadOnlySQL(sqlText); err != nil {
		respondQueryError(w, http.StatusBadRequest, sqlText, err.Error())
		return
	}

	dbCtx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	rows, closeRows, err := runReadOnlyApplicationQuery(dbCtx, sqlText)
	if err != nil {
		respondQueryErrorInternal(w, r, http.StatusBadRequest, sqlText, err)
		return
	}
	defer closeRows()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	out := make([][]interface{}, 0)
	truncated := false
	for rows.Next() {
		if len(out) >= maxQueryRows {
			truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			respondQueryErrorInternal(w, r, http.StatusBadRequest, sqlText, err)
			return
		}
		for i := range vals {
			vals[i] = normalizeRowValue(vals[i])
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		respondQueryErrorInternal(w, r, http.StatusBadRequest, sqlText, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"sql":       sqlText,
		"columns":   columns,
		"rows":      out,
		"row_count": len(out),
		"truncated": truncated,
	})
}
