//go:build integration

package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/aklmans/wow-dashboard-api/internal/app"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

func TestAuthHandlersIntegration(t *testing.T) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("test_auth_handlers_db"),
		postgres.WithUsername("test_auth_handlers_user"),
		postgres.WithPassword("test_auth_handlers_password"),
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
		t.Fatalf("failed to get container connection string: %v", err)
	}

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("failed to open database for migrations: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}
	if err := goose.Up(db, "../../../migrations"); err != nil {
		t.Fatalf("goose up failed: %v", err)
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
		t.Fatalf("failed to initialize pool: %v", err)
	}
	defer pool.Close()

	tokenManager, err := token.NewManager(
		"integration-secret-key-at-least-32-bytes",
		"test-issuer",
		"test-audience",
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf("failed to initialize token manager: %v", err)
	}

	queries := query.New(pool)
	authSvc := authservice.NewService(authrepo.NewUserStore(queries), tokenManager,
		authservice.WithRefreshTokenStore(authrepo.NewRefreshTokenStore(queries), 14*24*time.Hour))
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	api := app.NewAPI(router)
	app.RegisterRoutes(api, app.Dependencies{AuthService: authSvc})

	signUpRec := postJSON(router, "/api/auth/sign-up", map[string]string{
		"email":     "Hello@Gmail.com",
		"password":  "@Password",
		"firstName": "Hello",
		"lastName":  "Friend",
	})
	if signUpRec.Code != http.StatusCreated {
		t.Fatalf("sign-up status = %d, want %d; body=%s", signUpRec.Code, http.StatusCreated, signUpRec.Body.String())
	}

	var signUpBody authSessionResponse
	decodeJSON(t, signUpRec, &signUpBody)
	assertIntegrationUser(t, signUpBody.User, "hello@gmail.com", "Hello Friend")
	signUpAccessCookie := cookieByName(t, signUpRec, "wow_dashboard_access_token")
	if signUpAccessCookie.Value == "" || !signUpAccessCookie.HttpOnly || signUpAccessCookie.Path != "/" {
		t.Fatalf("sign-up access cookie = %#v, want HttpOnly / cookie", signUpAccessCookie)
	}
	signUpRefreshCookie := cookieByName(t, signUpRec, "wow_dashboard_refresh_token")
	if signUpRefreshCookie.Value == "" || !signUpRefreshCookie.HttpOnly || signUpRefreshCookie.Path != "/api/auth" {
		t.Fatalf("sign-up refresh cookie = %#v, want HttpOnly /api/auth cookie", signUpRefreshCookie)
	}

	signInRec := postJSON(router, "/api/auth/sign-in", map[string]string{
		"email":    "hello@gmail.com",
		"password": "@Password",
	})
	if signInRec.Code != http.StatusOK {
		t.Fatalf("sign-in status = %d, want %d; body=%s", signInRec.Code, http.StatusOK, signInRec.Body.String())
	}

	var signInBody authSessionResponse
	decodeJSON(t, signInRec, &signInBody)
	assertIntegrationUser(t, signInBody.User, "hello@gmail.com", "Hello Friend")
	signInAccessCookie := cookieByName(t, signInRec, "wow_dashboard_access_token")
	if signInAccessCookie.Value == "" || !signInAccessCookie.HttpOnly || signInAccessCookie.Path != "/" {
		t.Fatalf("sign-in access cookie = %#v, want HttpOnly / cookie", signInAccessCookie)
	}
	signInRefreshCookie := cookieByName(t, signInRec, "wow_dashboard_refresh_token")
	if signInRefreshCookie.Value == "" || !signInRefreshCookie.HttpOnly || signInRefreshCookie.Path != "/api/auth" {
		t.Fatalf("sign-in refresh cookie = %#v, want HttpOnly /api/auth cookie", signInRefreshCookie)
	}

	refreshRec := postNoBodyWithCookie(router, "/api/auth/refresh", "wow_dashboard_refresh_token", signInRefreshCookie.Value)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want %d; body=%s", refreshRec.Code, http.StatusOK, refreshRec.Body.String())
	}
	var refreshBody authSessionResponse
	decodeJSON(t, refreshRec, &refreshBody)
	assertIntegrationUser(t, refreshBody.User, "hello@gmail.com", "Hello Friend")
	refreshAccessCookie := cookieByName(t, refreshRec, "wow_dashboard_access_token")
	if refreshAccessCookie.Value == "" {
		t.Fatal("refresh access cookie is empty")
	}
	rotatedRefreshCookie := cookieByName(t, refreshRec, "wow_dashboard_refresh_token")
	if rotatedRefreshCookie.Value == "" || rotatedRefreshCookie.Value == signInRefreshCookie.Value {
		t.Fatal("refresh cookie was not rotated")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+refreshAccessCookie.Value)
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body=%s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	var meBody authMeResponse
	if err := json.NewDecoder(meRec.Body).Decode(&meBody); err != nil {
		t.Fatalf("failed to decode me response: %v", err)
	}
	assertIntegrationUser(t, meBody.User, "hello@gmail.com", "Hello Friend")
	if meBody.User.ID != signInBody.User.ID {
		t.Errorf("me user id = %q, want sign-in user id %q", meBody.User.ID, signInBody.User.ID)
	}

	signOutRec := postNoBodyWithCookie(router, "/api/auth/sign-out", "wow_dashboard_refresh_token", rotatedRefreshCookie.Value)
	if signOutRec.Code != http.StatusOK {
		t.Fatalf("sign-out status = %d, want %d; body=%s", signOutRec.Code, http.StatusOK, signOutRec.Body.String())
	}
	assertClearedRefreshCookie(t, signOutRec)
}

func assertIntegrationUser(t *testing.T, user starterUser, wantEmail string, wantDisplayName string) {
	t.Helper()
	if user.ID == "" {
		t.Error("user.id is empty")
	}
	if user.Email != wantEmail {
		t.Errorf("user.email = %q, want %q", user.Email, wantEmail)
	}
	if user.DisplayName != wantDisplayName {
		t.Errorf("user.displayName = %q, want %q", user.DisplayName, wantDisplayName)
	}
}
