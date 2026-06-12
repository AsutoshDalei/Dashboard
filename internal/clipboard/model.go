package clipboard

import "time"

type Item struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	SortOrder int       `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}