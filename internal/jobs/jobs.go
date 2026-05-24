// Package jobs wires the River background-job queue (Postgres-backed) into
// the application. The HTTP process inserts jobs via Client; cmd/worker
// consumes them via NewWorkerClient. Both share the same Postgres pool, so
// no extra infrastructure beyond the database is required.
package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// QueueDefault is the queue used by every job registered here unless a caller
// opts into a different queue via river.InsertOpts.
const QueueDefault = river.QueueDefault

// NewInsertOnlyClient returns a River client suited to the API process: it
// holds an insert-only handle so application code can enqueue jobs without
// spinning up workers in the same process.
func NewInsertOnlyClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: slog.Default(),
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: new insert-only client: %w", err)
	}
	return client, nil
}

// WorkerConfig captures the queue layout and concurrency for the worker
// process. Defaults are sized for a small team and can be overridden per
// deployment.
type WorkerConfig struct {
	// MaxWorkers is the per-queue worker concurrency.
	MaxWorkers int
}

// DefaultWorkerConfig returns a sensible starting point for a single-instance
// worker deployment.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{MaxWorkers: 5}
}

// NewWorkerClient returns a fully-wired client that both inserts and consumes
// jobs. Used by cmd/worker.
func NewWorkerClient(pool *pgxpool.Pool, cfg WorkerConfig, registerWorkers func(*river.Workers)) (*river.Client[pgx.Tx], error) {
	if cfg.MaxWorkers <= 0 {
		cfg = DefaultWorkerConfig()
	}

	workers := river.NewWorkers()
	if registerWorkers != nil {
		registerWorkers(workers)
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: slog.Default(),
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: cfg.MaxWorkers},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: new worker client: %w", err)
	}
	return client, nil
}

// RegisterAll registers every job type defined in this package. cmd/worker
// passes this to NewWorkerClient so adding a new job type only requires a
// new file plus a one-line addition here.
func RegisterAll(workers *river.Workers) {
	river.AddWorker(workers, &PingWorker{})
}

// Stop gracefully drains in-flight jobs and shuts down the client.
func Stop(ctx context.Context, client *river.Client[pgx.Tx]) error {
	if client == nil {
		return nil
	}
	return client.Stop(ctx)
}
