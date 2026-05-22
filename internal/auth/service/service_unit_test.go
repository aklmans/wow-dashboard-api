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

type unitRefreshTokenStore struct {
	created       domain.CreateRefreshTokenInput
	createErr     error
	token         domain.RefreshToken
	getErr        error
	lookupHash    string
	rotatedOldID  uuid.UUID
	rotatedInput  domain.CreateRefreshTokenInput
	rotateErr     error
	revokedHash   string
	revokeErr     error
	revokedFamily uuid.UUID
	revokeFamErr  error
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

type unitOfWork struct {
	users         *unitUserStore
	refreshTokens *unitRefreshTokenStore
	calls         int
	fnErr         error
	committed     bool
	rolledBack    bool
}

func (u *unitOfWork) Do(ctx context.Context, fn func(context.Context, service.WorkDeps) error) error {
	u.calls++
	err := fn(ctx, service.WorkDeps{
		Users:         u.users,
		RefreshTokens: u.refreshTokens,
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
