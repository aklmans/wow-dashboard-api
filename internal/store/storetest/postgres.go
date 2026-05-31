//go:build integration

package storetest

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func NewPostgresPool(t testing.TB, ctx context.Context, database string, migrationsDir string) *pgxpool.Pool {
	t.Helper()

	pgContainer, err := postgres.Run(ctx,
		"postgres:17.5-alpine",
		postgres.WithDatabase(database),
		postgres.WithUsername("test_user"),
		postgres.WithPassword("test_password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to open database for migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close goose db: %v", err)
		}
	})

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	if err := goose.Up(db, migrationsDir); err != nil {
		t.Fatalf("goose.Up failed: %v", err)
	}

	pool, err := store.NewPool(ctx, &config.Config{
		DatabaseURL:              connStr,
		DBMaxConns:               5,
		DBMinConns:               1,
		DBMaxConnLifetimeSeconds: 1800,
		DBMaxConnIdleTimeSeconds: 300,
		DBHealthTimeoutSeconds:   5,
	})
	if err != nil {
		t.Fatalf("failed to initialize store pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
