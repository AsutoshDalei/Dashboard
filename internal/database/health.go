package database

import (
	"context"
	"log/slog"
	"time"
)

type HealthStatus struct {
	Status    string `json:"status"`
	Latency   string `json:"latency,omitempty"`
	Error     string `json:"error,omitempty"`
}

func CheckHealth(ctx context.Context, pool *Pool) HealthStatus {
	start := time.Now()
	err := pool.Health(ctx)
	latency := time.Since(start).String()

	if err != nil {
		return HealthStatus{
			Status:  "unhealthy",
			Latency: latency,
			Error:   err.Error(),
		}
	}

	return HealthStatus{
		Status:  "healthy",
		Latency: latency,
	}
}

func WaitForPool(ctx context.Context, pool *Pool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := pool.Health(ctx); err == nil {
			return nil
		}
		slog.Warn("database not ready, retrying...")
		time.Sleep(time.Second)
	}
	return pool.Health(ctx)
}