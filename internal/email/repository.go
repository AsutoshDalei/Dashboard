package email

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type EmailRecord struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Template string    `json:"template"`
	SentAt   time.Time `json:"sent_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*EmailRecord, error) {
	var rec EmailRecord
	err := r.pool.QueryRow(ctx,
		`SELECT id, recipient_name, recipient_email, template_key, sent_at
		 FROM emails WHERE recipient_email = $1
		 ORDER BY sent_at DESC LIMIT 1`, email,
	).Scan(&rec.ID, &rec.Name, &rec.Email, &rec.Template, &rec.SentAt)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) RecordSend(ctx context.Context, name, email, templateKey string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO emails (recipient_name, recipient_email, template_key, sent_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (recipient_email) DO UPDATE
		 SET recipient_name = EXCLUDED.recipient_name,
		     template_key = EXCLUDED.template_key,
		     sent_at = NOW()`, name, email, templateKey,
	)
	if err != nil {
		return fmt.Errorf("record send: %w", err)
	}
	return nil
}
