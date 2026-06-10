package clipboard

import "time"

type Item struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
}