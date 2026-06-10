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
	TotalApplications int `json:"total_applications"`
	TotalCompanies    int `json:"total_companies"`
	TodayApplications int `json:"today_applications"`
	TodayCompanies    int `json:"today_companies"`
	WeekApplications  int `json:"week_applications"`
	WeekCompanies     int `json:"week_companies"`
}

type TimelineEntry struct {
	Date   string `json:"date"`
	Count  int    `json:"count"`
	Action string `json:"action"`
}

type ContributionDay struct {
	Date      string `json:"date"`
	Count     int    `json:"count"`
	Level     int    `json:"level"`
}

type QueryResult struct {
	SQL       string          `json:"sql"`
	Columns   []string        `json:"columns"`
	Rows      [][]any         `json:"rows"`
	RowCount  int             `json:"row_count"`
	Truncated bool            `json:"truncated"`
}