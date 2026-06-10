package tracker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Upsert(ctx context.Context, app Application) (*UpsertResult, error) {
	var prevCount int
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(count, 0) FROM applications WHERE organization = $1`, app.Organization).Scan(&prevCount)
	exists := err == nil

	newCount := app.Count
	if exists {
		newCount = prevCount + app.Count
	}

	query := `INSERT INTO applications (organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (organization) DO UPDATE SET
			count = $9,
			job_role = COALESCE(NULLIF(EXCLUDED.job_role, ''), applications.job_role),
			status = COALESCE(NULLIF(EXCLUDED.status, ''), applications.status),
			category = COALESCE(NULLIF(EXCLUDED.category, ''), applications.category),
			location = COALESCE(NULLIF(EXCLUDED.location, ''), applications.location),
			contacts = COALESCE(NULLIF(EXCLUDED.contacts, ''), applications.contacts),
			applied_dates = COALESCE(NULLIF(EXCLUDED.applied_dates, ''), applications.applied_dates),
			remarks = COALESCE(NULLIF(EXCLUDED.remarks, ''), applications.remarks),
			username_password = COALESCE(NULLIF(EXCLUDED.username_password, ''), applications.username_password),
			updated_at = NOW()
		RETURNING id, organization, count`

	var resultID int
	var resultOrg string
	var resultCount int
	err = r.pool.QueryRow(ctx, query,
		app.Organization, app.JobRole, app.Location, app.Contacts,
		app.AppliedDates, app.Remarks, app.Status, app.Category,
		newCount, app.UsernamePassword,
	).Scan(&resultID, &resultOrg, &resultCount)
	if err != nil {
		return nil, fmt.Errorf("upsert: %w", err)
	}

	action := "updated"
	if !exists {
		action = "created"
	}

	return &UpsertResult{
		Action:        action,
		Organization:  resultOrg,
		PreviousCount: prevCount,
		Added:         app.Count,
		NewCount:      resultCount,
	}, nil
}

func (r *Repository) GetByOrganization(ctx context.Context, name string) (*Application, error) {
	query := `SELECT id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password
		FROM applications WHERE organization = $1`
	row := r.pool.QueryRow(ctx, query, name)

	var app Application
	err := row.Scan(
		&app.ID, &app.Organization, &app.JobRole, &app.Location,
		&app.Contacts, &app.AppliedDates, &app.Remarks, &app.Status,
		&app.Category, &app.Count, &app.UsernamePassword,
	)
	if err != nil {
		return nil, fmt.Errorf("get by org: %w", err)
	}
	return &app, nil
}

func (r *Repository) Suggest(ctx context.Context, query string, limit int) ([]map[string]string, error) {
	sql := `SELECT DISTINCT organization FROM applications WHERE organization ILIKE $1 ORDER BY organization LIMIT $2`
	rows, err := r.pool.Query(ctx, sql, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("suggest: %w", err)
	}
	defer rows.Close()

	var results []map[string]string
	for rows.Next() {
		var org string
		if err := rows.Scan(&org); err != nil {
			return nil, err
		}
		results = append(results, map[string]string{"organization": org})
	}
	return results, nil
}

func (r *Repository) Stats(ctx context.Context, timezone *time.Location) (*Stats, error) {
	now := time.Now().In(timezone)
	today := now.Format("2006-01-02")
	weekStart := now.AddDate(0, 0, -int(now.Weekday())).Format("2006-01-02")

	var s Stats

	err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(count), 0) FROM applications`).Scan(&s.Companies, &s.Applications)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM applications WHERE status = 'Applied'`).Scan(&s.Applied)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM applications WHERE status = 'Rejected'`).Scan(&s.Rejected)
	if err != nil {
		return nil, err
	}

	if s.Companies > 0 {
		s.AppliedPct = float64(s.Applied) / float64(s.Companies) * 100
		s.RejectedPct = float64(s.Rejected) / float64(s.Companies) * 100
		s.AvgPerCompany = float64(s.Applications) / float64(s.Companies)
	}

	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_count), 0), COUNT(DISTINCT organization)
		FROM application_activity_logs WHERE activity_date = $1`, today,
	).Scan(&s.TodayApplications, &s.TodayCompanies)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_count), 0), COUNT(DISTINCT organization)
		FROM application_activity_logs WHERE activity_date >= $1 AND activity_date < $2`,
		weekStart, now.Format("2006-01-02"),
	).Scan(&s.WeekApplications, &s.WeekCompanies)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(organization, ''), COALESCE(MAX(count), 0) FROM applications GROUP BY organization ORDER BY MAX(count) DESC LIMIT 1`,
	).Scan(&s.TopCompany, &s.MaxPerCompany)
	if err != nil {
		s.TopCompany = ""
		s.MaxPerCompany = 0
	}

	return &s, nil
}

func (r *Repository) Timeline(ctx context.Context, days int) ([]TimelineEntry, error) {
	query := `SELECT activity_date, SUM(delta_count), COALESCE(string_agg(DISTINCT action, ', '), '')
		FROM application_activity_logs
		WHERE activity_date >= CURRENT_DATE - $1::integer
		GROUP BY activity_date ORDER BY activity_date`

	rows, err := r.pool.Query(ctx, query, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []TimelineEntry
	for rows.Next() {
		var e TimelineEntry
		if err := rows.Scan(&e.Date, &e.Count, &e.Action); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *Repository) ContributionHeatmap(ctx context.Context, year int) ([]ContributionDay, error) {
	query := `SELECT activity_date, SUM(delta_count)
		FROM application_activity_logs
		WHERE EXTRACT(YEAR FROM activity_date) = $1
		GROUP BY activity_date ORDER BY activity_date`

	rows, err := r.pool.Query(ctx, query, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var days []ContributionDay
	for rows.Next() {
		var d ContributionDay
		if err := rows.Scan(&d.Date, &d.Count); err != nil {
			return nil, err
		}
		switch {
		case d.Count == 0:
			d.Level = 0
		case d.Count <= 3:
			d.Level = 1
		case d.Count <= 6:
			d.Level = 2
		case d.Count <= 9:
			d.Level = 3
		default:
			d.Level = 4
		}
		days = append(days, d)
	}
	return days, nil
}

func (r *Repository) LogActivity(ctx context.Context, org string, delta int, date string, action string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO application_activity_logs (organization, delta_count, activity_date, action) VALUES ($1, $2, $3, $4)`,
		org, delta, date, action,
	)
	return err
}

func (r *Repository) Query(ctx context.Context, sql string) (pgx.Rows, error) {
	return r.pool.Query(ctx, sql)
}

func (r *Repository) CheckExists(ctx context.Context, name string) (bool, int, string, *string, error) {
	var count int
	var status string
	var appliedDates *string
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(status, ''), applied_dates FROM applications WHERE organization = $1 GROUP BY organization, status, applied_dates`,
		name,
	).Scan(&count, &status, &appliedDates)
	if err != nil {
		return false, 0, "", nil, nil
	}
	return true, count, status, appliedDates, nil
}