package tracker

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"pi_dashboard/internal/llm"
	"github.com/tmc/langchaingo/llms"
)

type Service struct {
	repo    *Repository
	llm     llms.Model
	prompts *llm.Prompts
}

func NewService(repo *Repository, llmProvider llms.Model, prompts *llm.Prompts) *Service {
	return &Service{
		repo:    repo,
		llm:     llmProvider,
		prompts: prompts,
	}
}

func (s *Service) Upsert(ctx context.Context, app Application) (*UpsertResult, error) {
	result, err := s.repo.Upsert(ctx, app)
	if err != nil {
		return nil, err
	}

	if app.Count > 0 {
		activityDate := ""
		if app.AppliedDates != nil && strings.TrimSpace(*app.AppliedDates) != "" {
			activityDate = strings.TrimSpace(*app.AppliedDates)
		} else {
			activityDate = time.Now().In(time.Local).Format("2006-01-02")
		}
		action := "upsert"
		if app.Status != nil && strings.TrimSpace(*app.Status) != "" {
			action = strings.TrimSpace(*app.Status)
		}
		if err := s.repo.LogActivity(ctx, result.Organization, app.Count, activityDate, action); err != nil {
			slog.Warn("log tracker activity", "err", err)
		}
	}

	return result, nil
}

func (s *Service) Check(ctx context.Context, name string) (string, bool, int, string, *string, error) {
	return s.repo.CheckExists(ctx, name)
}

func (s *Service) Suggest(ctx context.Context, query string, limit int) ([]map[string]string, error) {
	return s.repo.Suggest(ctx, query, limit)
}

func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	now := time.Now().In(time.Local)
	today := now.Format("2006-01-02")
	weekStart := now.AddDate(0, 0, -int(now.Weekday())).Format("2006-01-02")
	return s.repo.Stats(ctx, today, weekStart)
}

func (s *Service) Timeline(ctx context.Context, days int, freq string) ([]TimelineEntry, error) {
	today := time.Now().In(time.Local).Format("2006-01-02")
	return s.repo.Timeline(ctx, days, freq, today)
}

func (s *Service) ContributionHeatmap(ctx context.Context, year int, month int) ([]ContributionDay, error) {
	return s.repo.ContributionHeatmap(ctx, year, month)
}

func (s *Service) DateRange(ctx context.Context) (string, string, error) {
	return s.repo.DateRange(ctx)
}

func (s *Service) LogActivity(ctx context.Context, org string, delta int, date string, action string) error {
	return s.repo.LogActivity(ctx, org, delta, date, action)
}

var sqlFenceRe = regexp.MustCompile("(?is)```(?:sql)?\\s*(.*?)```")
var writeKeywordRe = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|truncate|create|grant|revoke|copy|vacuum|call|reindex|refresh)\b`)

func (s *Service) NaturalLanguageQuery(ctx context.Context, nl string) (string, error) {
	schemaDoc := s.prompts.Get("sql_assistant")
	messages := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(schemaDoc)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(nl)}},
	}
	resp, err := s.llm.GenerateContent(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("nl query: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("nl query: no response")
	}
	return extractSQL(resp.Choices[0].Content), nil
}

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

func ValidateReadOnlySQL(sql string) error {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return fmt.Errorf("empty query")
	}
	if idx := strings.Index(trimmed, ";"); idx >= 0 {
		rest := strings.TrimSpace(trimmed[idx+1:])
		if rest != "" {
			return fmt.Errorf("only single statement allowed")
		}
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")) {
		return fmt.Errorf("only SELECT or WITH queries permitted")
	}
	if writeKeywordRe.MatchString(lower) {
		return fmt.Errorf("query contains disallowed keyword")
	}
	return nil
}

func (s *Service) ExecuteQuery(ctx context.Context, sql string) (*QueryResult, error) {
	rows, err := s.repo.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var out [][]any
	truncated := false
	maxRows := 500

	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i := range vals {
			vals[i] = normalizeValue(vals[i])
		}
		out = append(out, vals)
	}

	return &QueryResult{
		SQL:       sql,
		Columns:   columns,
		Rows:      out,
		RowCount:  len(out),
		Truncated: truncated,
	}, nil
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	default:
		return val
	}
}
