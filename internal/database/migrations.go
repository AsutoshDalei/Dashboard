package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Migration struct {
	Name string
	SQL  string
}

var migrations = []Migration{
	{
		Name: "001_applications",
		SQL: `CREATE TABLE IF NOT EXISTS applications (
			id                SERIAL PRIMARY KEY,
			organization      VARCHAR(255),
			job_role          TEXT,
			location          VARCHAR(255),
			contacts          VARCHAR(255),
			applied_dates     DATE,
			remarks           TEXT,
			status            VARCHAR(50),
			category          VARCHAR(100),
			count             INTEGER,
			username_password TEXT,
			created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		Name: "002_application_activity_logs",
		SQL: `CREATE TABLE IF NOT EXISTS application_activity_logs (
			id SERIAL PRIMARY KEY,
			organization VARCHAR(255) NOT NULL,
			delta_count INTEGER NOT NULL DEFAULT 0,
			activity_date DATE NOT NULL,
			action VARCHAR(32),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	},
	{
		Name: "003_clipboard_items",
		SQL: `CREATE TABLE IF NOT EXISTS clipboard_items (
			id UUID PRIMARY KEY,
			label TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,
	},
	{
		Name: "004_clipboard_sort_order",
		SQL: `DO $$ BEGIN
			ALTER TABLE clipboard_items ADD COLUMN sort_order INTEGER DEFAULT 0;
		EXCEPTION WHEN duplicate_column THEN null;
		END $$`,
	},
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	for _, m := range migrations {
		slog.Info("running migration", "name", m.Name)
		if _, err := pool.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	slog.Info("migrations complete", "count", len(migrations))
	return nil
}