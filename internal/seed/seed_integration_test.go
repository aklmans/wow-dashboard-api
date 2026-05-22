//go:build integration

package seed

import (
	"context"
	"database/sql"
	"testing"
	"time"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSeedDemoUserIntegration(t *testing.T) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("test_seed_db"),
		postgres.WithUsername("test_seed_user"),
		postgres.WithPassword("test_seed_password"),
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
			t.Errorf("failed to terminate postgres container: %v", err)
		}
	}()

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to open db connection for goose: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
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
		t.Fatalf("failed to initialize store pool: %v", err)
	}
	defer pool.Close()

	queries := query.New(pool)

	first, err := SeedDemoUser(ctx, queries)
	if err != nil {
		t.Fatalf("SeedDemoUser first run failed: %v", err)
	}
	assertDemoUser(t, first)
	assertUserCount(t, ctx, queries, 1)

	var pgID pgtype.UUID
	if err := pgID.Scan(first.ID); err != nil {
		t.Fatalf("failed to scan seeded user ID: %v", err)
	}
	if _, err := queries.UpdateUserStatus(ctx, query.UpdateUserStatusParams{
		ID:     pgID,
		Status: "disabled",
	}); err != nil {
		t.Fatalf("failed to disable seeded user before second seed: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, pgID); err != nil {
		t.Fatalf("failed to downgrade seeded user role before second seed: %v", err)
	}

	second, err := SeedDemoUser(ctx, queries)
	if err != nil {
		t.Fatalf("SeedDemoUser second run failed: %v", err)
	}
	assertDemoUser(t, second)
	if second.ID != first.ID {
		t.Errorf("second seed ID = %q, want same ID %q", second.ID, first.ID)
	}
	assertUserCount(t, ctx, queries, 1)

	tokenManager, err := token.NewManager(
		"integration-secret-key-at-least-32-bytes",
		"test-issuer",
		"test-audience",
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf("failed to initialize token manager: %v", err)
	}
	authSvc := authservice.NewService(authrepo.NewUserStore(queries), tokenManager)

	session, err := authSvc.SignIn(ctx, authservice.SignInInput{
		Email:    DemoEmail,
		Password: DemoPassword,
	})
	if err != nil {
		t.Fatalf("demo user SignIn failed after seed: %v", err)
	}
	if session.AccessToken == "" {
		t.Fatal("demo user access token is empty")
	}
	if session.User.ID != first.ID {
		t.Errorf("session user ID = %q, want seeded user ID %q", session.User.ID, first.ID)
	}
	if session.User.Email != DemoEmail {
		t.Errorf("session email = %q, want %q", session.User.Email, DemoEmail)
	}
	if session.User.DisplayName != DemoDisplayName {
		t.Errorf("session displayName = %q, want %q", session.User.DisplayName, DemoDisplayName)
	}
	if session.User.Role != "admin" {
		t.Errorf("session role = %q, want admin", session.User.Role)
	}
}

func assertDemoUser(t *testing.T, user DemoUser) {
	t.Helper()
	if user.ID == "" {
		t.Fatal("seeded user ID is empty")
	}
	if user.Email != DemoEmail {
		t.Errorf("seeded email = %q, want %q", user.Email, DemoEmail)
	}
	if user.DisplayName != DemoDisplayName {
		t.Errorf("seeded display name = %q, want %q", user.DisplayName, DemoDisplayName)
	}
	if user.Status != "active" {
		t.Errorf("seeded status = %q, want active", user.Status)
	}
	if user.Role != "admin" {
		t.Errorf("seeded role = %q, want admin", user.Role)
	}
}

func assertUserCount(t *testing.T, ctx context.Context, queries *query.Queries, want int) {
	t.Helper()
	users, err := queries.ListUsers(ctx, query.ListUsersParams{
		OffsetVal: 0,
		LimitVal:  10,
	})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != want {
		t.Fatalf("user count = %d, want %d", len(users), want)
	}
}
