// Command river-migrate runs the River background-job-queue schema migrations
// against DATABASE_URL. River owns its own schema (river_job, river_leader,
// etc.) and ships the migrations programmatically; this binary just calls
// them so an operator does not need the standalone river CLI installed.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/river-migrate           # up
//	DATABASE_URL=postgres://... go run ./cmd/river-migrate down      # down by one
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

func main() {
	flag.Parse()
	direction := rivermigrate.DirectionUp
	maxSteps := 0
	switch flag.Arg(0) {
	case "", "up":
		direction = rivermigrate.DirectionUp
	case "down":
		direction = rivermigrate.DirectionDown
		if s := flag.Arg(1); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				log.Fatalf("river-migrate: down argument must be a number, got %q", s)
			}
			maxSteps = n
		} else {
			maxSteps = 1
		}
	default:
		log.Fatalf("river-migrate: unknown direction %q (use 'up' or 'down')", flag.Arg(0))
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("river-migrate: DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatalf("river-migrate: connect to database: %v", err)
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		log.Fatalf("river-migrate: build migrator: %v", err)
	}

	res, err := migrator.Migrate(ctx, direction, &rivermigrate.MigrateOpts{
		MaxSteps: maxSteps,
	})
	if err != nil {
		log.Fatalf("river-migrate: %s failed: %v", direction, err)
	}

	if len(res.Versions) == 0 {
		slog.Info(fmt.Sprintf("river schema is already at the target version (%s)", direction))
		return
	}
	for _, v := range res.Versions {
		slog.Info("river migration applied",
			"direction", string(direction),
			"version", v.Version,
			"name", v.Name,
			"duration", v.Duration,
		)
	}
}
