package tracker

import "time"

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

type ActivityLog struct {
	ID           int       `json:"id"`
	Organization string    `json:"organization"`
	DeltaCount   int       `json:"delta_count"`
	ActivityDate string    `json:"activity_date"`
	Action       string    `json:"action"`
	CreatedAt    time.Time `json:"created_at"`
}

type Stats struct {
	Companies         int     `json:"companies"`
	Applications      int     `json:"applications"`
	Applied           int     `json:"applied"`
	AppliedPct        float64 `json:"applied_pct"`
	Rejected          int     `json:"rejected"`
	RejectedPct       float64 `json:"rejected_pct"`
	TodayCompanies    int     `json:"today_companies"`
	TodayApplications int     `json:"today_applications"`
	WeekCompanies     int     `json:"week_companies"`
	WeekApplications  int     `json:"week_applications"`
	AvgPerCompany     float64 `json:"avg_per_company"`
	TopCompany        string  `json:"top_company"`
	MaxPerCompany     int     `json:"max_per_company"`
}

type UpsertResult struct {
	Action        string `json:"action"`
	Organization  string `json:"organization"`
	PreviousCount int    `json:"previous_count"`
	Added         int    `json:"added"`
	NewCount      int    `json:"new_count"`
}

type TimelineEntry struct {
	Date         string `json:"date"`
	Applications int    `json:"applications"`
	Companies    int    `json:"companies"`
	Action       string `json:"action"`
}

type ContributionDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	Level int    `json:"level"`
}

type QueryResult struct {
	SQL       string   `json:"sql"`
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated bool     `json:"truncated"`
}
