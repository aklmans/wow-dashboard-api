//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMigrationsAndQueriesIntegration(t *testing.T) {
	ctx := context.Background()

	// Start real PostgreSQL container using testcontainers-go
	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("test_db"),
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
	defer func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate container: %v", err)
		}
	}()

	// Get connection string from container
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	// Open standard sql.DB for Goose migrations
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to open database connection for goose: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}

	// Run migrations up to build schema
	t.Log("Running migrations up...")
	if err := goose.Up(db, "../../migrations"); err != nil {
		t.Fatalf("goose.Up failed: %v", err)
	}

	for _, relation := range []string{
		"public.system_events",
		"public.users",
		"public.idx_system_events_created_at",
		"public.idx_system_events_event_type",
		"public.idx_projects_owner_name_unique",
	} {
		if !relationExists(t, db, relation) {
			t.Fatalf("expected relation %s to exist after goose.Up", relation)
		}
	}

	// Create config with connection URL and pool settings pointing to the container
	cfg := &config.Config{
		DatabaseURL:              connStr,
		DBMaxConns:               5,
		DBMinConns:               1,
		DBMaxConnLifetimeSeconds: 1800,
		DBMaxConnIdleTimeSeconds: 300,
		DBHealthTimeoutSeconds:   5,
	}

	// Initialize store connection pool
	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	poolClosed := false
	defer func() {
		if !poolClosed {
			pool.Close()
		}
	}()

	// Initialize SQLC generated queries
	queries := query.New(pool)

	// Create and insert a system event
	eventID := uuid.New()
	var pgEventID pgtype.UUID
	if err := pgEventID.Scan(eventID.String()); err != nil {
		t.Fatalf("failed to scan event ID into pgtype: %v", err)
	}

	metadataMap := map[string]interface{}{
		"source":  "integration_test",
		"version": 1.0,
	}
	metadataBytes, err := json.Marshal(metadataMap)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	var pgCreatedAt pgtype.Timestamptz
	if err := pgCreatedAt.Scan(now); err != nil {
		t.Fatalf("failed to scan created_at: %v", err)
	}

	t.Log("Inserting new system event...")
	inserted, err := queries.CreateSystemEvent(ctx, query.CreateSystemEventParams{
		ID:        pgEventID,
		EventType: "system.test",
		Message:   "Integration test verification of migrations and sqlc",
		Metadata:  metadataBytes,
		CreatedAt: pgCreatedAt,
	})
	if err != nil {
		t.Fatalf("CreateSystemEvent failed: %v", err)
	}

	if inserted.EventType != "system.test" {
		t.Errorf("expected EventType 'system.test', got %q", inserted.EventType)
	}

	// Retrieve event back and verify contents
	t.Log("Retrieving system event back...")
	fetched, err := queries.GetSystemEvent(ctx, pgEventID)
	if err != nil {
		t.Fatalf("GetSystemEvent failed: %v", err)
	}

	if fetched.Message != "Integration test verification of migrations and sqlc" {
		t.Errorf("expected Message 'Integration test verification of migrations and sqlc', got %q", fetched.Message)
	}
	if fetched.ID.Bytes != eventID {
		t.Errorf("expected ID %s, got %s", eventID, fetched.ID.Bytes)
	}

	// List system events with limit
	t.Log("Listing system events...")
	list, err := queries.ListSystemEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListSystemEvents failed: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("expected list length 1, got %d", len(list))
	}
	if list[0].ID.Bytes != eventID {
		t.Errorf("expected listed item to have ID %s, got %s", eventID, list[0].ID.Bytes)
	}

	pool.Close()
	poolClosed = true

	t.Log("Running migrations down to version 0...")
	if err := goose.DownTo(db, "../../migrations", 0); err != nil {
		t.Fatalf("goose.DownTo failed: %v", err)
	}
	version, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("goose.GetDBVersion failed after DownTo: %v", err)
	}
	if version != 0 {
		t.Fatalf("goose version = %d after DownTo, want 0", version)
	}
	for _, relation := range []string{
		"public.system_events",
		"public.users",
		"public.idx_system_events_created_at",
		"public.idx_system_events_event_type",
		"public.idx_projects_owner_name_unique",
	} {
		if relationExists(t, db, relation) {
			t.Fatalf("expected relation %s to be dropped after goose.DownTo(0)", relation)
		}
	}
}

func relationExists(t *testing.T, db *sql.DB, relation string) bool {
	t.Helper()

	var exists bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, relation).Scan(&exists); err != nil {
		t.Fatalf("failed to check relation %s: %v", relation, err)
	}
	return exists
}
