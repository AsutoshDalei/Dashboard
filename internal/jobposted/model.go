package jobposted

type CheckRequest struct {
	URL string `json:"url"`
}

type CheckResponse struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Data    *PostedDateData `json:"data,omitempty"`
}

type PostedDateData struct {
	URL                   string         `json:"url"`
	NormalizedURL         string         `json:"normalized_url"`
	MostProbableDate      string         `json:"most_probable_date"`
	Explanation           string         `json:"explanation"`
	Sources               *APISources    `json:"sources"`
	JobTitle              string         `json:"job_title"`
	Company               string         `json:"company"`
	HiddenInsights        []HiddenInsight `json:"hidden_insights"`
	ATSDetected           string         `json:"ats_detected"`
	Supported             bool           `json:"supported"`
	Reason                string         `json:"reason"`
	Suggestion            string         `json:"suggestion"`
	Confidence            string         `json:"confidence"`
	LinkedInComparisonDate *string       `json:"linkedin_comparison_date"`
	LinkedInComparisonText *string       `json:"linkedin_comparison_text"`
	Tier2ATSName          *string        `json:"tier2_ats_name"`
	Tier2ATSJobURL        *string        `json:"tier2_ats_job_url"`
	LinkedInPrecisionDays *int           `json:"linkedin_precision_days"`
	Cached                bool           `json:"cached"`
	CachedAt              string         `json:"cached_at"`
}

type APISources struct {
	ATSAPI    *ATSSource `json:"ats_api"`
	JSONLD    *string    `json:"json_ld"`
	Regex     *string    `json:"regex"`
	OpenGraph *string    `json:"open_graph"`
	Updated   *string    `json:"updated"`
	Sitemap   *string    `json:"sitemap"`
	Wayback   *string    `json:"wayback"`
}

type ATSSource struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

type HiddenInsight struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
