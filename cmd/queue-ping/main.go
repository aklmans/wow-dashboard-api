// Command queue-ping enqueues a single PingArgs job to validate the worker
// pipeline end to end. Run cmd/worker in another terminal to observe the
// job being processed.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/jobs"
)

func main() {
	msg := "hello from queue-ping"
	if len(os.Args) > 1 {
		msg = strings.Join(os.Args[1:], " ")
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("queue-ping: DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("queue-ping: connect to database: %v", err)
	}
	defer pool.Close()

	client, err := jobs.NewInsertOnlyClient(pool)
	if err != nil {
		log.Fatalf("queue-ping: build river client: %v", err)
	}

	result, err := client.Insert(ctx, jobs.PingArgs{Message: msg}, nil)
	if err != nil {
		log.Fatalf("queue-ping: insert: %v", err)
	}
	slog.Info("ping job enqueued", "job_id", result.Job.ID, "message", msg)
}
