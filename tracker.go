package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type Application struct {
	ID               int     `json:"id,omitempty"`
	Organization     string  `json:"organization"`
	JobRole          *string `json:"job_role,omitempty"`
	Location         *string `json:"location,omitempty"`
	Contacts         *string `json:"contacts,omitempty"`
	AppliedDates     *string `json:"applied_dates,omitempty"`
	Remarks          *string `json:"remarks,omitempty"`
	Status           *string `json:"status,omitempty"`
	Category         *string `json:"category,omitempty"`
	Count            int     `json:"count"`
	UsernamePassword *string `json:"username_password,omitempty"`
}

type CompanySuggestion struct {
	Organization string `json:"organization"`
}

func ptrFromNullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func scanApplication(scanner interface {
	Scan(dest ...interface{}) error
}) (*Application, error) {
	var app Application
	var jobRole, location, contacts, appliedDates, remarks, status, category, usernamePassword sql.NullString
	err := scanner.Scan(
		&app.ID,
		&app.Organization,
		&jobRole,
		&location,
		&contacts,
		&appliedDates,
		&remarks,
		&status,
		&category,
		&app.Count,
		&usernamePassword,
	)
	if err != nil {
		return nil, err
	}
	app.JobRole = ptrFromNullString(jobRole)
	app.Location = ptrFromNullString(location)
	app.Contacts = ptrFromNullString(contacts)
	app.AppliedDates = ptrFromNullString(appliedDates)
	app.Remarks = ptrFromNullString(remarks)
	app.Status = ptrFromNullString(status)
	app.Category = ptrFromNullString(category)
	app.UsernamePassword = ptrFromNullString(usernamePassword)
	return &app, nil
}

func handleTrackerTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "tracker.html", nil); err != nil {
		log.Printf("Template rendering error: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func fetchApplicationByOrg(ctx context.Context, name string) (*Application, error) {
	const query = `
SELECT id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password
FROM applications
WHERE LOWER(organization) = LOWER($1)
LIMIT 1`
	row := dbPool.QueryRow(ctx, query, name)
	app, err := scanApplication(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return app, nil
}

func handleApplicationsCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	company := strings.TrimSpace(r.URL.Query().Get("company"))
	if company == "" {
		respondJSON(w, http.StatusBadRequest, false, "company query param is required", "")
		return
	}

	app, err := fetchApplicationByOrg(r.Context(), company)
	if err != nil {
		log.Printf("tracker check error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if app == nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"exists":  false,
			"query":   company,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"exists":        true,
		"id":            app.ID,
		"organization":  app.Organization,
		"count":         app.Count,
		"status":        app.Status,
		"applied_dates": app.AppliedDates,
		"category":      app.Category,
		"job_role":      app.JobRole,
		"location":      app.Location,
		"contacts":      app.Contacts,
		"remarks":       app.Remarks,
	})
}

func handleApplicationsSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"suggestions": []CompanySuggestion{},
		})
		return
	}

	limit := 3
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 10 {
			limit = parsed
		}
	}

	const suggestQuery = `
WITH matches AS (
	SELECT organization, 1 AS rank
	FROM applications
	WHERE LOWER(organization) LIKE LOWER($1) || '%'
	UNION ALL
	SELECT organization, 2 AS rank
	FROM applications
	WHERE LOWER(organization) LIKE '%' || LOWER($1) || '%'
	  AND LOWER(organization) NOT LIKE LOWER($1) || '%'
),
dedup AS (
	SELECT DISTINCT ON (LOWER(organization)) organization, rank
	FROM matches
	ORDER BY LOWER(organization), rank, organization
)
SELECT organization
FROM dedup
ORDER BY rank, organization
LIMIT $2`
	rows, err := dbPool.Query(r.Context(), suggestQuery, query, limit)
	if err != nil {
		log.Printf("tracker suggest error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}
	defer rows.Close()

	suggestions := make([]CompanySuggestion, 0, limit)
	for rows.Next() {
		var s CompanySuggestion
		if err := rows.Scan(&s.Organization); err != nil {
			log.Printf("tracker suggest scan error: %v", err)
			respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
			return
		}
		suggestions = append(suggestions, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("tracker suggest rows error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"suggestions": suggestions,
	})
}

type UpsertRequest struct {
	Organization     string `json:"organization"`
	JobRole          string `json:"job_role"`
	Location         string `json:"location"`
	Contacts         string `json:"contacts"`
	AppliedDates     string `json:"applied_dates"`
	Remarks          string `json:"remarks"`
	Status           string `json:"status"`
	Category         string `json:"category"`
	Count            int    `json:"count"`
	UsernamePassword string `json:"username_password"`
}

func parseOptionalDate(raw string) *time.Time {
	if t, ok := parseAppliedDate(raw); ok {
		return &t
	}
	return nil
}

func ensureApplicationActivityLogs(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS application_activity_logs (
	id SERIAL PRIMARY KEY,
	organization VARCHAR(255) NOT NULL,
	delta_count INTEGER NOT NULL DEFAULT 0,
	activity_date DATE NOT NULL,
	action VARCHAR(32),
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)`
	_, err := dbPool.Exec(ctx, ddl)
	return err
}

func logApplicationActivity(ctx context.Context, organization string, deltaCount int, activityDate *time.Time, action string) error {
	if deltaCount <= 0 {
		return nil
	}
	if err := ensureApplicationActivityLogs(ctx); err != nil {
		return err
	}
	when := time.Now().Format("2006-01-02")
	if activityDate != nil {
		when = activityDate.Format("2006-01-02")
	}
	const insertLog = `
INSERT INTO application_activity_logs (organization, delta_count, activity_date, action)
VALUES ($1, $2, $3, $4)`
	_, err := dbPool.Exec(ctx, insertLog, organization, deltaCount, when, action)
	return err
}

var (
	activityBootstrapMu   sync.Mutex
	activityBootstrapDone bool
)

func ensureActivityLogBootstrap(ctx context.Context) error {
	activityBootstrapMu.Lock()
	defer activityBootstrapMu.Unlock()

	if activityBootstrapDone {
		return nil
	}
	if err := ensureApplicationActivityLogs(ctx); err != nil {
		return err
	}
	if err := backfillLegacyActivityLogs(ctx); err != nil {
		return err
	}
	activityBootstrapDone = true
	return nil
}

func backfillLegacyActivityLogs(ctx context.Context) error {
	const backfill = `
INSERT INTO application_activity_logs (organization, delta_count, activity_date, action)
SELECT
	a.organization,
	a.count,
	CASE
		WHEN CAST(a.applied_dates AS text) ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}' THEN SUBSTRING(CAST(a.applied_dates AS text) FROM 1 FOR 10)::date
		ELSE CURRENT_DATE
	END,
	'created_backfill'
FROM applications a
WHERE a.count > 0
  AND NOT EXISTS (
		SELECT 1
		FROM application_activity_logs l
		WHERE LOWER(TRIM(l.organization)) = LOWER(TRIM(a.organization))
	)`
	_, err := dbPool.Exec(ctx, backfill)
	return err
}

func handleApplicationsUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, false, "Invalid JSON payload", "")
		return
	}
	req.Organization = strings.TrimSpace(req.Organization)
	if req.Organization == "" {
		respondJSON(w, http.StatusBadRequest, false, "organization is required", "")
		return
	}
	existing, err := fetchApplicationByOrg(r.Context(), req.Organization)
	if err != nil {
		log.Printf("tracker upsert lookup error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}

	if existing == nil {
		if req.Count <= 0 {
			req.Count = 1
		}
		const insertQuery = `
INSERT INTO applications (
	organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password`
		row := dbPool.QueryRow(
			r.Context(),
			insertQuery,
			req.Organization,
			nullIfEmpty(req.JobRole),
			nullIfEmpty(req.Location),
			nullIfEmpty(req.Contacts),
			nullIfEmpty(req.AppliedDates),
			nullIfEmpty(req.Remarks),
			nullIfEmpty(req.Status),
			nullIfEmpty(req.Category),
			req.Count,
			nullIfEmpty(req.UsernamePassword),
		)
		inserted, err := scanApplication(row)
		if err != nil {
			log.Printf("tracker insert error: %v", err)
			respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
			return
		}

		if err := logApplicationActivity(r.Context(), req.Organization, req.Count, parseOptionalDate(req.AppliedDates), "created"); err != nil {
			log.Printf("tracker activity log (create) error: %v", err)
			respondJSON(w, http.StatusBadGateway, false, "Failed to record activity: "+err.Error(), "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"action":         "created",
			"row":            inserted,
			"previous_count": 0,
			"added":          req.Count,
			"new_count":      req.Count,
		})
		return
	}
	if req.Count < 0 {
		req.Count = 0
	}
	activityDate := parseOptionalDate(req.AppliedDates)
	isAddFlow := req.Count > 0
	if isAddFlow {
		// Keep status and first applied date immutable when only adding more applications.
		req.Status = ""
		req.AppliedDates = ""
	}

	const updateQuery = `
UPDATE applications
SET count = $1,
	job_role = COALESCE($2, job_role),
	location = COALESCE($3, location),
	contacts = COALESCE($4, contacts),
	applied_dates = COALESCE($5, applied_dates),
	remarks = COALESCE($6, remarks),
	status = COALESCE($7, status),
	category = COALESCE($8, category),
	username_password = COALESCE($9, username_password)
WHERE id = $10
RETURNING id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password`
	row := dbPool.QueryRow(
		r.Context(),
		updateQuery,
		existing.Count+req.Count,
		nullIfEmpty(req.JobRole),
		nullIfEmpty(req.Location),
		nullIfEmpty(req.Contacts),
		nullIfEmpty(req.AppliedDates),
		nullIfEmpty(req.Remarks),
		nullIfEmpty(req.Status),
		nullIfEmpty(req.Category),
		nullIfEmpty(req.UsernamePassword),
		existing.ID,
	)
	patched, err := scanApplication(row)
	if err != nil {
		log.Printf("tracker patch error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}

	if isAddFlow {
		if err := logApplicationActivity(r.Context(), existing.Organization, req.Count, activityDate, "added"); err != nil {
			log.Printf("tracker activity log (update) error: %v", err)
			respondJSON(w, http.StatusBadGateway, false, "Failed to record activity: "+err.Error(), "")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"action":         "updated",
		"row":            patched,
		"previous_count": existing.Count,
		"added":          req.Count,
		"new_count":      existing.Count + req.Count,
	})
}

func nullIfEmpty(value string) interface{} {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	return v
}

func parseAppliedDate(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02",
		"01/02/2006",
		"02-01-2006",
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	// Last-resort normalization for datetime values like "YYYY-MM-DD ..."
	// so timeline/stat parsing still works when time is stored alongside date.
	if len(value) >= 10 {
		if t, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func fetchAllApplications(ctx context.Context) ([]Application, error) {
	const query = `
SELECT id, organization, job_role, location, contacts, applied_dates, remarks, status, category, count, username_password
FROM applications
LIMIT 100000`
	rows, err := dbPool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Application, 0)
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *app)
	}
	return out, rows.Err()
}

type StatsResponse struct {
	Companies           int            `json:"companies"`
	Applications        int            `json:"applications"`
	Applied             int            `json:"applied"`
	AppliedApps         int            `json:"applied_applications"`
	Rejected            int            `json:"rejected"`
	RejectedApps        int            `json:"rejected_applications"`
	Other               int            `json:"other"`
	OtherApps           int            `json:"other_applications"`
	AppliedPct          float64        `json:"applied_pct"`
	RejectedPct         float64        `json:"rejected_pct"`
	AvgPerCompany       float64        `json:"avg_per_company"`
	MaxPerCompany       int            `json:"max_per_company"`
	TopCompany          string         `json:"top_company"`
	LastAppliedDate     *string        `json:"last_applied_date"`
	Last30DaysCompanies int            `json:"last_30_days_companies"`
	Last30DaysApps      int            `json:"last_30_days_applications"`
	TodayCompanies      int            `json:"today_companies"`
	TodayApplications   int            `json:"today_applications"`
	WeekCompanies       int            `json:"week_companies"`
	WeekApplications    int            `json:"week_applications"`
	StatusBreakdown     map[string]int `json:"status_breakdown"`
	CategoryBreakdown   map[string]int `json:"category_breakdown"`
}

type ActivityLog struct {
	Organization string
	DeltaCount   int
	ActivityDate time.Time
}

func fetchAllActivityLogs(ctx context.Context) ([]ActivityLog, error) {
	const query = `
SELECT organization, delta_count, activity_date::text
FROM application_activity_logs
WHERE delta_count > 0
LIMIT 200000`
	rows, err := dbPool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	loc := time.Now().Location()
	out := make([]ActivityLog, 0)
	for rows.Next() {
		var item ActivityLog
		var dateStr string
		if err := rows.Scan(&item.Organization, &item.DeltaCount, &dateStr); err != nil {
			return nil, err
		}
		dateStr = strings.TrimSpace(dateStr)
		t, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			return nil, fmt.Errorf("activity log invalid activity_date %q: %w", dateStr, err)
		}
		item.ActivityDate = t
		out = append(out, item)
	}
	return out, rows.Err()
}

func loadActivityLogsForHandlers(ctx context.Context) ([]ActivityLog, error) {
	if err := ensureActivityLogBootstrap(ctx); err != nil {
		return nil, err
	}
	return fetchAllActivityLogs(ctx)
}

func computeStats(rows []Application, activityRows []ActivityLog) StatsResponse {
	stats := StatsResponse{
		StatusBreakdown:   map[string]int{},
		CategoryBreakdown: map[string]int{},
		Companies:         len(rows),
	}

	now := time.Now()
	loc := now.Location()
	nowLocal := now.In(loc)
	todayStart := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	weekday := int(nowLocal.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	weekStart := todayStart.AddDate(0, 0, -(weekday - 1))
	weekEnd := weekStart.AddDate(0, 0, 7)
	cutoff := now.AddDate(0, 0, -30)
	var lastDate time.Time

	for _, r := range rows {
		stats.Applications += r.Count

		statusKey := "(none)"
		if r.Status != nil && strings.TrimSpace(*r.Status) != "" {
			statusKey = strings.TrimSpace(*r.Status)
		}
		stats.StatusBreakdown[statusKey]++

		switch strings.ToLower(statusKey) {
		case "applied":
			stats.Applied++
			stats.AppliedApps += r.Count
		case "rejected":
			stats.Rejected++
			stats.RejectedApps += r.Count
		default:
			stats.Other++
			stats.OtherApps += r.Count
		}

		categoryKey := "(none)"
		if r.Category != nil && strings.TrimSpace(*r.Category) != "" {
			categoryKey = strings.TrimSpace(*r.Category)
		}
		stats.CategoryBreakdown[categoryKey]++

		if r.Count > stats.MaxPerCompany {
			stats.MaxPerCompany = r.Count
			stats.TopCompany = r.Organization
		}

		if r.AppliedDates != nil {
			if t, ok := parseAppliedDate(*r.AppliedDates); ok {
				if t.After(lastDate) {
					lastDate = t
					iso := t.Format("2006-01-02")
					stats.LastAppliedDate = &iso
				}
				if !t.Before(cutoff) {
					stats.Last30DaysCompanies++
					stats.Last30DaysApps += r.Count
				}
			}
		}
	}

	todayCompanies := map[string]struct{}{}
	weekCompanies := map[string]struct{}{}
	for _, a := range activityRows {
		activityDay := time.Date(a.ActivityDate.Year(), a.ActivityDate.Month(), a.ActivityDate.Day(), 0, 0, 0, 0, loc)
		orgKey := strings.ToLower(strings.TrimSpace(a.Organization))

		if !activityDay.Before(todayStart) && activityDay.Before(tomorrowStart) {
			stats.TodayApplications += a.DeltaCount
			if orgKey != "" {
				todayCompanies[orgKey] = struct{}{}
			}
		}
		if !activityDay.Before(weekStart) && activityDay.Before(weekEnd) {
			stats.WeekApplications += a.DeltaCount
			if orgKey != "" {
				weekCompanies[orgKey] = struct{}{}
			}
		}
	}
	stats.TodayCompanies = len(todayCompanies)
	stats.WeekCompanies = len(weekCompanies)

	if stats.Companies > 0 {
		stats.AppliedPct = float64(stats.Applied) / float64(stats.Companies) * 100
		stats.RejectedPct = float64(stats.Rejected) / float64(stats.Companies) * 100
		stats.AvgPerCompany = float64(stats.Applications) / float64(stats.Companies)
	}

	return stats
}

func handleApplicationsStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rows, err := fetchAllApplications(r.Context())
	if err != nil {
		log.Printf("tracker stats error: %v", err)
		respondJSON(w, http.StatusBadGateway, false, err.Error(), "")
		return
	}

	activityRows, actErr := loadActivityLogsForHandlers(r.Context())
	payload := map[string]interface{}{
		"success":             true,
		"stats":               computeStats(rows, activityRows),
		"activity_logs_ok":    actErr == nil,
		"activity_logs_error": "",
	}
	if actErr != nil {
		log.Printf("tracker stats activity: %v", actErr)
		payload["activity_logs_error"] = actErr.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

type TimelineBucket struct {
	Bucket       string `json:"bucket"`
	Companies    int    `json:"companies"`
	Applications int    `json:"applications"`
}

func bucketTimeline(activityRows []ActivityLog, freq string) []TimelineBucket {
	type counter struct {
		applications int
		companiesSet map[string]struct{}
	}
	bucketMap := map[string]*counter{}
	loc := time.Now().Location()

	for _, r := range activityRows {
		t := time.Date(r.ActivityDate.Year(), r.ActivityDate.Month(), r.ActivityDate.Day(), 0, 0, 0, 0, loc)

		var key string
		switch freq {
		case "day":
			key = t.Format("2006-01-02")
		case "week":
			weekday := int(t.Weekday())
			if weekday == 0 {
				weekday = 7
			}
			start := t.AddDate(0, 0, -(weekday - 1))
			key = start.Format("2006-01-02")
		case "month":
			key = t.Format("2006-01")
		}

		c, ok := bucketMap[key]
		if !ok {
			c = &counter{companiesSet: map[string]struct{}{}}
			bucketMap[key] = c
		}
		orgKey := strings.ToLower(strings.TrimSpace(r.Organization))
		if orgKey != "" {
			c.companiesSet[orgKey] = struct{}{}
		}
		c.applications += r.DeltaCount
	}

	keys := make([]string, 0, len(bucketMap))
	for k := range bucketMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]TimelineBucket, 0, len(keys))
	for _, k := range keys {
		out = append(out, TimelineBucket{
			Bucket:       k,
			Companies:    len(bucketMap[k].companiesSet),
			Applications: bucketMap[k].applications,
		})
	}
	return out
}

func handleApplicationsTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	freq := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("freq")))
	if freq == "" {
		freq = "month"
	}
	if freq != "day" && freq != "week" && freq != "month" {
		respondJSON(w, http.StatusBadRequest, false, "freq must be day, week, or month", "")
		return
	}

	activityRows, actErr := loadActivityLogsForHandlers(r.Context())
	if actErr != nil {
		log.Printf("tracker timeline activity: %v", actErr)
	}
	buckets := bucketTimeline(activityRows, freq)

	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"success":             true,
		"freq":                freq,
		"buckets":             buckets,
		"activity_logs_ok":    actErr == nil,
		"activity_logs_error": "",
	}
	if actErr != nil {
		payload["activity_logs_error"] = actErr.Error()
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// ContributionDay is one calendar day in a monthly applications heatmap.
type ContributionDay struct {
	Date         string `json:"date"`
	Applications int    `json:"applications"`
}

func aggregateApplicationsByLocalDay(rows []ActivityLog) map[string]int {
	loc := time.Now().Location()
	out := make(map[string]int)
	for _, r := range rows {
		key := time.Date(r.ActivityDate.Year(), r.ActivityDate.Month(), r.ActivityDate.Day(), 0, 0, 0, 0, loc).Format("2006-01-02")
		out[key] += r.DeltaCount
	}
	return out
}

func contributionMonthsList(counts map[string]int) []string {
	loc := time.Now().Location()
	now := time.Now().In(loc)
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

	var earliest *time.Time
	for k := range counts {
		d, err := time.ParseInLocation("2006-01-02", k, loc)
		if err != nil {
			continue
		}
		if earliest == nil || d.Before(*earliest) {
			dt := d
			earliest = &dt
		}
	}
	if earliest == nil {
		return []string{currentMonthStart.Format("2006-01")}
	}
	firstMonth := time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, loc)

	var months []string
	for cur := firstMonth; !cur.After(currentMonthStart); cur = cur.AddDate(0, 1, 0) {
		months = append(months, cur.Format("2006-01"))
	}
	return months
}

func handleApplicationsContribution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	monthParam := strings.TrimSpace(r.URL.Query().Get("month"))
	if monthParam == "" {
		respondJSON(w, http.StatusBadRequest, false, "month query param is required (YYYY-MM)", "")
		return
	}

	loc := time.Now().Location()
	monthStart, err := time.ParseInLocation("2006-01", monthParam, loc)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, false, "month must be YYYY-MM", "")
		return
	}

	activityRows, actErr := loadActivityLogsForHandlers(r.Context())
	if actErr != nil {
		log.Printf("tracker contribution activity: %v", actErr)
	}
	counts := aggregateApplicationsByLocalDay(activityRows)
	months := contributionMonthsList(counts)

	y, m, _ := monthStart.Date()
	lastOfMonth := time.Date(y, m+1, 0, 0, 0, 0, 0, loc)
	daysInMonth := lastOfMonth.Day()

	days := make([]ContributionDay, 0, daysInMonth)
	maxN := 0
	for d := 1; d <= daysInMonth; d++ {
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		key := day.Format("2006-01-02")
		n := counts[key]
		if n > maxN {
			maxN = n
		}
		days = append(days, ContributionDay{Date: key, Applications: n})
	}

	maxOut := maxN
	if maxOut == 0 {
		maxOut = 1
	}

	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"success":             true,
		"month":               monthParam,
		"days":                days,
		"max_applications":    maxOut,
		"months":              months,
		"activity_logs_ok":    actErr == nil,
		"activity_logs_error": "",
	}
	if actErr != nil {
		payload["activity_logs_error"] = actErr.Error()
	}
	_ = json.NewEncoder(w).Encode(payload)
}
