package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	*pgxpool.Pool
}

type Config struct {
	DSN          string
	MaxConns     int32
	MinConns     int32
	MaxIdleTime  time.Duration
	MaxLifetime  time.Duration
}

func DefaultConfig(dsn string) Config {
	return Config{
		DSN:         dsn,
		MaxConns:    4,
		MinConns:    1,
		MaxIdleTime: 5 * time.Minute,
		MaxLifetime: time.Hour,
	}
}

func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnIdleTime = cfg.MaxIdleTime
	pcfg.MaxConnLifetime = cfg.MaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

func (p *Pool) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return p.Pool.Ping(ctx)
}