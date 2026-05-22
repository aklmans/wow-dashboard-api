//go:build integration

package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
	"github.com/google/uuid"
)

func TestServiceIntegration(t *testing.T) {
	ctx := context.Background()

	pool := storetest.NewPostgresPool(t, ctx, "test_auth_db", "../../../migrations")

	// Setup Token Manager
	const testSecret = "super-secret-key-that-is-at-least-32-bytes-long"
	tokenManager, err := token.NewManager(testSecret, "test-issuer", "test-audience", 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}

	// Setup Auth Service
	authService := service.NewService(authrepo.NewUserStoreFromDB(pool), tokenManager,
		service.WithRefreshTokenStore(authrepo.NewRefreshTokenStoreFromDB(pool), 14*24*time.Hour))

	// 1. SignUp Success Scenario
	t.Run("SignUpSuccess", func(t *testing.T) {
		input := service.SignUpInput{
			Email:     "John.Doe@Example.Com", // Mixed case to test lowercase normalization
			Password:  "secure-password-123",
			FirstName: "John",
			LastName:  "Doe",
		}

		session, err := authService.SignUp(ctx, input)
		if err != nil {
			t.Fatalf("expected successful SignUp, got error: %v", err)
		}

		if session.AccessToken == "" {
			t.Error("expected valid AccessToken in session, got empty")
		}
		if session.RefreshToken == "" {
			t.Error("expected valid RefreshToken in session, got empty")
		}

		if session.User.Email != "john.doe@example.com" {
			t.Errorf("expected email 'john.doe@example.com', got %q", session.User.Email)
		}

		if session.User.DisplayName != "John Doe" {
			t.Errorf("expected display name 'John Doe', got %q", session.User.DisplayName)
		}

		if session.User.Status != "active" {
			t.Errorf("expected status 'active', got %q", session.User.Status)
		}

		if _, err := uuid.Parse(session.User.ID); err != nil {
			t.Errorf("expected user ID to be a valid UUID, got %q (error: %v)", session.User.ID, err)
		}
	})

	// 2. SignUp Duplicate Email rejection
	t.Run("SignUpDuplicateEmailRejected", func(t *testing.T) {
		input := service.SignUpInput{
			Email:     "JOHN.DOE@example.com", // Duplicate of previous John.Doe@Example.Com
			Password:  "another-pass-456",
			FirstName: "Johnny",
			LastName:  "Doe",
		}

		_, err := authService.SignUp(ctx, input)
		if err == nil {
			t.Fatal("expected error on duplicate email SignUp, got nil")
		}

		if !errors.Is(err, service.ErrEmailAlreadyExists) {
			t.Errorf("expected error %v, got %v", service.ErrEmailAlreadyExists, err)
		}

		// Ensure no sensitive database or email details are leaked in error string
		errStr := err.Error()
		if strings.Contains(strings.ToLower(errStr), "john.doe") {
			t.Errorf("error message leaks email: %s", errStr)
		}
		for _, keyword := range []string{"duplicate key", "users_email_unique", "key (email)", "sqlstate"} {
			if strings.Contains(strings.ToLower(errStr), keyword) {
				t.Errorf("error message leaks database details %q: %s", keyword, errStr)
			}
		}
	})

	// 3. SignUp password limit check
	t.Run("SignUpPasswordTooShort", func(t *testing.T) {
		input := service.SignUpInput{
			Email:     "short-pass@example.com",
			Password:  "12345", // Less than 6 chars
			FirstName: "Short",
			LastName:  "Pass",
		}

		_, err := authService.SignUp(ctx, input)
		if err == nil {
			t.Fatal("expected error on short password SignUp, got nil")
		}

		if !errors.Is(err, service.ErrInvalidInput) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidInput, err)
		}
	})

	// 4. SignUp blank names check
	t.Run("SignUpBlankNamesRejected", func(t *testing.T) {
		inputs := []service.SignUpInput{
			{
				Email:     "blank-first@example.com",
				Password:  "secure-pass-123",
				FirstName: "  ",
				LastName:  "Valid",
			},
			{
				Email:     "blank-last@example.com",
				Password:  "secure-pass-123",
				FirstName: "Valid",
				LastName:  "  ",
			},
		}

		for _, input := range inputs {
			_, err := authService.SignUp(ctx, input)
			if err == nil {
				t.Errorf("expected error on blank name SignUp for email %q, got nil", input.Email)
			}
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Errorf("expected error %v, got %v", service.ErrInvalidInput, err)
			}
		}
	})

	// 5. SignIn Success Scenario
	t.Run("SignInSuccess", func(t *testing.T) {
		input := service.SignInInput{
			Email:    "JOHN.DOE@EXAMPLE.COM", // Mixed case lookup
			Password: "secure-password-123",
		}

		session, err := authService.SignIn(ctx, input)
		if err != nil {
			t.Fatalf("expected successful SignIn, got error: %v", err)
		}

		if session.AccessToken == "" {
			t.Error("expected valid AccessToken in session, got empty")
		}
		if session.RefreshToken == "" {
			t.Error("expected valid RefreshToken in session, got empty")
		}

		if session.User.Email != "john.doe@example.com" {
			t.Errorf("expected email 'john.doe@example.com', got %q", session.User.Email)
		}
		// Verify the issued token can be parsed and verified
		claims, err := tokenManager.VerifyAccessToken(session.AccessToken)
		if err != nil {
			t.Fatalf("failed to verify issued session token: %v", err)
		}

		if claims.Subject != session.User.ID {
			t.Errorf("expected subject claim %q, got %q", session.User.ID, claims.Subject)
		}
	})

	// 6. SignIn Wrong Email
	t.Run("SignInWrongEmail", func(t *testing.T) {
		input := service.SignInInput{
			Email:    "nonexistent@example.com",
			Password: "secure-password-123",
		}

		_, err := authService.SignIn(ctx, input)
		if err == nil {
			t.Fatal("expected error on wrong email SignIn, got nil")
		}

		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidCredentials, err)
		}
	})

	// 7. SignIn Wrong Password
	t.Run("SignInWrongPassword", func(t *testing.T) {
		input := service.SignInInput{
			Email:    "john.doe@example.com",
			Password: "wrong-password",
		}

		_, err := authService.SignIn(ctx, input)
		if err == nil {
			t.Fatal("expected error on wrong password SignIn, got nil")
		}

		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidCredentials, err)
		}
	})

	// 8. Disabled User Cannot SignIn
	t.Run("SignInDisabledUserRejected", func(t *testing.T) {
		// First register a new user
		input := service.SignUpInput{
			Email:     "disabled-user@example.com",
			Password:  "secure-pass-123",
			FirstName: "Disabled",
			LastName:  "User",
		}

		session, err := authService.SignUp(ctx, input)
		if err != nil {
			t.Fatalf("failed to register disabled-user: %v", err)
		}

		if err := disableIntegrationUser(ctx, pool, session.User.ID); err != nil {
			t.Fatalf("failed to manually disable user: %v", err)
		}

		// Attempt sign-in
		_, err = authService.SignIn(ctx, service.SignInInput{
			Email:    "disabled-user@example.com",
			Password: "secure-pass-123",
		})
		if err == nil {
			t.Fatal("expected error on disabled user SignIn, got nil")
		}

		// Sign-in returns the generic invalid-credentials error for a disabled
		// account so it cannot be used to enumerate which accounts exist; the
		// disabled state is only surfaced post-authentication (CurrentUser,
		// Refresh).
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidCredentials, err)
		}
	})

	// 9. CurrentUser Success Scenario
	t.Run("CurrentUserSuccess", func(t *testing.T) {
		// Login john.doe to get token
		session, err := authService.SignIn(ctx, service.SignInInput{
			Email:    "john.doe@example.com",
			Password: "secure-password-123",
		})
		if err != nil {
			t.Fatalf("failed to sign in for CurrentUser test: %v", err)
		}

		profile, err := authService.CurrentUser(ctx, session.AccessToken)
		if err != nil {
			t.Fatalf("expected successful CurrentUser retrieval, got error: %v", err)
		}

		if profile.Email != "john.doe@example.com" {
			t.Errorf("expected email 'john.doe@example.com', got %q", profile.Email)
		}

		if profile.ID != session.User.ID {
			t.Errorf("expected ID %q, got %q", session.User.ID, profile.ID)
		}

		if profile.DisplayName != "John Doe" {
			t.Errorf("expected display name 'John Doe', got %q", profile.DisplayName)
		}
		if len(profile.Roles) != 1 || profile.Roles[0] != "user" {
			t.Errorf("expected roles [user], got %v", profile.Roles)
		}
		// The built-in user role carries projects:create and nothing else.
		if len(profile.Permissions) != 1 || profile.Permissions[0] != "projects:create" {
			t.Errorf("expected permissions [projects:create] for a plain user, got %v", profile.Permissions)
		}
	})

	// 10. CurrentUser Invalid Token
	t.Run("CurrentUserInvalidToken", func(t *testing.T) {
		const rawToken = "invalid-token-string"
		_, err := authService.CurrentUser(ctx, rawToken)
		if err == nil {
			t.Fatal("expected error on invalid token CurrentUser check, got nil")
		}

		if !errors.Is(err, service.ErrInvalidToken) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidToken, err)
		}

		// Ensure raw token is not leaked in error string
		if strings.Contains(err.Error(), rawToken) {
			t.Errorf("error message leaks raw token: %s", err.Error())
		}
	})

	// 11. CurrentUser Token Subject Not UUID
	t.Run("CurrentUserSubjectNotUUID", func(t *testing.T) {
		const invalidSubject = "not-a-uuid"
		// Generate token using invalid non-UUID subject
		invalidToken, err := tokenManager.IssueAccessToken(invalidSubject)
		if err != nil {
			t.Fatalf("failed to issue token with non-UUID subject: %v", err)
		}

		_, err = authService.CurrentUser(ctx, invalidToken)
		if err == nil {
			t.Fatal("expected error on non-UUID subject token CurrentUser check, got nil")
		}

		if !errors.Is(err, service.ErrInvalidToken) {
			t.Errorf("expected error %v, got %v", service.ErrInvalidToken, err)
		}

		// Ensure raw invalid subject is not leaked in error string
		if strings.Contains(err.Error(), invalidSubject) {
			t.Errorf("error message leaks subject: %s", err.Error())
		}
	})

	// 12. CurrentUser User Disabled
	t.Run("CurrentUserDisabledUserRejected", func(t *testing.T) {
		// Register a user that will be disabled
		input := service.SignUpInput{
			Email:     "current-disabled@example.com",
			Password:  "secure-pass-123",
			FirstName: "Current",
			LastName:  "Disabled",
		}

		session, err := authService.SignUp(ctx, input)
		if err != nil {
			t.Fatalf("failed to register current-disabled user: %v", err)
		}

		if err := disableIntegrationUser(ctx, pool, session.User.ID); err != nil {
			t.Fatalf("failed to manually disable user: %v", err)
		}

		// Attempt CurrentUser fetch with valid token
		_, err = authService.CurrentUser(ctx, session.AccessToken)
		if err == nil {
			t.Fatal("expected error on disabled user CurrentUser check, got nil")
		}

		if !errors.Is(err, service.ErrUserDisabled) {
			t.Errorf("expected error %v, got %v", service.ErrUserDisabled, err)
		}
	})
}

func disableIntegrationUser(ctx context.Context, db authrepo.DBTX, userID string) error {
	_, err := db.Exec(ctx,
		"UPDATE users SET status = $1, updated_at = $2 WHERE id = $3::uuid",
		"disabled",
		time.Now().UTC().Truncate(time.Microsecond),
		userID,
	)
	return err
}
