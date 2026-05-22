package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/seed"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load environment configuration: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize database pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	demoUser, err := seed.SeedDemoUser(ctx, query.New(pool))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed demo user: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Seeded demo user %s (%s)\n", demoUser.Email, demoUser.ID)
}
