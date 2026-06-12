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
	rows, err := r.pool.Query(ctx, `SELECT id, label, content, category, sort_order, created_at FROM clipboard_items ORDER BY sort_order ASC, created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Label, &item.Content, &item.Category, &item.SortOrder, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) Create(ctx context.Context, item Item) error {
	var maxOrder int
	r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order), -1) FROM clipboard_items`).Scan(&maxOrder)
	item.SortOrder = maxOrder + 1
	_, err := r.pool.Exec(ctx,
		`INSERT INTO clipboard_items (id, label, content, category, sort_order, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		item.ID, item.Label, item.Content, item.Category, item.SortOrder, item.CreatedAt,
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
		`SELECT id, label, content, category, sort_order, created_at FROM clipboard_items
		WHERE label ILIKE $1 OR content ILIKE $1 OR category ILIKE $1
		ORDER BY sort_order ASC, created_at DESC`, q)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.Label, &item.Content, &item.Category, &item.SortOrder, &item.CreatedAt); err != nil {
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

func (r *Repository) Reorder(ctx context.Context, ids []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	for i, id := range ids {
		_, err := tx.Exec(ctx, `UPDATE clipboard_items SET sort_order = $1 WHERE id = $2`, i, id)
		if err != nil {
			return fmt.Errorf("reorder item %s: %w", id, err)
		}
	}
	return tx.Commit(ctx)
}