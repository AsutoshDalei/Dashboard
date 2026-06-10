package tracker

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pi_dashboard/pkg/llm"
)

type Service struct {
	repo     *Repository
	llm      llm.Provider
	timezone *time.Location
}

func NewService(repo *Repository, llmProvider llm.Provider, tz string) *Service {
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return &Service{
		repo:     repo,
		llm:      llmProvider,
		timezone: loc,
	}
}

func (s *Service) Upsert(ctx context.Context, app Application) (*Application, error) {
	return s.repo.Upsert(ctx, app)
}

func (s *Service) Check(ctx context.Context, name string) (*Application, error) {
	return s.repo.GetByOrganization(ctx, name)
}

func (s *Service) Suggest(ctx context.Context, query string, limit int) ([]string, error) {
	return s.repo.Suggest(ctx, query, limit)
}

func (s *Service) Stats(ctx context.Context) (*Stats, error) {
	return s.repo.Stats(ctx, s.timezone)
}

func (s *Service) Timeline(ctx context.Context, days int) ([]TimelineEntry, error) {
	return s.repo.Timeline(ctx, days)
}

func (s *Service) ContributionHeatmap(ctx context.Context, year int) ([]ContributionDay, error) {
	return s.repo.ContributionHeatmap(ctx, year)
}

func (s *Service) LogActivity(ctx context.Context, org string, delta int, date string, action string) error {
	return s.repo.LogActivity(ctx, org, delta, date, action)
}

const schemaDoc = `You are a PostgreSQL query generator for a personal job-application tracker.
The only table you may query is:

TABLE applications (
  id                SERIAL PRIMARY KEY,
  organization      VARCHAR(255),
  job_role          TEXT,
  location          VARCHAR(255),
  contacts          VARCHAR(255),
  applied_dates     DATE,
  remarks           TEXT,
  status            VARCHAR(50),
  category          VARCHAR(100),
  count             INTEGER,
  username_password TEXT,
  created_at        TIMESTAMP
)

Rules:
- Only SELECT or WITH (CTE) read queries.
- Use PostgreSQL syntax.
- Prefer explicit column lists over SELECT *.
- Return ONLY the SQL statement. No markdown, prose, or multiple statements.`

var sqlFenceRe = regexp.MustCompile("(?is)```(?:sql)?\\s*(.*?)```")
var writeKeywordRe = regexp.MustCompile(`(?i)\b(insert|update|delete|merge|drop|alter|truncate|create|grant|revoke|copy|vacuum|call|reindex|refresh)\b`)

func (s *Service) NaturalLanguageQuery(ctx context.Context, nl string) (string, error) {
	messages := []llm.Message{
		{Role: "system", Content: schemaDoc},
		{Role: "user", Content: nl},
	}
	resp, err := s.llm.Chat(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("nl query: %w", err)
	}
	return extractSQL(resp.Content), nil
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