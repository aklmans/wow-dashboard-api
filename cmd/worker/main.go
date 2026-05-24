// Command worker is the background-job consumer process. It connects to the
// same Postgres database as the API, registers every River job type defined
// in internal/jobs, and runs the configured number of workers per queue.
// Deploy it alongside cmd/api as a separate process so worker capacity
// scales independently from HTTP capacity.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/jobs"
	"github.com/aklmans/wow-dashboard-api/internal/logging"
	"github.com/aklmans/wow-dashboard-api/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("worker: load config", "error", err)
		os.Exit(1)
	}
	logger := logging.NewLogger(cfg, os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		logger.Error("worker: connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	client, err := jobs.NewWorkerClient(pool, jobs.DefaultWorkerConfig(), jobs.RegisterAll)
	if err != nil {
		logger.Error("worker: build river client", "error", err)
		os.Exit(1)
	}

	if err := client.Start(ctx); err != nil {
		logger.Error("worker: start", "error", err)
		os.Exit(1)
	}
	logger.Info("worker started, awaiting jobs")

	<-ctx.Done()
	logger.Info("worker draining…")

	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := jobs.Stop(drainCtx, client); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker: stop", "error", err)
		os.Exit(1)
	}
	logger.Info("worker stopped")
}
