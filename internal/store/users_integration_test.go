//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
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

func TestUsersIntegration(t *testing.T) {
	ctx := context.Background()

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

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to open database connection for goose: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "../../migrations"); err != nil {
		t.Fatalf("goose.Up failed: %v", err)
	}

	cfg := &config.Config{
		DatabaseURL:              connStr,
		DBMaxConns:               5,
		DBMinConns:               1,
		DBMaxConnLifetimeSeconds: 1800,
		DBMaxConnIdleTimeSeconds: 300,
		DBHealthTimeoutSeconds:   5,
	}
	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	queries := query.New(pool)

	pgNow := func() pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: time.Now().UTC().Truncate(time.Microsecond), Valid: true}
	}
	pgID := func(t *testing.T) pgtype.UUID {
		t.Helper()
		var id pgtype.UUID
		if err := id.Scan(uuid.New().String()); err != nil {
			t.Fatalf("failed to scan user id: %v", err)
		}
		return id
	}

	// --- CreateUser ---
	t.Run("CreateUser", func(t *testing.T) {
		hash, err := password.Hash("test-password-123")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		created, err := queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgID(t),
			Email:        "Alice@Example.com", // mixed case to test lower()
			DisplayName:  "Alice Test",
			PasswordHash: hash,
			Status:       "active",
			CreatedAt:    pgNow(),
			UpdatedAt:    pgNow(),
		})
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}
		if created.Email != "alice@example.com" {
			t.Errorf("expected email to be lowercased, got %q", created.Email)
		}
		if created.DisplayName != "Alice Test" || created.Status != "active" {
			t.Errorf("created = %#v, want Alice Test / active", created)
		}
	})

	// --- GetUserByEmail (public projection, no password_hash) ---
	t.Run("GetUserByEmail", func(t *testing.T) {
		fetched, err := queries.GetUserByEmail(ctx, "Alice@Example.com") // mixed case lookup
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}
		if fetched.Email != "alice@example.com" || fetched.DisplayName != "Alice Test" {
			t.Errorf("fetched = %#v, want alice@example.com / Alice Test", fetched)
		}
	})

	// --- GetUserByEmailForAuth (auth projection, includes password_hash) ---
	t.Run("GetUserByEmailForAuth", func(t *testing.T) {
		fetched, err := queries.GetUserByEmailForAuth(ctx, "Alice@Example.com")
		if err != nil {
			t.Fatalf("GetUserByEmailForAuth failed: %v", err)
		}
		if fetched.Email != "alice@example.com" || fetched.PasswordHash == "" {
			t.Fatalf("auth projection = %#v, want email and password hash", fetched)
		}
		match, err := password.Verify("test-password-123", fetched.PasswordHash)
		if err != nil {
			t.Fatalf("password.Verify failed: %v", err)
		}
		if !match {
			t.Error("stored password hash should verify against original password")
		}
	})

	// --- GetUserByID (public projection) ---
	t.Run("GetUserByID", func(t *testing.T) {
		byEmail, err := queries.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}
		byID, err := queries.GetUserByID(ctx, byEmail.ID)
		if err != nil {
			t.Fatalf("GetUserByID failed: %v", err)
		}
		if byID.Email != byEmail.Email || byID.DisplayName != byEmail.DisplayName {
			t.Errorf("byID = %#v, want to match byEmail", byID)
		}
	})

	// --- ListUsers (public projection) ---
	t.Run("ListUsers", func(t *testing.T) {
		hash, err := password.Hash("another-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}
		if _, err := queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgID(t),
			Email:        "bob@example.com",
			DisplayName:  "Bob Test",
			PasswordHash: hash,
			Status:       "active",
			CreatedAt:    pgNow(),
			UpdatedAt:    pgNow(),
		}); err != nil {
			t.Fatalf("CreateUser (bob) failed: %v", err)
		}

		allUsers, err := queries.ListUsers(ctx, query.ListUsersParams{LimitVal: 10, OffsetVal: 0})
		if err != nil {
			t.Fatalf("ListUsers failed: %v", err)
		}
		if len(allUsers) != 2 {
			t.Errorf("expected 2 users, got %d", len(allUsers))
		}

		page1, err := queries.ListUsers(ctx, query.ListUsersParams{LimitVal: 1, OffsetVal: 0})
		if err != nil {
			t.Fatalf("ListUsers page 1 failed: %v", err)
		}
		page2, err := queries.ListUsers(ctx, query.ListUsersParams{LimitVal: 1, OffsetVal: 1})
		if err != nil {
			t.Fatalf("ListUsers page 2 failed: %v", err)
		}
		if len(page1) != 1 || len(page2) != 1 || page1[0].Email == page2[0].Email {
			t.Error("pages should each return a distinct user")
		}
	})

	// --- UpdateUserFields (status + profile, narg semantics) ---
	t.Run("UpdateUserFields", func(t *testing.T) {
		alice, err := queries.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}

		if err := queries.UpdateUserFields(ctx, query.UpdateUserFieldsParams{
			ID:        alice.ID,
			Status:    pgtype.Text{String: "disabled", Valid: true},
			JobTitle:  pgtype.Text{String: "Engineer", Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		}); err != nil {
			t.Fatalf("UpdateUserFields (disable) failed: %v", err)
		}

		updated, err := queries.GetUserByID(ctx, alice.ID)
		if err != nil {
			t.Fatalf("GetUserByID failed: %v", err)
		}
		if updated.Status != "disabled" {
			t.Errorf("expected status 'disabled', got %q", updated.Status)
		}
		if updated.JobTitle.String != "Engineer" {
			t.Errorf("expected job_title 'Engineer', got %q", updated.JobTitle.String)
		}
		if !updated.UpdatedAt.Time.After(alice.UpdatedAt.Time) {
			t.Error("expected updated_at to be refreshed after the update")
		}

		// A nil narg leaves the column unchanged: restore status without
		// passing job_title.
		if err := queries.UpdateUserFields(ctx, query.UpdateUserFieldsParams{
			ID:        alice.ID,
			Status:    pgtype.Text{String: "active", Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		}); err != nil {
			t.Fatalf("UpdateUserFields (restore) failed: %v", err)
		}
		restored, err := queries.GetUserByID(ctx, alice.ID)
		if err != nil {
			t.Fatalf("GetUserByID failed: %v", err)
		}
		if restored.Status != "active" {
			t.Errorf("restored status = %q, want active", restored.Status)
		}
		if restored.JobTitle.String != "Engineer" {
			t.Errorf("job_title should be unchanged by a nil narg, got %q", restored.JobTitle.String)
		}
	})

	// --- Constraint: duplicate email ---
	t.Run("DuplicateEmailRejected", func(t *testing.T) {
		hash, err := password.Hash("any-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}
		if _, err := queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgID(t),
			Email:        "alice@example.com",
			DisplayName:  "Alice Duplicate",
			PasswordHash: hash,
			Status:       "active",
			CreatedAt:    pgNow(),
			UpdatedAt:    pgNow(),
		}); err == nil {
			t.Fatal("expected error when creating user with duplicate email, got nil")
		}
	})

	// --- Constraint: invalid status ---
	t.Run("InvalidStatusRejected", func(t *testing.T) {
		hash, err := password.Hash("any-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}
		if _, err := queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgID(t),
			Email:        "invalid-status@example.com",
			DisplayName:  "Invalid Status User",
			PasswordHash: hash,
			Status:       "banned", // not in CHECK constraint
			CreatedAt:    pgNow(),
			UpdatedAt:    pgNow(),
		}); err == nil {
			t.Fatal("expected error when creating user with invalid status 'banned', got nil")
		}
	})
}
