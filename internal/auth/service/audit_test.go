package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestAuthAuditRecordsSignUpSuccess(t *testing.T) {
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{}, &fakeTokenManager{issuedToken: "raw-access-token-secret"},
		service.WithAuditRecorder(audit))

	session, err := authSvc.SignUp(testRequestContext(), service.SignUpInput{
		Email:     "New.User@Example.com",
		Password:  "secure-password",
		FirstName: "New",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("SignUp returned error: %v", err)
	}

	event := audit.singleEvent(t)
	if event.EventType != service.EventAuthSignUpSucceeded {
		t.Fatalf("event type = %q, want %q", event.EventType, service.EventAuthSignUpSucceeded)
	}
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "masked_email", "n***@example.com")
	assertMetadataValue(t, metadata, "user_id", session.User.ID)
	assertMetadataValue(t, metadata, "request_id", "req-audit-123")
	assertNoAuditMetadataLeaks(t, event, "secure-password", "password", "password_hash", "raw-access-token-secret", "new.user@example.com")
}

func TestAuthAuditRecordsSignUpFailure(t *testing.T) {
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{}, &fakeTokenManager{issuedToken: "token"},
		service.WithAuditRecorder(audit))

	_, err := authSvc.SignUp(testRequestContext(), service.SignUpInput{
		Email:     "bad-email",
		Password:  "secret-password",
		FirstName: "Bad",
		LastName:  "Email",
	})
	if err == nil {
		t.Fatal("SignUp returned nil error, want invalid input")
	}

	event := audit.singleEvent(t)
	if event.EventType != service.EventAuthSignUpFailed {
		t.Fatalf("event type = %q, want %q", event.EventType, service.EventAuthSignUpFailed)
	}
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "masked_email", "***")
	assertMetadataValue(t, metadata, "reason", service.AuditReasonInvalidInput)
	assertMetadataValue(t, metadata, "request_id", "req-audit-123")
	assertNoAuditMetadataLeaks(t, event, "secret-password", "password", "password_hash")
}

func TestAuthAuditRecordsDuplicateSignUpFailureWithoutSyntheticUserID(t *testing.T) {
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{
		createUserErr: domain.ErrEmailAlreadyExists,
	}, &fakeTokenManager{issuedToken: "token"}, service.WithAuditRecorder(audit))

	_, err := authSvc.SignUp(testRequestContext(), service.SignUpInput{
		Email:     "existing@example.com",
		Password:  "secret-password",
		FirstName: "Existing",
		LastName:  "User",
	})
	if !errors.Is(err, service.ErrEmailAlreadyExists) {
		t.Fatalf("SignUp error = %v, want ErrEmailAlreadyExists", err)
	}

	event := audit.singleEvent(t)
	if event.EventType != service.EventAuthSignUpFailed {
		t.Fatalf("event type = %q, want %q", event.EventType, service.EventAuthSignUpFailed)
	}
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "masked_email", "e***@example.com")
	assertMetadataValue(t, metadata, "reason", service.AuditReasonEmailAlreadyExists)
	if metadata["user_id"] != "" {
		t.Fatalf("metadata user_id = %q, want omitted because existing user id is unknown", metadata["user_id"])
	}
	assertNoAuditMetadataLeaks(t, event, "secret-password", "password", "password_hash", "duplicate key", "SQLSTATE")
}

func TestAuthAuditRecordsSignInSuccess(t *testing.T) {
	userID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{
		authUser: testAuthUser(t, userID, "demo@example.com", "Demo User", domain.UserStatusActive, "correct-password"),
	}, &fakeTokenManager{issuedToken: "raw-access-token-secret"}, service.WithAuditRecorder(audit))

	session, err := authSvc.SignIn(testRequestContext(), service.SignInInput{
		Email:    "DEMO@example.com",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("SignIn returned error: %v", err)
	}

	event := audit.singleEvent(t)
	if event.EventType != service.EventAuthSignInSucceeded {
		t.Fatalf("event type = %q, want %q", event.EventType, service.EventAuthSignInSucceeded)
	}
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "masked_email", "d***@example.com")
	assertMetadataValue(t, metadata, "user_id", session.User.ID)
	assertMetadataValue(t, metadata, "request_id", "req-audit-123")
	assertNoAuditMetadataLeaks(t, event, "correct-password", "password", "password_hash", "raw-access-token-secret", "demo@example.com")
}

func TestAuthAuditRecordsSignInFailure(t *testing.T) {
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{
		authUserErr: domain.ErrUserNotFound,
	}, &fakeTokenManager{issuedToken: "token"}, service.WithAuditRecorder(audit))

	_, err := authSvc.SignIn(testRequestContext(), service.SignInInput{
		Email:    "missing@example.com",
		Password: "wrong-password",
	})
	if err == nil {
		t.Fatal("SignIn returned nil error, want invalid credentials")
	}

	event := audit.singleEvent(t)
	if event.EventType != service.EventAuthSignInFailed {
		t.Fatalf("event type = %q, want %q", event.EventType, service.EventAuthSignInFailed)
	}
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "masked_email", "m***@example.com")
	assertMetadataValue(t, metadata, "reason", service.AuditReasonInvalidCredentials)
	assertMetadataValue(t, metadata, "request_id", "req-audit-123")
	assertNoAuditMetadataLeaks(t, event, "wrong-password", "password", "password_hash")
}

func TestAuthAuditMetadataDoesNotLeakRawInternalErrors(t *testing.T) {
	audit := &fakeAuditRecorder{}
	authSvc := service.NewService(&fakeUserStore{
		authUserErr: errors.New("database exploded with password_hash and SQLSTATE 23505"),
	}, &fakeTokenManager{issuedToken: "token"}, service.WithAuditRecorder(audit))

	_, err := authSvc.SignIn(testRequestContext(), service.SignInInput{
		Email:    "demo@example.com",
		Password: "secret-password",
	})
	if err == nil {
		t.Fatal("SignIn returned nil error, want internal retrieval error")
	}

	event := audit.singleEvent(t)
	metadata := eventMetadata(t, event)
	assertMetadataValue(t, metadata, "reason", service.AuditReasonInternalError)
	assertNoAuditMetadataLeaks(t, event,
		"secret-password",
		"password",
		"password_hash",
		"database exploded",
		"SQLSTATE",
		"23505",
	)
}

func TestAuthAuditRecorderFailureDoesNotChangeAuthResults(t *testing.T) {
	t.Run("success still succeeds", func(t *testing.T) {
		audit := &fakeAuditRecorder{err: errors.New("audit insert failed")}
		authSvc := service.NewService(&fakeUserStore{}, &fakeTokenManager{issuedToken: "token"},
			service.WithAuditRecorder(audit))

		session, err := authSvc.SignUp(testRequestContext(), service.SignUpInput{
			Email:     "audit-failure@example.com",
			Password:  "secure-password",
			FirstName: "Audit",
			LastName:  "Failure",
		})
		if err != nil {
			t.Fatalf("SignUp returned error: %v", err)
		}
		if session == nil || session.AccessToken != "token" {
			t.Fatalf("SignUp session = %#v, want token session", session)
		}
	})

	t.Run("failure keeps original auth error", func(t *testing.T) {
		audit := &fakeAuditRecorder{err: errors.New("audit insert failed")}
		authSvc := service.NewService(&fakeUserStore{
			authUserErr: domain.ErrUserNotFound,
		}, &fakeTokenManager{issuedToken: "token"}, service.WithAuditRecorder(audit))

		_, err := authSvc.SignIn(testRequestContext(), service.SignInInput{
			Email:    "missing@example.com",
			Password: "wrong-password",
		})
		if !errors.Is(err, service.ErrInvalidCredentials) {
			t.Fatalf("SignIn error = %v, want ErrInvalidCredentials", err)
		}
	})
}

type fakeAuditRecorder struct {
	events []service.AuditEvent
	err    error
}

func (f *fakeAuditRecorder) RecordAuthEvent(ctx context.Context, event service.AuditEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func (f *fakeAuditRecorder) singleEvent(t *testing.T) service.AuditEvent {
	t.Helper()
	if len(f.events) != 1 {
		t.Fatalf("recorded %d audit events, want 1: %#v", len(f.events), f.events)
	}
	return f.events[0]
}

type fakeUserStore struct {
	createUserErr error
	authUser      domain.AuthUser
	authUserErr   error
	user          domain.User
	userErr       error
	roles         []string
	permissions   []string
}

func (f *fakeUserStore) CreateUser(ctx context.Context, input domain.CreateUserInput) (domain.User, error) {
	if f.createUserErr != nil {
		return domain.User{}, f.createUserErr
	}
	return domain.User{
		ID:          input.ID,
		Email:       strings.ToLower(input.Email),
		DisplayName: input.DisplayName,
		Status:      input.Status,
		CreatedAt:   input.CreatedAt,
		UpdatedAt:   input.UpdatedAt,
	}, nil
}

func (f *fakeUserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	if f.userErr != nil {
		return domain.User{}, f.userErr
	}
	return f.user, nil
}

func (f *fakeUserStore) GetUserByEmailForAuth(ctx context.Context, email string) (domain.AuthUser, error) {
	if f.authUserErr != nil {
		return domain.AuthUser{}, f.authUserErr
	}
	return f.authUser, nil
}

func (f *fakeUserStore) GetRoleByName(ctx context.Context, name string) (domain.Role, error) {
	return domain.Role{ID: uuid.New(), Name: name}, nil
}

func (f *fakeUserStore) AddUserRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	return nil
}

func (f *fakeUserStore) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return f.roles, nil
}

func (f *fakeUserStore) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return f.permissions, nil
}

func (f *fakeUserStore) RegisterLoginFailure(ctx context.Context, userID uuid.UUID, maxAttempts int, lockUntil time.Time, now time.Time) (bool, error) {
	return false, nil
}

func (f *fakeUserStore) ClearLoginFailures(ctx context.Context, userID uuid.UUID, now time.Time) error {
	return nil
}

func (f *fakeUserStore) GetUserByIDForAuth(ctx context.Context, id uuid.UUID) (domain.AuthUser, error) {
	if f.authUserErr != nil {
		return domain.AuthUser{}, f.authUserErr
	}
	return f.authUser, nil
}

func (f *fakeUserStore) UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordHash string, updatedAt time.Time) error {
	return nil
}

func (f *fakeUserStore) SetEmailVerified(ctx context.Context, userID uuid.UUID, verifiedAt time.Time, updatedAt time.Time) error {
	return nil
}

func (f *fakeUserStore) UpdateUserProfile(ctx context.Context, userID uuid.UUID, input domain.UpdateProfileInput, now time.Time) error {
	return nil
}

type fakeTokenManager struct {
	issuedToken string
	issueErr    error
	claims      *token.Claims
	verifyErr   error

	impersonationTarget string
	impersonationActor  string
}

func (f *fakeTokenManager) IssueAccessToken(userID string) (string, error) {
	if f.issueErr != nil {
		return "", f.issueErr
	}
	return f.issuedToken, nil
}

func (f *fakeTokenManager) IssueImpersonationToken(targetID, actorID string) (string, error) {
	if f.issueErr != nil {
		return "", f.issueErr
	}
	f.impersonationTarget = targetID
	f.impersonationActor = actorID
	return f.issuedToken, nil
}

func (f *fakeTokenManager) VerifyAccessToken(raw string) (*token.Claims, error) {
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return f.claims, nil
}

func (f *fakeTokenManager) ParseClaimsAllowExpired(raw string) (*token.Claims, error) {
	if f.claims == nil {
		return &token.Claims{}, nil
	}
	return f.claims, nil
}

func testAuthUser(t *testing.T, id uuid.UUID, email string, displayName string, status domain.UserStatus, plainPassword string) domain.AuthUser {
	t.Helper()

	hash, err := password.Hash(plainPassword)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
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

func testClaims(subject string) *token.Claims {
	return &token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: subject,
		},
	}
}

func testRequestContext() context.Context {
	return context.WithValue(context.Background(), middleware.RequestIDKey, "req-audit-123")
}

func eventMetadata(t *testing.T, event service.AuditEvent) map[string]string {
	t.Helper()

	data, err := json.Marshal(event.Metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("failed to unmarshal metadata %q: %v", data, err)
	}
	return metadata
}

func assertMetadataValue(t *testing.T, metadata map[string]string, key string, want string) {
	t.Helper()
	if got := metadata[key]; got != want {
		t.Fatalf("metadata[%q] = %q, want %q; metadata=%v", key, got, want, metadata)
	}
}

func assertNoAuditMetadataLeaks(t *testing.T, event service.AuditEvent, forbidden ...string) {
	t.Helper()

	data, err := json.Marshal(event.Metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}
	raw := strings.ToLower(string(data))
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(raw, strings.ToLower(secret)) {
			t.Fatalf("audit metadata leaks %q: %s", secret, data)
		}
	}
}
