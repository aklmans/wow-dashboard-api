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
	defer pool.Close()

	// Initialize SQLC generated queries
	queries := query.New(pool)

	// --- CreateUser ---
	t.Run("CreateUser", func(t *testing.T) {
		userID := uuid.New()
		var pgUserID pgtype.UUID
		if err := pgUserID.Scan(userID.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash, err := password.Hash("test-password-123")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		var pgNow pgtype.Timestamptz
		if err := pgNow.Scan(now); err != nil {
			t.Fatalf("failed to scan timestamp: %v", err)
		}

		created, err := queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgUserID,
			Email:        "Alice@Example.com", // deliberately mixed case to test lower()
			DisplayName:  "Alice Test",
			PasswordHash: hash,
			Status:       "active",
			Role:         "admin",
			CreatedAt:    pgNow,
			UpdatedAt:    pgNow,
		})
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}

		// CreateUserRow is a public projection — verify it does not expose password_hash.
		// The generated CreateUserRow type has no PasswordHash field; this test
		// confirms the public fields are correct.
		if created.Email != "alice@example.com" {
			t.Errorf("expected email to be lowercased, got %q", created.Email)
		}
		if created.DisplayName != "Alice Test" {
			t.Errorf("expected display_name 'Alice Test', got %q", created.DisplayName)
		}
		if created.Status != "active" {
			t.Errorf("expected status 'active', got %q", created.Status)
		}
		if created.Role != "admin" {
			t.Errorf("expected role 'admin', got %q", created.Role)
		}
	})

	t.Run("RoleDefault", func(t *testing.T) {
		userID := uuid.New()
		var pgUserID pgtype.UUID
		if err := pgUserID.Scan(userID.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash, err := password.Hash("default-role-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO users (id, email, display_name, password_hash, status)
			VALUES ($1, lower($2), $3, $4, $5)
		`, pgUserID, "default-role@example.com", "Default Role", hash, "active")
		if err != nil {
			t.Fatalf("insert user without explicit role failed: %v", err)
		}

		fetched, err := queries.GetUserByEmail(ctx, "default-role@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail default-role failed: %v", err)
		}
		if fetched.Role != "user" {
			t.Errorf("default role = %q, want user", fetched.Role)
		}
	})

	// --- GetUserByEmail (public projection, no password_hash) ---
	t.Run("GetUserByEmail", func(t *testing.T) {
		fetched, err := queries.GetUserByEmail(ctx, "Alice@Example.com") // mixed case lookup
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}

		// GetUserByEmailRow is a public projection — no PasswordHash field.
		if fetched.Email != "alice@example.com" {
			t.Errorf("expected email 'alice@example.com', got %q", fetched.Email)
		}
		if fetched.DisplayName != "Alice Test" {
			t.Errorf("expected display_name 'Alice Test', got %q", fetched.DisplayName)
		}
		if fetched.Role != "admin" {
			t.Errorf("expected role 'admin', got %q", fetched.Role)
		}
	})

	// --- GetUserByEmailForAuth (auth projection, includes password_hash) ---
	t.Run("GetUserByEmailForAuth", func(t *testing.T) {
		fetched, err := queries.GetUserByEmailForAuth(ctx, "Alice@Example.com") // mixed case
		if err != nil {
			t.Fatalf("GetUserByEmailForAuth failed: %v", err)
		}

		if fetched.Email != "alice@example.com" {
			t.Errorf("expected email 'alice@example.com', got %q", fetched.Email)
		}
		if fetched.DisplayName != "Alice Test" {
			t.Errorf("expected display_name 'Alice Test', got %q", fetched.DisplayName)
		}
		if fetched.Role != "admin" {
			t.Errorf("expected role 'admin', got %q", fetched.Role)
		}
		if fetched.PasswordHash == "" {
			t.Fatal("expected PasswordHash to be populated in auth projection")
		}

		// Verify the stored hash matches the original password
		match, err := password.Verify("test-password-123", fetched.PasswordHash)
		if err != nil {
			t.Fatalf("password.Verify failed: %v", err)
		}
		if !match {
			t.Error("stored password hash should verify against original password")
		}

		// Wrong password should not match
		match, err = password.Verify("wrong-password", fetched.PasswordHash)
		if err != nil {
			t.Fatalf("password.Verify returned unexpected error: %v", err)
		}
		if match {
			t.Error("wrong password should not verify against stored hash")
		}
	})

	// --- GetUserByID (public projection) ---
	t.Run("GetUserByID", func(t *testing.T) {
		// First get the user by email to obtain their ID
		byEmail, err := queries.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}

		byID, err := queries.GetUserByID(ctx, byEmail.ID)
		if err != nil {
			t.Fatalf("GetUserByID failed: %v", err)
		}

		// GetUserByIDRow is a public projection — no PasswordHash field.
		if byID.Email != byEmail.Email {
			t.Errorf("expected email %q, got %q", byEmail.Email, byID.Email)
		}
		if byID.DisplayName != byEmail.DisplayName {
			t.Errorf("expected display_name %q, got %q", byEmail.DisplayName, byID.DisplayName)
		}
		if byID.Role != byEmail.Role {
			t.Errorf("expected role %q, got %q", byEmail.Role, byID.Role)
		}
	})

	// --- ListUsers (public projection) ---
	t.Run("ListUsers", func(t *testing.T) {
		// Create a second user for pagination testing
		userID2 := uuid.New()
		var pgUserID2 pgtype.UUID
		if err := pgUserID2.Scan(userID2.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash2, err := password.Hash("another-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		var pgNow pgtype.Timestamptz
		if err := pgNow.Scan(now); err != nil {
			t.Fatalf("failed to scan timestamp: %v", err)
		}

		_, err = queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgUserID2,
			Email:        "bob@example.com",
			DisplayName:  "Bob Test",
			PasswordHash: hash2,
			Status:       "active",
			Role:         "user",
			CreatedAt:    pgNow,
			UpdatedAt:    pgNow,
		})
		if err != nil {
			t.Fatalf("CreateUser (bob) failed: %v", err)
		}

		// List all users — ListUsersRow is a public projection, no PasswordHash.
		allUsers, err := queries.ListUsers(ctx, query.ListUsersParams{
			LimitVal:  10,
			OffsetVal: 0,
		})
		if err != nil {
			t.Fatalf("ListUsers failed: %v", err)
		}

		if len(allUsers) != 3 {
			t.Errorf("expected 3 users, got %d", len(allUsers))
		}
		for _, user := range allUsers {
			if user.Role != "admin" && user.Role != "user" {
				t.Errorf("listed user role = %q, want admin or user", user.Role)
			}
		}

		// Test pagination: LIMIT 1 OFFSET 0 should return 1 user
		page1, err := queries.ListUsers(ctx, query.ListUsersParams{
			LimitVal:  1,
			OffsetVal: 0,
		})
		if err != nil {
			t.Fatalf("ListUsers page 1 failed: %v", err)
		}
		if len(page1) != 1 {
			t.Errorf("expected 1 user in page 1, got %d", len(page1))
		}

		// LIMIT 1 OFFSET 1 should return the other user
		page2, err := queries.ListUsers(ctx, query.ListUsersParams{
			LimitVal:  1,
			OffsetVal: 1,
		})
		if err != nil {
			t.Fatalf("ListUsers page 2 failed: %v", err)
		}
		if len(page2) != 1 {
			t.Errorf("expected 1 user in page 2, got %d", len(page2))
		}

		// The two pages should contain different users
		if page1[0].Email == page2[0].Email {
			t.Error("page 1 and page 2 should contain different users")
		}
	})

	// --- UpdateUserStatus (public projection) ---
	t.Run("UpdateUser", func(t *testing.T) {
		alice, err := queries.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail failed: %v", err)
		}

		// Status-only update: role is omitted (NULL) so COALESCE keeps it.
		updated, err := queries.UpdateUser(ctx, query.UpdateUserParams{
			ID:        alice.ID,
			Status:    pgtype.Text{String: "disabled", Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			t.Fatalf("UpdateUser (disable) failed: %v", err)
		}
		if updated.Status != "disabled" {
			t.Errorf("expected status 'disabled', got %q", updated.Status)
		}
		if updated.Role != "admin" {
			t.Errorf("expected role to stay 'admin' on a status-only update, got %q", updated.Role)
		}
		if !updated.UpdatedAt.Time.After(alice.UpdatedAt.Time) {
			t.Error("expected updated_at to be refreshed after the update")
		}

		// Role-only update: status is omitted (NULL) so COALESCE keeps it.
		demoted, err := queries.UpdateUser(ctx, query.UpdateUserParams{
			ID:        alice.ID,
			Role:      pgtype.Text{String: "user", Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			t.Fatalf("UpdateUser (demote) failed: %v", err)
		}
		if demoted.Role != "user" {
			t.Errorf("expected role 'user', got %q", demoted.Role)
		}
		if demoted.Status != "disabled" {
			t.Errorf("expected status to stay 'disabled' on a role-only update, got %q", demoted.Status)
		}

		// Restore alice to an active admin for any later subtests.
		restored, err := queries.UpdateUser(ctx, query.UpdateUserParams{
			ID:        alice.ID,
			Role:      pgtype.Text{String: "admin", Valid: true},
			Status:    pgtype.Text{String: "active", Valid: true},
			UpdatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		if err != nil {
			t.Fatalf("UpdateUser (restore) failed: %v", err)
		}
		if restored.Role != "admin" || restored.Status != "active" {
			t.Errorf("restore = role %q status %q, want admin/active", restored.Role, restored.Status)
		}
	})

	// --- Constraint: duplicate email ---
	t.Run("DuplicateEmailRejected", func(t *testing.T) {
		userID := uuid.New()
		var pgUserID pgtype.UUID
		if err := pgUserID.Scan(userID.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash, err := password.Hash("any-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		var pgNow pgtype.Timestamptz
		if err := pgNow.Scan(now); err != nil {
			t.Fatalf("failed to scan timestamp: %v", err)
		}

		// Attempt to create a user with an existing email (should fail)
		_, err = queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgUserID,
			Email:        "alice@example.com",
			DisplayName:  "Alice Duplicate",
			PasswordHash: hash,
			Status:       "active",
			Role:         "user",
			CreatedAt:    pgNow,
			UpdatedAt:    pgNow,
		})
		if err == nil {
			t.Fatal("expected error when creating user with duplicate email, got nil")
		}
	})

	// --- Constraint: invalid status ---
	t.Run("InvalidStatusRejected", func(t *testing.T) {
		userID := uuid.New()
		var pgUserID pgtype.UUID
		if err := pgUserID.Scan(userID.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash, err := password.Hash("any-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		var pgNow pgtype.Timestamptz
		if err := pgNow.Scan(now); err != nil {
			t.Fatalf("failed to scan timestamp: %v", err)
		}

		// Attempt to create a user with invalid status (should fail)
		_, err = queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgUserID,
			Email:        "invalid-status@example.com",
			DisplayName:  "Invalid Status User",
			PasswordHash: hash,
			Status:       "banned", // not in CHECK constraint
			Role:         "user",
			CreatedAt:    pgNow,
			UpdatedAt:    pgNow,
		})
		if err == nil {
			t.Fatal("expected error when creating user with invalid status 'banned', got nil")
		}
	})

	t.Run("InvalidRoleRejected", func(t *testing.T) {
		userID := uuid.New()
		var pgUserID pgtype.UUID
		if err := pgUserID.Scan(userID.String()); err != nil {
			t.Fatalf("failed to scan user ID: %v", err)
		}

		hash, err := password.Hash("any-password")
		if err != nil {
			t.Fatalf("password.Hash failed: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Microsecond)
		var pgNow pgtype.Timestamptz
		if err := pgNow.Scan(now); err != nil {
			t.Fatalf("failed to scan timestamp: %v", err)
		}

		_, err = queries.CreateUser(ctx, query.CreateUserParams{
			ID:           pgUserID,
			Email:        "invalid-role@example.com",
			DisplayName:  "Invalid Role User",
			PasswordHash: hash,
			Status:       "active",
			Role:         "owner",
			CreatedAt:    pgNow,
			UpdatedAt:    pgNow,
		})
		if err == nil {
			t.Fatal("expected error when creating user with invalid role 'owner', got nil")
		}
	})
}
