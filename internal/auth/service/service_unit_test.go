package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/email"
	"github.com/google/uuid"
)

func TestServiceSignUpWithDomainStore(t *testing.T) {
	t.Run("success with unit of work", func(t *testing.T) {
		directStore := &unitUserStore{}
		directRefreshStore := &unitRefreshTokenStore{}
		uow := &unitOfWork{
			users:         &unitUserStore{},
			refreshTokens: &unitRefreshTokenStore{},
		}
		authSvc := service.NewService(directStore, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(directRefreshStore, 14*24*time.Hour),
			service.WithUnitOfWork(uow))

		session, err := authSvc.SignUp(context.Background(), service.SignUpInput{
			Email:     "Tx.User@Example.com",
			Password:  "secure-password",
			FirstName: "Tx",
			LastName:  "User",
		})
		if err != nil {
			t.Fatalf("SignUp returned error: %v", err)
		}

		if uow.calls != 1 {
			t.Fatalf("UnitOfWork calls = %d, want 1", uow.calls)
		}
		if !uow.committed || uow.rolledBack {
			t.Fatalf("UnitOfWork committed/rolledBack = %v/%v, want committed only", uow.committed, uow.rolledBack)
		}
		if directStore.createCalls != 0 {
			t.Fatalf("direct CreateUser calls = %d, want 0", directStore.createCalls)
		}
		if uow.users.created.Email != "tx.user@example.com" {
			t.Fatalf("uow created email = %q, want normalized email", uow.users.created.Email)
		}
		if session.RefreshToken == "" {
			t.Fatal("refresh token is empty")
		}
		if uow.refreshTokens.created.UserID != uow.users.created.ID {
			t.Fatalf("refresh token user ID = %s, want created user ID %s", uow.refreshTokens.created.UserID, uow.users.created.ID)
		}
	})

	t.Run("refresh token failure returns transaction error", func(t *testing.T) {
		refreshErr := errors.New("refresh token insert failed")
		uow := &unitOfWork{
			users:         &unitUserStore{},
			refreshTokens: &unitRefreshTokenStore{createErr: refreshErr},
		}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(&unitRefreshTokenStore{}, 14*24*time.Hour),
			service.WithUnitOfWork(uow))

		_, err := authSvc.SignUp(context.Background(), service.SignUpInput{
			Email:     "rollback@example.com",
			Password:  "secure-password",
			FirstName: "Rollback",
			LastName:  "User",
		})
		if err == nil {
			t.Fatal("SignUp returned nil error, want refresh token failure")
		}
		if !errors.Is(err, refreshErr) {
			t.Fatalf("SignUp error = %v, want refresh token failure", err)
		}
		if uow.fnErr == nil {
			t.Fatal("UnitOfWork fnErr is nil, want transaction callback error")
		}
		if !uow.rolledBack || uow.committed {
			t.Fatalf("UnitOfWork committed/rolledBack = %v/%v, want rollback only", uow.committed, uow.rolledBack)
		}
		if uow.users.createCalls != 1 {
			t.Fatalf("uow CreateUser calls = %d, want 1", uow.users.createCalls)
		}
	})

	t.Run("success", func(t *testing.T) {
		store := &unitUserStore{}
		refreshStore := &unitRefreshTokenStore{}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		session, err := authSvc.SignUp(context.Background(), service.SignUpInput{
			Email:     "New.User@Example.com",
			Password:  "secure-password",
			FirstName: "New",
			LastName:  "User",
		})
		if err != nil {
			t.Fatalf("SignUp returned error: %v", err)
		}

		if store.created.Email != "new.user@example.com" {
			t.Fatalf("created email = %q, want normalized email", store.created.Email)
		}
		if store.created.DisplayName != "New User" {
			t.Fatalf("created display name = %q, want New User", store.created.DisplayName)
		}
		if store.created.Status != domain.UserStatusActive {
			t.Fatalf("created status = %q, want active", store.created.Status)
		}
		if len(store.addedRoles) != 1 {
			t.Fatalf("default role assignments = %d, want 1", len(store.addedRoles))
		}
		if store.created.ID == uuid.Nil {
			t.Fatal("created ID is nil")
		}
		if store.created.PasswordHash == "" || strings.Contains(store.created.PasswordHash, "secure-password") {
			t.Fatalf("password hash was not stored safely: %q", store.created.PasswordHash)
		}
		if session.User.Email != "new.user@example.com" {
			t.Fatalf("session email = %q, want normalized email", session.User.Email)
		}
		if session.User.Status != "active" {
			t.Fatalf("session user status = %q, want active", session.User.Status)
		}
		if session.AccessToken != "access-token" {
			t.Fatalf("access token = %q, want access-token", session.AccessToken)
		}
		if session.RefreshToken == "" {
			t.Fatal("refresh token is empty")
		}
		if refreshStore.created.UserID != store.created.ID {
			t.Fatalf("refresh token user ID = %s, want %s", refreshStore.created.UserID, store.created.ID)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		tests := []service.SignUpInput{
			{Email: "bad-email", Password: "secure-password", FirstName: "Bad", LastName: "Email"},
			{Email: "short@example.com", Password: "12345", FirstName: "Short", LastName: "Password"},
			{Email: "blank-first@example.com", Password: "secure-password", FirstName: " ", LastName: "User"},
			{Email: "blank-last@example.com", Password: "secure-password", FirstName: "Blank", LastName: " "},
		}

		for _, input := range tests {
			store := &unitUserStore{}
			authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})
			_, err := authSvc.SignUp(context.Background(), input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("SignUp(%q) error = %v, want ErrInvalidInput", input.Email, err)
			}
			if store.createCalls != 0 {
				t.Fatalf("CreateUser calls = %d, want 0 for invalid input", store.createCalls)
			}
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		store := &unitUserStore{createErr: domain.ErrEmailAlreadyExists}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		_, err := authSvc.SignUp(context.Background(), service.SignUpInput{
			Email:     "existing@example.com",
			Password:  "secure-password",
			FirstName: "Existing",
			LastName:  "User",
		})
		if !errors.Is(err, service.ErrEmailAlreadyExists) {
			t.Fatalf("SignUp error = %v, want ErrEmailAlreadyExists", err)
		}
	})
}

func TestServiceSignInWithDomainStore(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("success", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password"),
		}
		refreshStore := &unitRefreshTokenStore{}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		session, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "DEMO@example.com",
			Password: "correct-password",
		})
		if err != nil {
			t.Fatalf("SignIn returned error: %v", err)
		}
		if store.authLookupEmail != "demo@example.com" {
			t.Fatalf("lookup email = %q, want normalized email", store.authLookupEmail)
		}
		if session.User.ID != userID.String() {
			t.Fatalf("session user = %#v, want user %s", session.User, userID)
		}
		if session.RefreshToken == "" {
			t.Fatal("refresh token is empty")
		}
		if refreshStore.created.UserID != userID {
			t.Fatalf("refresh token user ID = %s, want %s", refreshStore.created.UserID, userID)
		}
		if refreshStore.created.TokenHash == "" || strings.Contains(refreshStore.created.TokenHash, session.RefreshToken) {
			t.Fatalf("refresh token hash was not stored safely: hash=%q raw=%q", refreshStore.created.TokenHash, session.RefreshToken)
		}
	})

	t.Run("not found maps to invalid credentials", func(t *testing.T) {
		store := &unitUserStore{authUserErr: domain.ErrUserNotFound}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		_, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "missing@example.com",
			Password: "any-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password"),
		}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		_, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "demo@example.com",
			Password: "wrong-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
		if store.registerFailureCalls != 1 {
			t.Fatalf("registerFailureCalls = %d, want 1 after a wrong password", store.registerFailureCalls)
		}
	})

	t.Run("configured lockout policy is applied to the failure counter", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password"),
		}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"},
			service.WithLockoutPolicy(3, 5*time.Minute))

		_, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "demo@example.com",
			Password: "wrong-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
		// The configured threshold (not the default 10) is passed to the store.
		if store.registeredMaxAttempts != 3 {
			t.Fatalf("registeredMaxAttempts = %d, want the configured 3", store.registeredMaxAttempts)
		}
	})

	t.Run("disabled user returns generic invalid credentials", func(t *testing.T) {
		// A disabled account must return the same error as a wrong password so
		// sign-in cannot be used to enumerate which accounts exist.
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "disabled@example.com", "Disabled User", domain.UserStatusDisabled, "correct-password"),
		}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		_, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "disabled@example.com",
			Password: "correct-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("locked account is rejected without registering a new failure", func(t *testing.T) {
		authUser := testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password")
		lockedUntil := time.Now().Add(10 * time.Minute)
		authUser.LockedUntil = &lockedUntil
		store := &unitUserStore{authUser: authUser}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		// Even the correct password is rejected while the account is locked.
		_, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "demo@example.com",
			Password: "correct-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
		if store.registerFailureCalls != 0 {
			t.Fatalf("registerFailureCalls = %d, want 0 for an already-locked account", store.registerFailureCalls)
		}
	})

	t.Run("successful sign-in clears accumulated failures", func(t *testing.T) {
		authUser := testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password")
		authUser.FailedLoginCount = 3
		store := &unitUserStore{authUser: authUser}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "access-token"})

		if _, err := authSvc.SignIn(context.Background(), service.SignInInput{
			Email:    "demo@example.com",
			Password: "correct-password",
		}); err != nil {
			t.Fatalf("SignIn returned error: %v", err)
		}
		if store.clearFailuresCalls != 1 {
			t.Fatalf("clearFailuresCalls = %d, want 1", store.clearFailuresCalls)
		}
	})
}

func TestServiceChangePassword(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("success updates the hash and revokes every session", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		refreshStore := &unitRefreshTokenStore{}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		if err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "new-password-123"); err != nil {
			t.Fatalf("ChangePassword returned error: %v", err)
		}
		if store.updatedPasswordHash == "" || strings.Contains(store.updatedPasswordHash, "new-password-123") {
			t.Fatalf("password hash was not stored safely: %q", store.updatedPasswordHash)
		}
		if refreshStore.revokedAllForUser != userID {
			t.Fatalf("revokedAllForUser = %s, want %s", refreshStore.revokedAllForUser, userID)
		}
	})

	t.Run("wrong current password is rejected", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())})

		if err := authSvc.ChangePassword(context.Background(), "raw-token", "wrong-password", "new-password-123"); !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("ChangePassword error = %v, want ErrInvalidCredentials", err)
		}
		if store.updatedPasswordHash != "" {
			t.Fatal("password was updated despite a wrong current password")
		}
	})

	t.Run("invalid token is rejected", func(t *testing.T) {
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{verifyErr: errors.New("bad token")})
		if err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "new-password-123"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("ChangePassword error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("short new password is rejected", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())})
		if err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "short"); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("ChangePassword error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("session revocation failure is returned", func(t *testing.T) {
		revokeErr := errors.New("revoke failed")
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		refreshStore := &unitRefreshTokenStore{revokeAllErr: revokeErr}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "new-password-123")
		if !errors.Is(err, revokeErr) {
			t.Fatalf("ChangePassword error = %v, want revoke error", err)
		}
	})
}

func TestServiceChangePasswordTransactionalAudit(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("success records the audit inside the transaction", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		uow := &unitOfWork{users: &unitUserStore{}, refreshTokens: &unitRefreshTokenStore{}}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())},
			service.WithUnitOfWork(uow))

		if err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "new-password-123"); err != nil {
			t.Fatalf("ChangePassword returned error: %v", err)
		}
		if !uow.committed || uow.rolledBack {
			t.Fatalf("committed/rolledBack = %v/%v, want committed only", uow.committed, uow.rolledBack)
		}
		if uow.audit == nil || len(uow.audit.events) != 1 || uow.audit.events[0].EventType != service.EventAuthPasswordChanged {
			t.Fatalf("transaction audit events = %#v, want one %s", uow.audit, service.EventAuthPasswordChanged)
		}
	})

	t.Run("audit failure rolls back the password change", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "current-password"),
		}
		uow := &unitOfWork{
			users:         &unitUserStore{},
			refreshTokens: &unitRefreshTokenStore{},
			audit:         &fakeAuditRecorder{err: errors.New("audit insert failed")},
		}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())},
			service.WithUnitOfWork(uow))

		// With the audit write failing inside the transaction, the whole change
		// must roll back rather than leave an un-audited password change.
		if err := authSvc.ChangePassword(context.Background(), "raw-token", "current-password", "new-password-123"); err == nil {
			t.Fatal("ChangePassword returned nil, want the audit failure to roll back the change")
		}
		if !uow.rolledBack || uow.committed {
			t.Fatalf("committed/rolledBack = %v/%v, want rollback only", uow.committed, uow.rolledBack)
		}
	})
}

func TestServiceCurrentUserWithDomainStore(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("success", func(t *testing.T) {
		store := &unitUserStore{
			user: domain.User{
				ID:          userID,
				Email:       "demo@example.com",
				DisplayName: "Demo User",
				Status:      domain.UserStatusActive,
			},
			roles:       []string{"admin"},
			permissions: []string{"*"},
		}
		authSvc := service.NewService(store, &fakeTokenManager{
			claims: testClaims(userID.String()),
		})

		user, err := authSvc.CurrentUser(context.Background(), "raw-token")
		if err != nil {
			t.Fatalf("CurrentUser returned error: %v", err)
		}
		if user.ID != userID.String() || user.Email != "demo@example.com" {
			t.Fatalf("current user = %#v, want demo user", user)
		}
		if len(user.Roles) != 1 || user.Roles[0] != "admin" {
			t.Fatalf("current user roles = %v, want [admin]", user.Roles)
		}
		if len(user.Permissions) != 1 || user.Permissions[0] != "*" {
			t.Fatalf("current user permissions = %v, want [*]", user.Permissions)
		}
		if store.userLookupID != userID {
			t.Fatalf("lookup ID = %s, want %s", store.userLookupID, userID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		authSvc := service.NewService(&unitUserStore{userErr: domain.ErrUserNotFound}, &fakeTokenManager{
			claims: testClaims(userID.String()),
		})

		_, err := authSvc.CurrentUser(context.Background(), "raw-token")
		if !errors.Is(err, service.ErrUserNotFound) {
			t.Fatalf("CurrentUser error = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{verifyErr: errors.New("bad token")})

		_, err := authSvc.CurrentUser(context.Background(), "raw-token")
		if !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("CurrentUser error = %v, want ErrInvalidToken", err)
		}
	})
}

func TestServiceRefreshWithDomainStores(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	oldTokenID := uuid.MustParse("00000000-0000-0000-0000-000000000456")
	familyID := uuid.MustParse("00000000-0000-0000-0000-000000000789")

	t.Run("success rotates refresh token", func(t *testing.T) {
		store := &unitUserStore{
			user: domain.User{
				ID:          userID,
				Email:       "demo@example.com",
				DisplayName: "Demo User",
				Status:      domain.UserStatusActive,
			},
		}
		refreshStore := &unitRefreshTokenStore{
			token: domain.RefreshToken{
				ID:        oldTokenID,
				UserID:    userID,
				FamilyID:  familyID,
				ExpiresAt: now.Add(time.Hour),
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now.Add(-time.Hour),
			},
		}
		authSvc := service.NewService(store, &fakeTokenManager{issuedToken: "new-access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour),
			service.WithClock(func() time.Time { return now }))

		session, err := authSvc.Refresh(context.Background(), "raw-refresh-token")
		if err != nil {
			t.Fatalf("Refresh returned error: %v", err)
		}
		if session.AccessToken != "new-access-token" {
			t.Fatalf("access token = %q, want new-access-token", session.AccessToken)
		}
		if session.RefreshToken == "" || session.RefreshToken == "raw-refresh-token" {
			t.Fatalf("refresh token was not rotated: %q", session.RefreshToken)
		}
		if refreshStore.lookupHash == "" || strings.Contains(refreshStore.lookupHash, "raw-refresh-token") {
			t.Fatalf("lookup hash = %q, want non-raw hash", refreshStore.lookupHash)
		}
		if refreshStore.rotatedOldID != oldTokenID {
			t.Fatalf("rotated old ID = %s, want %s", refreshStore.rotatedOldID, oldTokenID)
		}
		if refreshStore.rotatedInput.FamilyID != familyID {
			t.Fatalf("rotated family ID = %s, want %s", refreshStore.rotatedInput.FamilyID, familyID)
		}
		if refreshStore.rotatedInput.UserID != userID {
			t.Fatalf("rotated user ID = %s, want %s", refreshStore.rotatedInput.UserID, userID)
		}
	})

	t.Run("invalid token cases", func(t *testing.T) {
		revokedAt := now.Add(-time.Minute)
		tests := []struct {
			name         string
			rawToken     string
			refreshStore *unitRefreshTokenStore
		}{
			{
				name:     "missing",
				rawToken: "",
				refreshStore: &unitRefreshTokenStore{
					token: domain.RefreshToken{ID: oldTokenID, UserID: userID, ExpiresAt: now.Add(time.Hour)},
				},
			},
			{
				name:     "not found",
				rawToken: "missing-token",
				refreshStore: &unitRefreshTokenStore{
					getErr: domain.ErrRefreshTokenNotFound,
				},
			},
			{
				name:     "expired",
				rawToken: "expired-token",
				refreshStore: &unitRefreshTokenStore{
					token: domain.RefreshToken{ID: oldTokenID, UserID: userID, ExpiresAt: now.Add(-time.Second)},
				},
			},
			{
				name:     "revoked",
				rawToken: "revoked-token",
				refreshStore: &unitRefreshTokenStore{
					token: domain.RefreshToken{ID: oldTokenID, UserID: userID, ExpiresAt: now.Add(time.Hour), RevokedAt: &revokedAt},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
					service.WithRefreshTokenStore(tt.refreshStore, 14*24*time.Hour),
					service.WithClock(func() time.Time { return now }))

				_, err := authSvc.Refresh(context.Background(), tt.rawToken)
				if !errors.Is(err, service.ErrInvalidToken) {
					t.Fatalf("Refresh error = %v, want ErrInvalidToken", err)
				}
			})
		}
	})

	t.Run("disabled user", func(t *testing.T) {
		authSvc := service.NewService(&unitUserStore{
			user: domain.User{
				ID:          userID,
				Email:       "disabled@example.com",
				DisplayName: "Disabled User",
				Status:      domain.UserStatusDisabled,
			},
		}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(&unitRefreshTokenStore{
				token: domain.RefreshToken{ID: oldTokenID, UserID: userID, ExpiresAt: now.Add(time.Hour)},
			}, 14*24*time.Hour),
			service.WithClock(func() time.Time { return now }))

		_, err := authSvc.Refresh(context.Background(), "raw-refresh-token")
		if !errors.Is(err, service.ErrUserDisabled) {
			t.Fatalf("Refresh error = %v, want ErrUserDisabled", err)
		}
	})

	t.Run("reused revoked token revokes the whole family", func(t *testing.T) {
		familyID := uuid.MustParse("00000000-0000-0000-0000-0000000000fa")
		revokedAt := now.Add(-time.Minute)
		refreshStore := &unitRefreshTokenStore{
			token: domain.RefreshToken{
				ID:        oldTokenID,
				UserID:    userID,
				FamilyID:  familyID,
				ExpiresAt: now.Add(time.Hour),
				RevokedAt: &revokedAt,
			},
		}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour),
			service.WithClock(func() time.Time { return now }))

		_, err := authSvc.Refresh(context.Background(), "reused-revoked-token")
		if !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("Refresh error = %v, want ErrInvalidToken", err)
		}
		if refreshStore.revokedFamily != familyID {
			t.Fatalf("revoked family = %s, want %s", refreshStore.revokedFamily, familyID)
		}
	})

	t.Run("rotation conflict revokes the whole family", func(t *testing.T) {
		familyID := uuid.MustParse("00000000-0000-0000-0000-0000000000fb")
		refreshStore := &unitRefreshTokenStore{
			token: domain.RefreshToken{
				ID:        oldTokenID,
				UserID:    userID,
				FamilyID:  familyID,
				ExpiresAt: now.Add(time.Hour),
			},
			rotateErr: domain.ErrRefreshTokenNotFound,
		}
		authSvc := service.NewService(&unitUserStore{
			user: domain.User{
				ID:          userID,
				Email:       "demo@example.com",
				DisplayName: "Demo User",
				Status:      domain.UserStatusActive,
			},
		}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour),
			service.WithClock(func() time.Time { return now }))

		_, err := authSvc.Refresh(context.Background(), "conflicting-token")
		if !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("Refresh error = %v, want ErrInvalidToken", err)
		}
		if refreshStore.revokedFamily != familyID {
			t.Fatalf("revoked family = %s, want %s", refreshStore.revokedFamily, familyID)
		}
	})
}

func TestServiceSignOutRevokesRefreshTokenIdempotently(t *testing.T) {
	refreshStore := &unitRefreshTokenStore{}
	authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
		service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

	if err := authSvc.SignOut(context.Background(), "raw-refresh-token"); err != nil {
		t.Fatalf("SignOut with token returned error: %v", err)
	}
	if refreshStore.revokedHash == "" || strings.Contains(refreshStore.revokedHash, "raw-refresh-token") {
		t.Fatalf("revoked hash = %q, want non-raw hash", refreshStore.revokedHash)
	}

	refreshStore.revokeErr = domain.ErrRefreshTokenNotFound
	if err := authSvc.SignOut(context.Background(), "unknown-token"); err != nil {
		t.Fatalf("SignOut unknown token returned error: %v", err)
	}

	if err := authSvc.SignOut(context.Background(), ""); err != nil {
		t.Fatalf("SignOut missing token returned error: %v", err)
	}
}

func TestServiceSignOutOtherSessions(t *testing.T) {
	userID := uuid.New()
	familyID := uuid.New()

	t.Run("revokes the user's other families and keeps the current one", func(t *testing.T) {
		refreshStore := &unitRefreshTokenStore{
			token: domain.RefreshToken{UserID: userID, FamilyID: familyID},
		}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		if err := authSvc.SignOutOtherSessions(context.Background(), "raw-refresh-token"); err != nil {
			t.Fatalf("SignOutOtherSessions returned error: %v", err)
		}
		if refreshStore.revokedExceptFamilyUser != userID {
			t.Fatalf("revoked user = %s, want %s", refreshStore.revokedExceptFamilyUser, userID)
		}
		// The caller's current family is the one preserved.
		if refreshStore.revokedExceptFamily != familyID {
			t.Fatalf("kept family = %s, want the current %s", refreshStore.revokedExceptFamily, familyID)
		}
	})

	t.Run("a missing refresh token is rejected", func(t *testing.T) {
		refreshStore := &unitRefreshTokenStore{}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		if err := authSvc.SignOutOtherSessions(context.Background(), "   "); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("SignOutOtherSessions error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("an unknown refresh token is rejected", func(t *testing.T) {
		refreshStore := &unitRefreshTokenStore{getErr: domain.ErrRefreshTokenNotFound}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{issuedToken: "access-token"},
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		if err := authSvc.SignOutOtherSessions(context.Background(), "raw-refresh-token"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("SignOutOtherSessions error = %v, want ErrInvalidToken", err)
		}
	})
}

type unitUserStore struct {
	created         domain.CreateUserInput
	createCalls     int
	createErr       error
	authUser        domain.AuthUser
	authUserErr     error
	authLookupEmail string
	user            domain.User
	userErr         error
	userLookupID    uuid.UUID
	roles           []string
	permissions     []string
	addedRoles      []uuid.UUID

	registerFailureCalls  int
	registerLockedResult  bool
	registeredMaxAttempts int
	clearFailuresCalls    int
	updatedPasswordHash   string
	emailVerifiedSet      bool

	updateProfileInput domain.UpdateProfileInput
	updateProfileCalls int
}

func (s *unitUserStore) CreateUser(ctx context.Context, input domain.CreateUserInput) (domain.User, error) {
	s.createCalls++
	s.created = input
	if s.createErr != nil {
		return domain.User{}, s.createErr
	}
	return domain.User{
		ID:          input.ID,
		Email:       input.Email,
		DisplayName: input.DisplayName,
		Status:      input.Status,
		CreatedAt:   input.CreatedAt,
		UpdatedAt:   input.UpdatedAt,
	}, nil
}

func (s *unitUserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	s.userLookupID = id
	if s.userErr != nil {
		return domain.User{}, s.userErr
	}
	return s.user, nil
}

func (s *unitUserStore) GetUserByEmailForAuth(ctx context.Context, email string) (domain.AuthUser, error) {
	s.authLookupEmail = email
	if s.authUserErr != nil {
		return domain.AuthUser{}, s.authUserErr
	}
	return s.authUser, nil
}

func (s *unitUserStore) GetRoleByName(ctx context.Context, name string) (domain.Role, error) {
	return domain.Role{ID: uuid.New(), Name: name}, nil
}

func (s *unitUserStore) AddUserRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	s.addedRoles = append(s.addedRoles, roleID)
	return nil
}

func (s *unitUserStore) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.roles, nil
}

func (s *unitUserStore) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.permissions, nil
}

func (s *unitUserStore) RegisterLoginFailure(ctx context.Context, userID uuid.UUID, maxAttempts int, lockUntil time.Time, now time.Time) (bool, error) {
	s.registerFailureCalls++
	s.registeredMaxAttempts = maxAttempts
	return s.registerLockedResult, nil
}

func (s *unitUserStore) ClearLoginFailures(ctx context.Context, userID uuid.UUID, now time.Time) error {
	s.clearFailuresCalls++
	return nil
}

func (s *unitUserStore) GetUserByIDForAuth(ctx context.Context, id uuid.UUID) (domain.AuthUser, error) {
	if s.authUserErr != nil {
		return domain.AuthUser{}, s.authUserErr
	}
	return s.authUser, nil
}

func (s *unitUserStore) UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordHash string, updatedAt time.Time) error {
	s.updatedPasswordHash = passwordHash
	return nil
}

func (s *unitUserStore) SetEmailVerified(ctx context.Context, userID uuid.UUID, verifiedAt time.Time, updatedAt time.Time) error {
	s.emailVerifiedSet = true
	return nil
}

func (s *unitUserStore) UpdateUserProfile(ctx context.Context, userID uuid.UUID, input domain.UpdateProfileInput, now time.Time) error {
	s.updateProfileInput = input
	s.updateProfileCalls++
	return nil
}

type unitRefreshTokenStore struct {
	created           domain.CreateRefreshTokenInput
	createErr         error
	token             domain.RefreshToken
	getErr            error
	lookupHash        string
	rotatedOldID      uuid.UUID
	rotatedInput      domain.CreateRefreshTokenInput
	rotateErr         error
	revokedHash       string
	revokeErr         error
	revokedFamily     uuid.UUID
	revokeFamErr      error
	revokedAllForUser uuid.UUID
	revokeAllErr      error

	revokedExceptFamilyUser uuid.UUID
	revokedExceptFamily     uuid.UUID
	revokeExceptFamilyErr   error
}

func (s *unitRefreshTokenStore) CreateRefreshToken(ctx context.Context, input domain.CreateRefreshTokenInput) (domain.RefreshToken, error) {
	s.created = input
	if s.createErr != nil {
		return domain.RefreshToken{}, s.createErr
	}
	return refreshTokenFromInput(input), nil
}

func (s *unitRefreshTokenStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (domain.RefreshToken, error) {
	s.lookupHash = tokenHash
	if s.getErr != nil {
		return domain.RefreshToken{}, s.getErr
	}
	return s.token, nil
}

func (s *unitRefreshTokenStore) RotateRefreshToken(ctx context.Context, oldTokenID uuid.UUID, input domain.CreateRefreshTokenInput, revokedAt time.Time) (domain.RefreshToken, error) {
	s.rotatedOldID = oldTokenID
	s.rotatedInput = input
	if s.rotateErr != nil {
		return domain.RefreshToken{}, s.rotateErr
	}
	return refreshTokenFromInput(input), nil
}

func (s *unitRefreshTokenStore) RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	s.revokedHash = tokenHash
	return s.revokeErr
}

func (s *unitRefreshTokenStore) RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID, revokedAt time.Time) error {
	s.revokedFamily = familyID
	return s.revokeFamErr
}

func (s *unitRefreshTokenStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) error {
	s.revokedAllForUser = userID
	return s.revokeAllErr
}

func (s *unitRefreshTokenStore) RevokeAllForUserExceptFamily(ctx context.Context, userID uuid.UUID, familyID uuid.UUID, revokedAt time.Time) error {
	s.revokedExceptFamilyUser = userID
	s.revokedExceptFamily = familyID
	return s.revokeExceptFamilyErr
}

type unitOfWork struct {
	users         *unitUserStore
	refreshTokens *unitRefreshTokenStore
	authTokens    *fakeAuthTokenStore
	audit         *fakeAuditRecorder
	calls         int
	fnErr         error
	committed     bool
	rolledBack    bool
}

func (u *unitOfWork) Do(ctx context.Context, fn func(context.Context, service.WorkDeps) error) error {
	u.calls++
	if u.audit == nil {
		u.audit = &fakeAuditRecorder{}
	}
	err := fn(ctx, service.WorkDeps{
		Users:         u.users,
		RefreshTokens: u.refreshTokens,
		AuthTokens:    u.authTokens,
		Audit:         u.audit,
	})
	u.fnErr = err
	if err != nil {
		u.rolledBack = true
		return err
	}
	u.committed = true
	return nil
}

func refreshTokenFromInput(input domain.CreateRefreshTokenInput) domain.RefreshToken {
	return domain.RefreshToken{
		ID:        input.ID,
		UserID:    input.UserID,
		TokenHash: input.TokenHash,
		FamilyID:  input.FamilyID,
		ExpiresAt: input.ExpiresAt,
		CreatedAt: input.CreatedAt,
		UpdatedAt: input.UpdatedAt,
	}
}

func testDomainAuthUser(t *testing.T, id uuid.UUID, email string, displayName string, status domain.UserStatus, plainPassword string) domain.AuthUser {
	t.Helper()
	hash, err := password.Hash(plainPassword)
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	now := time.Now().UTC()
	return domain.AuthUser{
		User: domain.User{
			ID:          id,
			Email:       email,
			DisplayName: displayName,
			Status:      status,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		PasswordHash: hash,
	}
}

type fakeAuthTokenStore struct {
	created    []domain.CreateAuthTokenInput
	token      domain.AuthToken
	getErr     error
	consumeErr error
	consumed   []string
	markedUsed []uuid.UUID
	deleted    []string
}

func (s *fakeAuthTokenStore) CreateAuthToken(ctx context.Context, input domain.CreateAuthTokenInput) error {
	s.created = append(s.created, input)
	return nil
}

func (s *fakeAuthTokenStore) GetAuthTokenByHash(ctx context.Context, purpose string, tokenHash string) (domain.AuthToken, error) {
	if s.getErr != nil {
		return domain.AuthToken{}, s.getErr
	}
	return s.token, nil
}

func (s *fakeAuthTokenStore) ConsumeAuthToken(ctx context.Context, purpose string, tokenHash string, usedAt time.Time) (domain.AuthToken, error) {
	s.consumed = append(s.consumed, purpose)
	if s.consumeErr != nil {
		return domain.AuthToken{}, s.consumeErr
	}
	if s.getErr != nil {
		return domain.AuthToken{}, s.getErr
	}
	if s.token.UsedAt != nil || !s.token.ExpiresAt.After(usedAt) || (s.token.Purpose != "" && s.token.Purpose != purpose) {
		return domain.AuthToken{}, domain.ErrAuthTokenNotFound
	}
	token := s.token
	token.UsedAt = &usedAt
	s.token = token
	return token, nil
}

func (s *fakeAuthTokenStore) MarkAuthTokenUsed(ctx context.Context, id uuid.UUID, usedAt time.Time) error {
	s.markedUsed = append(s.markedUsed, id)
	return nil
}

func (s *fakeAuthTokenStore) DeleteAuthTokensForUser(ctx context.Context, userID uuid.UUID, purpose string) error {
	s.deleted = append(s.deleted, purpose)
	return nil
}

type captureEmailSender struct {
	messages []email.Message
}

func (s *captureEmailSender) Send(ctx context.Context, msg email.Message) error {
	s.messages = append(s.messages, msg)
	return nil
}

func TestServiceForgotPassword(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("issues a token and emails it for a known user", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "pw"),
		}
		tokens := &fakeAuthTokenStore{}
		mailer := &captureEmailSender{}
		authSvc := service.NewService(store, &fakeTokenManager{},
			service.WithAuthTokenStore(tokens), service.WithEmailSender(mailer))

		if err := authSvc.ForgotPassword(context.Background(), "Demo@example.com"); err != nil {
			t.Fatalf("ForgotPassword returned error: %v", err)
		}
		if len(tokens.created) != 1 || tokens.created[0].Purpose != domain.AuthTokenPurposePasswordReset {
			t.Fatalf("created tokens = %#v, want one password_reset token", tokens.created)
		}
		if len(mailer.messages) != 1 {
			t.Fatalf("sent %d emails, want 1", len(mailer.messages))
		}
	})

	t.Run("is a silent no-op for an unknown email", func(t *testing.T) {
		store := &unitUserStore{authUserErr: domain.ErrUserNotFound}
		tokens := &fakeAuthTokenStore{}
		authSvc := service.NewService(store, &fakeTokenManager{},
			service.WithAuthTokenStore(tokens), service.WithEmailSender(&captureEmailSender{}))

		if err := authSvc.ForgotPassword(context.Background(), "missing@example.com"); err != nil {
			t.Fatalf("ForgotPassword error = %v, want nil (no enumeration)", err)
		}
		if len(tokens.created) != 0 {
			t.Fatal("a token was created for an unknown email")
		}
	})
}

func TestServiceResetPassword(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	validToken := func() domain.AuthToken {
		return domain.AuthToken{
			ID:        uuid.New(),
			UserID:    userID,
			Purpose:   domain.AuthTokenPurposePasswordReset,
			ExpiresAt: time.Now().Add(time.Hour),
		}
	}

	t.Run("success updates the password and revokes sessions", func(t *testing.T) {
		store := &unitUserStore{}
		tokens := &fakeAuthTokenStore{token: validToken()}
		refreshStore := &unitRefreshTokenStore{}
		authSvc := service.NewService(store, &fakeTokenManager{},
			service.WithAuthTokenStore(tokens),
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		if err := authSvc.ResetPassword(context.Background(), "raw-token", "new-password-123"); err != nil {
			t.Fatalf("ResetPassword returned error: %v", err)
		}
		if store.updatedPasswordHash == "" {
			t.Fatal("password was not updated")
		}
		if len(tokens.consumed) != 1 {
			t.Fatalf("consumed tokens = %d, want 1", len(tokens.consumed))
		}
		if refreshStore.revokedAllForUser != userID {
			t.Fatalf("revokedAllForUser = %s, want %s", refreshStore.revokedAllForUser, userID)
		}
	})

	t.Run("session revocation failure is returned", func(t *testing.T) {
		revokeErr := errors.New("revoke failed")
		tokens := &fakeAuthTokenStore{token: validToken()}
		refreshStore := &unitRefreshTokenStore{revokeAllErr: revokeErr}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{},
			service.WithAuthTokenStore(tokens),
			service.WithRefreshTokenStore(refreshStore, 14*24*time.Hour))

		err := authSvc.ResetPassword(context.Background(), "raw-token", "new-password-123")
		if !errors.Is(err, revokeErr) {
			t.Fatalf("ResetPassword error = %v, want revoke error", err)
		}
	})

	t.Run("rejects an expired, used, or unknown token", func(t *testing.T) {
		expired := validToken()
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		usedAt := time.Now()
		used := validToken()
		used.UsedAt = &usedAt

		cases := map[string]*fakeAuthTokenStore{
			"expired": {token: expired},
			"used":    {token: used},
			"unknown": {getErr: domain.ErrAuthTokenNotFound},
		}
		for name, tokens := range cases {
			t.Run(name, func(t *testing.T) {
				store := &unitUserStore{}
				authSvc := service.NewService(store, &fakeTokenManager{}, service.WithAuthTokenStore(tokens))
				if err := authSvc.ResetPassword(context.Background(), "raw-token", "new-password-123"); !errors.Is(err, service.ErrInvalidToken) {
					t.Fatalf("ResetPassword error = %v, want ErrInvalidToken", err)
				}
				if store.updatedPasswordHash != "" {
					t.Fatal("password was updated despite an invalid token")
				}
			})
		}
	})

	t.Run("rejects a short new password", func(t *testing.T) {
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{},
			service.WithAuthTokenStore(&fakeAuthTokenStore{token: validToken()}))
		if err := authSvc.ResetPassword(context.Background(), "raw-token", "short"); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("ResetPassword error = %v, want ErrInvalidInput", err)
		}
	})
}

func TestServiceVerifyEmail(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	t.Run("success marks the email verified", func(t *testing.T) {
		store := &unitUserStore{}
		tokens := &fakeAuthTokenStore{token: domain.AuthToken{
			ID:        uuid.New(),
			UserID:    userID,
			Purpose:   domain.AuthTokenPurposeEmailVerification,
			ExpiresAt: time.Now().Add(time.Hour),
		}}
		authSvc := service.NewService(store, &fakeTokenManager{}, service.WithAuthTokenStore(tokens))

		if err := authSvc.VerifyEmail(context.Background(), "raw-token"); err != nil {
			t.Fatalf("VerifyEmail returned error: %v", err)
		}
		if !store.emailVerifiedSet {
			t.Fatal("email was not marked verified")
		}
		if len(tokens.consumed) != 1 {
			t.Fatalf("consumed tokens = %d, want 1", len(tokens.consumed))
		}
	})

	t.Run("rejects an unknown token", func(t *testing.T) {
		store := &unitUserStore{}
		tokens := &fakeAuthTokenStore{getErr: domain.ErrAuthTokenNotFound}
		authSvc := service.NewService(store, &fakeTokenManager{}, service.WithAuthTokenStore(tokens))
		if err := authSvc.VerifyEmail(context.Background(), "raw-token"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("VerifyEmail error = %v, want ErrInvalidToken", err)
		}
		if store.emailVerifiedSet {
			t.Fatal("email was marked verified despite an unknown token")
		}
	})
}

func TestServiceResetVerifyAuditEvents(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")

	hasEvent := func(events []service.AuditEvent, eventType string) bool {
		for _, e := range events {
			if e.EventType == eventType {
				return true
			}
		}
		return false
	}

	t.Run("forgot-password records reset_requested for a known user", func(t *testing.T) {
		store := &unitUserStore{
			authUser: testDomainAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "pw"),
		}
		audit := &fakeAuditRecorder{}
		authSvc := service.NewService(store, &fakeTokenManager{},
			service.WithAuthTokenStore(&fakeAuthTokenStore{}),
			service.WithEmailSender(&captureEmailSender{}),
			service.WithAuditRecorder(audit))

		if err := authSvc.ForgotPassword(context.Background(), "Demo@example.com"); err != nil {
			t.Fatalf("ForgotPassword returned error: %v", err)
		}
		if !hasEvent(audit.events, service.EventAuthPasswordResetRequested) {
			t.Fatalf("audit events = %#v, want a %s", audit.events, service.EventAuthPasswordResetRequested)
		}
	})

	t.Run("reset-password records reset_failed on a bad token", func(t *testing.T) {
		audit := &fakeAuditRecorder{}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{},
			service.WithAuthTokenStore(&fakeAuthTokenStore{getErr: domain.ErrAuthTokenNotFound}),
			service.WithAuditRecorder(audit))

		if err := authSvc.ResetPassword(context.Background(), "raw-token", "new-password-123"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("ResetPassword error = %v, want ErrInvalidToken", err)
		}
		if !hasEvent(audit.events, service.EventAuthPasswordResetFailed) {
			t.Fatalf("audit events = %#v, want a %s", audit.events, service.EventAuthPasswordResetFailed)
		}
	})

	t.Run("verify-email records verification_failed on a bad token", func(t *testing.T) {
		audit := &fakeAuditRecorder{}
		authSvc := service.NewService(&unitUserStore{}, &fakeTokenManager{},
			service.WithAuthTokenStore(&fakeAuthTokenStore{getErr: domain.ErrAuthTokenNotFound}),
			service.WithAuditRecorder(audit))

		if err := authSvc.VerifyEmail(context.Background(), "raw-token"); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("VerifyEmail error = %v, want ErrInvalidToken", err)
		}
		if !hasEvent(audit.events, service.EventAuthEmailVerificationFailed) {
			t.Fatalf("audit events = %#v, want a %s", audit.events, service.EventAuthEmailVerificationFailed)
		}
	})
}

func TestServiceUpdateMyProfile(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	baseUser := domain.User{
		ID:          userID,
		Email:       "demo@example.com",
		DisplayName: "Demo",
		Status:      domain.UserStatusActive,
	}

	t.Run("success trims fields and refetches the user", func(t *testing.T) {
		store := &unitUserStore{user: baseUser}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())})

		newName := "  Demo User  "
		newPhone := "+1 555 0100"
		updated, err := authSvc.UpdateMyProfile(context.Background(), "raw-token", service.UpdateMyProfileInput{
			DisplayName: &newName,
			Phone:       &newPhone,
		})
		if err != nil {
			t.Fatalf("UpdateMyProfile returned error: %v", err)
		}
		if updated == nil {
			t.Fatal("UpdateMyProfile returned nil user")
		}
		if store.updateProfileCalls != 1 {
			t.Fatalf("store.UpdateUserProfile called %d times, want 1", store.updateProfileCalls)
		}
		if store.updateProfileInput.DisplayName == nil || *store.updateProfileInput.DisplayName != "Demo User" {
			t.Fatalf("DisplayName forwarded as %v, want trimmed value", store.updateProfileInput.DisplayName)
		}
		if store.updateProfileInput.Phone == nil || *store.updateProfileInput.Phone != "+1 555 0100" {
			t.Fatalf("Phone forwarded as %v", store.updateProfileInput.Phone)
		}
		// Status and roles cannot be touched through this path.
		if store.updateProfileInput.AvatarURL != nil {
			t.Fatalf("AvatarURL forwarded as %v despite not being provided", store.updateProfileInput.AvatarURL)
		}
	})

	t.Run("rejects an empty display name", func(t *testing.T) {
		store := &unitUserStore{user: baseUser}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())})
		empty := "   "
		if _, err := authSvc.UpdateMyProfile(context.Background(), "raw-token", service.UpdateMyProfileInput{
			DisplayName: &empty,
		}); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("UpdateMyProfile error = %v, want ErrInvalidInput", err)
		}
		if store.updateProfileCalls != 0 {
			t.Fatal("store.UpdateUserProfile was called despite an empty display name")
		}
	})

	t.Run("rejects an overlong field", func(t *testing.T) {
		store := &unitUserStore{user: baseUser}
		authSvc := service.NewService(store, &fakeTokenManager{claims: testClaims(userID.String())})
		oversize := strings.Repeat("x", 257)
		if _, err := authSvc.UpdateMyProfile(context.Background(), "raw-token", service.UpdateMyProfileInput{
			JobTitle: &oversize,
		}); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("UpdateMyProfile error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("rejects an invalid token", func(t *testing.T) {
		store := &unitUserStore{user: baseUser}
		authSvc := service.NewService(store, &fakeTokenManager{verifyErr: errors.New("bad token")})
		newName := "Demo User"
		if _, err := authSvc.UpdateMyProfile(context.Background(), "bad-token", service.UpdateMyProfileInput{
			DisplayName: &newName,
		}); !errors.Is(err, service.ErrInvalidToken) {
			t.Fatalf("UpdateMyProfile error = %v, want ErrInvalidToken", err)
		}
	})
}
