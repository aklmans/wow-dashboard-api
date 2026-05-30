// Package jobs wires the River background-job queue (Postgres-backed) into
// the application. The HTTP process inserts jobs via Client; cmd/worker
// consumes them via NewWorkerClient. Both share the same Postgres pool, so
// no extra infrastructure beyond the database is required.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/aklmans/wow-dashboard-api/internal/email"
)

// QueueDefault is the queue used by every job registered here unless a caller
// opts into a different queue via river.InsertOpts.
const QueueDefault = river.QueueDefault

// retentionCleanupInterval is how often the data-retention purge runs.
const retentionCleanupInterval = time.Hour

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
		Logger:       slog.Default(),
		ErrorHandler: jobErrorHandler{},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(retentionCleanupInterval),
				func() (river.JobArgs, *river.InsertOpts) { return RetentionCleanupArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
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

// Dependencies bundles the runtime collaborators a worker may need. Add
// fields as new job types come online; nil fields are tolerated so cmd/worker
// can boot in degraded mode (e.g. without an SMTP sender for the email job).
type Dependencies struct {
	EmailSender email.Sender
	// Retention runs the data-retention purge for the periodic cleanup job.
	Retention RetentionStore
	// SystemEventsRetention is how long audit events are kept before purging.
	SystemEventsRetention time.Duration
}

// RegisterAll registers every job type defined in this package. cmd/worker
// passes this to NewWorkerClient so adding a new job type only requires a
// new file plus a one-line addition here.
func RegisterAll(workers *river.Workers, deps Dependencies) {
	river.AddWorker(workers, &PingWorker{})
	river.AddWorker(workers, &SendEmailWorker{sender: deps.EmailSender})
	river.AddWorker(workers, &RetentionCleanupWorker{
		store:           deps.Retention,
		systemEventsTTL: deps.SystemEventsRetention,
	})
}

// Stop gracefully drains in-flight jobs and shuts down the client.
func Stop(ctx context.Context, client *river.Client[pgx.Tx]) error {
	if client == nil {
		return nil
	}
	return client.Stop(ctx)
}

// jobErrorHandler logs job failures. A job that has exhausted its attempts is
// about to be discarded (River's dead-letter equivalent), so it is logged at
// ERROR for alerting; transient failures that will retry are logged at WARN.
type jobErrorHandler struct{}

func (jobErrorHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	attrs := []any{
		"kind", job.Kind,
		"job_id", job.ID,
		"queue", job.Queue,
		"attempt", job.Attempt,
		"max_attempts", job.MaxAttempts,
		"error", err,
	}
	if job.Attempt >= job.MaxAttempts {
		slog.Default().ErrorContext(ctx, "river job discarded after exhausting attempts", attrs...)
	} else {
		slog.Default().WarnContext(ctx, "river job failed; will retry", attrs...)
	}
	return nil
}

func (jobErrorHandler) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, _ string) *river.ErrorHandlerResult {
	slog.Default().ErrorContext(ctx, "river job panicked",
		"kind", job.Kind,
		"job_id", job.ID,
		"panic", fmt.Sprintf("%v", panicVal),
	)
	return nil
}
