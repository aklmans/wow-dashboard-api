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
	if created.Status != domain.UserStatusActive {
		t.Fatalf("created status = %q, want active", created.Status)
	}

	_, err = repo.CreateUser(ctx, domain.CreateUserInput{
		ID:           uuid.New(),
		Email:        "ALICE@example.com",
		DisplayName:  "Alice Duplicate",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
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
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if !errors.Is(err, domain.ErrInvalidUserStatus) {
		t.Fatalf("invalid status error = %v, want ErrInvalidUserStatus", err)
	}

	// Role resolution: a freshly created user has no roles until one is
	// assigned. After granting the seeded admin role, the RBAC lookups should
	// return that role and its wildcard permission.
	if roles, err := repo.GetUserRoles(ctx, userID); err != nil || len(roles) != 0 {
		t.Fatalf("GetUserRoles before assignment = %v, %v; want empty", roles, err)
	}

	adminRole, err := repo.GetRoleByName(ctx, "admin")
	if err != nil {
		t.Fatalf("GetRoleByName(admin) failed: %v", err)
	}
	if _, err := repo.GetRoleByName(ctx, "no-such-role"); !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("GetRoleByName(missing) error = %v, want ErrRoleNotFound", err)
	}
	if err := repo.AddUserRole(ctx, userID, adminRole.ID); err != nil {
		t.Fatalf("AddUserRole failed: %v", err)
	}

	roles, err := repo.GetUserRoles(ctx, userID)
	if err != nil || len(roles) != 1 || roles[0] != "admin" {
		t.Fatalf("GetUserRoles after assignment = %v, %v; want [admin]", roles, err)
	}
	perms, err := repo.GetUserPermissions(ctx, userID)
	if err != nil || len(perms) != 1 || perms[0] != "*" {
		t.Fatalf("GetUserPermissions = %v, %v; want [*]", perms, err)
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
