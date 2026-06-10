package observability

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

type ctxKey int

const loggerCtxKey ctxKey = 1

func NewLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: l,
	})

	return slog.New(handler)
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return slog.Default()
	}
	if l, ok := ctx.Value(loggerCtxKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

type Metrics struct {
	startTime time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{startTime: time.Now()}
}

func (m *Metrics) Uptime() time.Duration {
	return time.Since(m.startTime)
}

type RequestStats struct {
	TotalRequests   int64            `json:"total_requests"`
	StatusCounts    map[int]int64    `json:"status_counts"`
	PathCounts      map[string]int64 `json:"path_counts"`
	UptimeSeconds   float64          `json:"uptime_seconds"`
}

type StatsCollector struct {
	mu            sync.Mutex
	totalRequests int64
	statusCounts  map[int]int64
	pathCounts    map[string]int64
	startTime     time.Time
}

func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		statusCounts: make(map[int]int64),
		pathCounts:   make(map[string]int64),
		startTime:    time.Now(),
	}
}

func (c *StatsCollector) Record(status int, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalRequests++
	c.statusCounts[status]++
	c.pathCounts[path]++
}

func (c *StatsCollector) Snapshot() RequestStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	statusCopy := make(map[int]int64, len(c.statusCounts))
	for k, v := range c.statusCounts {
		statusCopy[k] = v
	}
	pathCopy := make(map[string]int64, len(c.pathCounts))
	for k, v := range c.pathCounts {
		pathCopy[k] = v
	}

	return RequestStats{
		TotalRequests: c.totalRequests,
		StatusCounts:  statusCopy,
		PathCounts:    pathCopy,
		UptimeSeconds: time.Since(c.startTime).Seconds(),
	}
}