// Package observability holds cross-cutting telemetry helpers (tracing and
// Prometheus collectors) shared by the API and worker processes.
package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	dbPoolConnections = prometheus.NewDesc(
		"db_pool_connections",
		"Connections in the pgx pool by state (total, idle, acquired, constructing, max).",
		[]string{"state"}, nil,
	)
	dbPoolAcquireTotal = prometheus.NewDesc(
		"db_pool_acquire_total",
		"Cumulative count of successful connection acquisitions from the pool.",
		nil, nil,
	)
	dbPoolEmptyAcquireTotal = prometheus.NewDesc(
		"db_pool_empty_acquire_total",
		"Cumulative count of acquisitions that had to wait for a connection (a saturation signal).",
		nil, nil,
	)
	dbPoolCanceledAcquireTotal = prometheus.NewDesc(
		"db_pool_canceled_acquire_total",
		"Cumulative count of acquisitions canceled by a context before completing.",
		nil, nil,
	)
)

// PgxPoolCollector exports pgxpool runtime statistics as Prometheus metrics so
// pool saturation (e.g. rising empty-acquire counts, idle near zero) is visible
// and alertable.
type PgxPoolCollector struct {
	pool *pgxpool.Pool
}

// NewPgxPoolCollector builds a collector over the given pool.
func NewPgxPoolCollector(pool *pgxpool.Pool) *PgxPoolCollector {
	return &PgxPoolCollector{pool: pool}
}

func (c *PgxPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- dbPoolConnections
	ch <- dbPoolAcquireTotal
	ch <- dbPoolEmptyAcquireTotal
	ch <- dbPoolCanceledAcquireTotal
}

func (c *PgxPoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c == nil || c.pool == nil {
		return
	}
	s := c.pool.Stat()
	gauge := func(state string, v float64) {
		ch <- prometheus.MustNewConstMetric(dbPoolConnections, prometheus.GaugeValue, v, state)
	}
	gauge("total", float64(s.TotalConns()))
	gauge("idle", float64(s.IdleConns()))
	gauge("acquired", float64(s.AcquiredConns()))
	gauge("constructing", float64(s.ConstructingConns()))
	gauge("max", float64(s.MaxConns()))

	ch <- prometheus.MustNewConstMetric(dbPoolAcquireTotal, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(dbPoolEmptyAcquireTotal, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(dbPoolCanceledAcquireTotal, prometheus.CounterValue, float64(s.CanceledAcquireCount()))
}

// riverJobsByState is a gauge of background jobs grouped by River state, so a
// growing backlog (state="available") or dead-letter pile (state="discarded")
// can be alerted on.
var riverJobsByState = prometheus.NewDesc(
	"river_jobs",
	"Background jobs by River state (available, running, completed, retryable, scheduled, discarded, cancelled, pending).",
	[]string{"state"}, nil,
)

// RiverQueueCollector reports job counts per River state by querying the
// river_job table on the shared pool. River creates that table through its own
// migration tool (cmd/river-migrate), separate from the goose migrations, so
// the collector degrades to emitting nothing when the table is absent rather
// than failing a scrape.
type RiverQueueCollector struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewRiverQueueCollector builds a collector over the given pool.
func NewRiverQueueCollector(pool *pgxpool.Pool, logger *slog.Logger) *RiverQueueCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &RiverQueueCollector{pool: pool, logger: logger}
}

func (c *RiverQueueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- riverJobsByState
}

func (c *RiverQueueCollector) Collect(ch chan<- prometheus.Metric) {
	if c == nil || c.pool == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rows, err := c.pool.Query(ctx, "SELECT state, count(*) FROM river_job GROUP BY state")
	if err != nil {
		// Most commonly the river_job table does not exist yet (River migrations
		// not run). Emit nothing rather than failing the scrape.
		c.logger.DebugContext(ctx, "river queue metrics unavailable", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var state string
		var count float64
		if err := rows.Scan(&state, &count); err != nil {
			c.logger.DebugContext(ctx, "river queue metrics scan failed", "error", err)
			return
		}
		ch <- prometheus.MustNewConstMetric(riverJobsByState, prometheus.GaugeValue, count, state)
	}
}
