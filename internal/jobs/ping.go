package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// PingArgs is a no-op job used to validate the queue pipeline end to end.
// New job types should follow the same shape: an Args struct that names its
// Kind, and a Worker that embeds river.WorkerDefaults.
type PingArgs struct {
	Message string `json:"message"`
}

// Kind is the stable type name persisted to the river_job table. Renaming
// this is a breaking change for any jobs already queued under the old name.
func (PingArgs) Kind() string { return "ping" }

// PingWorker is the consumer side of PingArgs.
type PingWorker struct {
	river.WorkerDefaults[PingArgs]
}

func (*PingWorker) Work(ctx context.Context, job *river.Job[PingArgs]) error {
	slog.InfoContext(ctx, "ping job processed",
		"job_id", job.ID,
		"message", job.Args.Message,
		"attempt", job.Attempt,
	)
	return nil
}
