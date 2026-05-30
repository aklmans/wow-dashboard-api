package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// RetentionStore performs the data-retention purge deletes. The worker depends
// on this interface so it can be unit-tested without a database.
type RetentionStore interface {
	PurgeExpiredRefreshTokens(ctx context.Context) (int64, error)
	PurgeConsumedOrExpiredAuthTokens(ctx context.Context) (int64, error)
	PurgeSystemEventsBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// RetentionCleanupArgs is the payload for the periodic retention purge. It
// carries no fields — the job reads its retention window from the worker.
type RetentionCleanupArgs struct{}

// Kind is the persisted job type. Renaming strands jobs already queued under
// the old name; bump only with a migration plan.
func (RetentionCleanupArgs) Kind() string { return "retention_cleanup" }

// RetentionCleanupWorker deletes expired refresh tokens, consumed/expired auth
// tokens, and audit events older than the configured window, keeping those
// tables from growing without bound.
type RetentionCleanupWorker struct {
	river.WorkerDefaults[RetentionCleanupArgs]
	store           RetentionStore
	systemEventsTTL time.Duration
}

func (w *RetentionCleanupWorker) Work(ctx context.Context, _ *river.Job[RetentionCleanupArgs]) error {
	if w.store == nil {
		return fmt.Errorf("jobs: RetentionCleanupWorker has no retention store configured")
	}

	refreshDeleted, err := w.store.PurgeExpiredRefreshTokens(ctx)
	if err != nil {
		return err
	}
	authDeleted, err := w.store.PurgeConsumedOrExpiredAuthTokens(ctx)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-w.systemEventsTTL)
	eventsDeleted, err := w.store.PurgeSystemEventsBefore(ctx, cutoff)
	if err != nil {
		return err
	}

	slog.Default().InfoContext(ctx, "retention cleanup complete",
		"refresh_tokens_deleted", refreshDeleted,
		"auth_tokens_deleted", authDeleted,
		"system_events_deleted", eventsDeleted,
		"system_events_cutoff", cutoff,
	)
	return nil
}
