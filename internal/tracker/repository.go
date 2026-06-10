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

func (r *Repository) Upsert(ctx context.Context, app Application) (*Application, error) {
	query := `INSERT INTO applications (organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (organization) DO UPDATE SET
			count = applications.count + 1,
			job_role = EXCLUDED.job_role,
			status = EXCLUDED.status,
			updated_at = NOW()
		RETURNING id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password`

	row := r.pool.QueryRow(ctx, query,
		app.Organization, app.JobRole, app.Location, app.Contacts,
		app.AppliedDates, app.Remarks, app.Status, app.Category,
		app.Count, app.UsernamePassword,
	)

	var result Application
	err := row.Scan(
		&result.ID, &result.Organization, &result.JobRole, &result.Location,
		&result.Contacts, &result.AppliedDates, &result.Remarks, &result.Status,
		&result.Category, &result.Count, &result.UsernamePassword,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert: %w", err)
	}
	return &result, nil
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

func (r *Repository) Suggest(ctx context.Context, query string, limit int) ([]string, error) {
	sql := `SELECT DISTINCT organization FROM applications WHERE organization ILIKE $1 ORDER BY organization LIMIT $2`
	rows, err := r.pool.Query(ctx, sql, "%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("suggest: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var org string
		if err := rows.Scan(&org); err != nil {
			return nil, err
		}
		results = append(results, org)
	}
	return results, nil
}

func (r *Repository) Stats(ctx context.Context, timezone *time.Location) (*Stats, error) {
	now := time.Now().In(timezone)
	today := now.Format("2006-01-02")
	weekStart := now.AddDate(0, 0, -int(now.Weekday())).Format("2006-01-02")

	var stats Stats

	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM applications`).Scan(&stats.TotalApplications)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT organization) FROM applications`).Scan(&stats.TotalCompanies)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_count), 0), COUNT(DISTINCT organization)
		FROM application_activity_logs WHERE activity_date = $1`, today,
	).Scan(&stats.TodayApplications, &stats.TodayCompanies)
	if err != nil {
		return nil, err
	}

	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(delta_count), 0), COUNT(DISTINCT organization)
		FROM application_activity_logs WHERE activity_date >= $1 AND activity_date < $2`,
		weekStart, now.Format("2006-01-02"),
	).Scan(&stats.WeekApplications, &stats.WeekCompanies)
	if err != nil {
		return nil, err
	}

	return &stats, nil
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

func (r *Repository) CheckExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE organization = $1)`, name).Scan(&exists)
	return exists, err
}