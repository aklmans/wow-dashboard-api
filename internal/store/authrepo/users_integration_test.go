//go:build integration

package authrepo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestUserStoreIntegration(t *testing.T) {
	ctx := context.Background()
	queries := newAuthRepoTestQueries(t, ctx, "test_authrepo_users_db")
	repo := authrepo.NewUserStore(queries)

	hash, err := password.Hash("correct-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}

	userID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)

	created, err := repo.CreateUser(ctx, domain.CreateUserInput{
		ID:           userID,
		Email:        "Alice@Example.com",
		DisplayName:  "Alice Example",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		Role:         domain.UserRoleAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if created.ID != userID {
		t.Fatalf("created ID = %s, want %s", created.ID, userID)
	}
	if created.Email != "alice@example.com" {
		t.Fatalf("created email = %q, want lowercase email", created.Email)
	}
	if created.Role != domain.UserRoleAdmin || created.Status != domain.UserStatusActive {
		t.Fatalf("created role/status = %q/%q, want admin/active", created.Role, created.Status)
	}

	_, err = repo.CreateUser(ctx, domain.CreateUserInput{
		ID:           uuid.New(),
		Email:        "ALICE@example.com",
		DisplayName:  "Alice Duplicate",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		Role:         domain.UserRoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if !errors.Is(err, domain.ErrEmailAlreadyExists) {
		t.Fatalf("duplicate CreateUser error = %v, want ErrEmailAlreadyExists", err)
	}

	_, err = repo.GetUserByID(ctx, uuid.New())
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("GetUserByID missing error = %v, want ErrUserNotFound", err)
	}

	_, err = repo.GetUserByEmailForAuth(ctx, "missing@example.com")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("GetUserByEmailForAuth missing error = %v, want ErrUserNotFound", err)
	}

	authUser, err := repo.GetUserByEmailForAuth(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmailForAuth failed: %v", err)
	}
	if authUser.ID != userID || authUser.PasswordHash == "" {
		t.Fatalf("auth user = %#v, want matching user with password hash", authUser)
	}

	_, err = repo.CreateUser(ctx, domain.CreateUserInput{
		ID:           uuid.New(),
		Email:        "invalid-status@example.com",
		DisplayName:  "Invalid Status",
		PasswordHash: hash,
		Status:       domain.UserStatus("banned"),
		Role:         domain.UserRoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if !errors.Is(err, domain.ErrInvalidUserStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidUserStatus", err)
	}

	_, err = repo.CreateUser(ctx, domain.CreateUserInput{
		ID:           uuid.New(),
		Email:        "invalid-role@example.com",
		DisplayName:  "Invalid Role",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		Role:         domain.UserRole("owner"),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if !errors.Is(err, domain.ErrInvalidUserRole) {
		t.Fatalf("invalid role error = %v, want ErrInvalidUserRole", err)
	}
}

func newAuthRepoTestQueries(t *testing.T, ctx context.Context, database string) *query.Queries {
	t.Helper()

	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase(database),
		postgres.WithUsername("test_authrepo_user"),
		postgres.WithPassword("test_authrepo_password"),
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
		t.Fatalf("failed to open db connection for goose: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("failed to close goose db: %v", err)
		}
	})

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	if err := goose.Up(db, "../../../migrations"); err != nil {
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

	return query.New(pool)
}
