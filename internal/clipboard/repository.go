package clipboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context) ([]Item, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, label, content, category, created_at FROM clipboard_items ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Label, &item.Content, &item.Category, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) Create(ctx context.Context, item Item) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO clipboard_items (id, label, content, category, created_at) VALUES ($1, $2, $3, $4, $5)`,
		item.ID, item.Label, item.Content, item.Category, item.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM clipboard_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("item not found")
	}
	return nil
}

func (r *Repository) Search(ctx context.Context, query string) ([]Item, error) {
	q := "%" + strings.TrimSpace(query) + "%"
	rows, err := r.pool.Query(ctx,
		`SELECT id, label, content, category, created_at FROM clipboard_items
		WHERE label ILIKE $1 OR content ILIKE $1 OR category ILIKE $1
		ORDER BY created_at DESC`, q)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Label, &item.Content, &item.Category, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) ImportFromJSON(ctx context.Context, items []Item) error {
	for _, item := range items {
		if err := r.Create(ctx, item); err != nil {
			return err
		}
	}
	return nil
}