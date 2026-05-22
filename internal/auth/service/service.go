// Package service implements the core authentication and authorization business service layer.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/google/uuid"
)

// Domain sentinel errors for upstream mapping to API error envelopes.
var (
	ErrInvalidInput       = errors.New("auth: invalid input")
	ErrEmailAlreadyExists = errors.New("auth: email already exists")
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrUserDisabled       = errors.New("auth: user disabled")
	ErrInvalidToken       = errors.New("auth: invalid token")
	ErrUserNotFound       = errors.New("auth: user not found")
)

const (
	// minPasswordLength is the minimum accepted password length at sign-up.
	// Eight characters is the OWASP baseline minimum.
	minPasswordLength = 8
	// maxPasswordLength caps password input length. Argon2id cost scales with
	// input size, so an unbounded password is a CPU/memory DoS vector; the cap
	// is enforced on both sign-up and sign-in before any hashing work.
	maxPasswordLength = 4096
	// maxFailedLoginAttempts is the number of consecutive failed sign-ins that
	// locks an account. It is per-account, complementing the per-IP limiter.
	maxFailedLoginAttempts = 10
	// accountLockoutWindow is how long an account stays locked after reaching
	// the failure threshold. The lock is self-healing — it simply expires.
	accountLockoutWindow = 15 * time.Minute
)

// defaultRoleName is the role assigned to every newly registered user.
const defaultRoleName = "user"

// PublicUser defines the client-facing user presentation. It never exposes
// password hashes or other sensitive internal state. Roles and Permissions
// are populated by CurrentUser; sign-in/sign-up leave them empty (clients
// fetch the resolved set from /api/auth/me).
type PublicUser struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	Status      string   `json:"status,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// Session represents a successful authentication result containing both the
// PublicUser profile and the newly generated access token.
type Session struct {
	User         PublicUser `json:"user"`
	AccessToken  string     `json:"accessToken"`
	RefreshToken string     `json:"-"`
}

// SignUpInput defines the inputs required to register a new user.
type SignUpInput struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
}

// SignInInput defines the credentials required to log in.
type SignInInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserStore defines the subset of repository methods required by the auth service.
type UserStore interface {
	CreateUser(ctx context.Context, input domain.CreateUserInput) (domain.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	GetUserByEmailForAuth(ctx context.Context, email string) (domain.AuthUser, error)
	GetRoleByName(ctx context.Context, name string) (domain.Role, error)
	AddUserRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error
	GetUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error)
	GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error)
	RegisterLoginFailure(ctx context.Context, userID uuid.UUID, maxAttempts int, lockUntil time.Time, now time.Time) (bool, error)
	ClearLoginFailures(ctx context.Context, userID uuid.UUID, now time.Time) error
}

// RefreshTokenStore defines the persistence port for opaque refresh tokens.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, input domain.CreateRefreshTokenInput) (domain.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (domain.RefreshToken, error)
	RotateRefreshToken(ctx context.Context, oldTokenID uuid.UUID, input domain.CreateRefreshTokenInput, revokedAt time.Time) (domain.RefreshToken, error)
	RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt time.Time) error
	RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID, revokedAt time.Time) error
}

// WorkDeps contains transaction-scoped stores for a unit of work.
type WorkDeps struct {
	Users         UserStore
	RefreshTokens RefreshTokenStore
}

// UnitOfWork defines the transactional boundary needed by auth workflows.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context, WorkDeps) error) error
}

// TokenManager defines the capabilities needed to issue and verify access tokens.
type TokenManager interface {
	IssueAccessToken(userID string) (string, error)
	VerifyAccessToken(raw string) (*token.Claims, error)
}

// Service provides the orchestration layer for auth business logic.
type Service struct {
	store             UserStore
	refreshTokenStore RefreshTokenStore
	refreshTokenTTL   time.Duration
	unitOfWork        UnitOfWork
	tokenManager      TokenManager
	auditRecorder     AuditRecorder
	now               func() time.Time
}

// Option configures Service dependencies.
type Option func(*Service)

// WithAuditRecorder configures best-effort auth audit event recording.
func WithAuditRecorder(recorder AuditRecorder) Option {
	return func(s *Service) {
		if recorder != nil {
			s.auditRecorder = recorder
		}
	}
}

// WithRefreshTokenStore configures persistent opaque refresh tokens.
func WithRefreshTokenStore(store RefreshTokenStore, ttl time.Duration) Option {
	return func(s *Service) {
		if store != nil {
			s.refreshTokenStore = store
		}
		if ttl > 0 {
			s.refreshTokenTTL = ttl
		}
	}
}

// WithUnitOfWork configures transactional auth store operations.
func WithUnitOfWork(uow UnitOfWork) Option {
	return func(s *Service) {
		if uow != nil {
			s.unitOfWork = uow
		}
	}
}

// WithClock configures the service clock for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService constructs a new Service instance with injected dependencies.
func NewService(store UserStore, tokenManager TokenManager, opts ...Option) *Service {
	s := &Service{
		store:           store,
		refreshTokenTTL: 14 * 24 * time.Hour,
		tokenManager:    tokenManager,
		auditRecorder:   noopAuditRecorder{},
		now:             time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SignUp creates a new user profile, validates its fields, hashes the password using Argon2id,
// persists the user inside the database, and issues an access token.
func (s *Service) SignUp(ctx context.Context, input SignUpInput) (*Session, error) {
	// Normalize email to trim and lowercase
	email := strings.ToLower(strings.TrimSpace(input.Email))

	// Basic email validation
	if !validateEmail(email) {
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidInput})
		return nil, fmt.Errorf("%w: invalid email address", ErrInvalidInput)
	}

	// Validate password length
	if len(input.Password) < minPasswordLength {
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidInput})
		return nil, fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLength)
	}
	if len(input.Password) > maxPasswordLength {
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidInput})
		return nil, fmt.Errorf("%w: password must not exceed %d characters", ErrInvalidInput, maxPasswordLength)
	}

	// Trim and validate names
	firstName := strings.TrimSpace(input.FirstName)
	lastName := strings.TrimSpace(input.LastName)
	if firstName == "" || lastName == "" {
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidInput})
		return nil, fmt.Errorf("%w: first and last names are required", ErrInvalidInput)
	}

	displayName := firstName + " " + lastName

	// Hash password via Argon2id
	hashedPassword, err := password.Hash(input.Password)
	if err != nil {
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInternalError})
		return nil, fmt.Errorf("auth: failed to hash password: %w", err)
	}

	// Generate UUID explicitly in application layer
	userID := uuid.New()

	now := s.now().UTC().Truncate(time.Microsecond)

	createUserInput := domain.CreateUserInput{
		ID:           userID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: hashedPassword,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if s.refreshTokenStore != nil && s.unitOfWork != nil {
		session, err := s.signUpWithUnitOfWork(ctx, createUserInput)
		if err != nil {
			if errors.Is(err, domain.ErrEmailAlreadyExists) {
				s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonEmailAlreadyExists})
				return nil, ErrEmailAlreadyExists
			}
			s.recordSignUpFailed(ctx, AuditMetadata{
				Email:  createUserInput.Email,
				UserID: createUserInput.ID.String(),
				Reason: AuditReasonInternalError,
			})
			return nil, fmt.Errorf("auth: failed to create user session: %w", err)
		}

		s.recordSignUpSucceeded(ctx, AuditMetadata{
			Email:  session.User.Email,
			UserID: session.User.ID,
		})
		return session, nil
	}

	// Create user row inside database
	createdUser, err := s.store.CreateUser(ctx, createUserInput)
	if err != nil {
		if errors.Is(err, domain.ErrEmailAlreadyExists) {
			s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonEmailAlreadyExists})
			return nil, ErrEmailAlreadyExists
		}
		s.recordSignUpFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInternalError})
		return nil, fmt.Errorf("auth: failed to create user: %w", err)
	}

	if err := assignDefaultRole(ctx, s.store, createdUser.ID); err != nil {
		s.recordSignUpFailed(ctx, AuditMetadata{
			Email:  createdUser.Email,
			UserID: createdUser.ID.String(),
			Reason: AuditReasonInternalError,
		})
		return nil, fmt.Errorf("auth: failed to assign default role: %w", err)
	}

	session, err := s.issueSession(ctx, createdUser, uuid.Nil)
	if err != nil {
		s.recordSignUpFailed(ctx, AuditMetadata{
			Email:  createdUser.Email,
			UserID: createdUser.ID.String(),
			Reason: AuditReasonInternalError,
		})
		return nil, fmt.Errorf("auth: failed to issue session: %w", err)
	}

	s.recordSignUpSucceeded(ctx, AuditMetadata{
		Email:  session.User.Email,
		UserID: session.User.ID,
	})
	return session, nil
}

// assignDefaultRole grants the newly registered user the default "user" role.
func assignDefaultRole(ctx context.Context, users UserStore, userID uuid.UUID) error {
	role, err := users.GetRoleByName(ctx, defaultRoleName)
	if err != nil {
		return err
	}
	return users.AddUserRole(ctx, userID, role.ID)
}

func (s *Service) signUpWithUnitOfWork(ctx context.Context, input domain.CreateUserInput) (*Session, error) {
	accessToken, err := s.tokenManager.IssueAccessToken(input.ID.String())
	if err != nil {
		return nil, err
	}

	rawRefreshToken, refreshInput, err := s.newRefreshTokenInput(input.ID, uuid.Nil)
	if err != nil {
		return nil, err
	}

	var createdUser domain.User
	err = s.unitOfWork.Do(ctx, func(ctx context.Context, deps WorkDeps) error {
		if deps.Users == nil || deps.RefreshTokens == nil {
			return fmt.Errorf("auth: unit of work missing auth stores")
		}

		var err error
		createdUser, err = deps.Users.CreateUser(ctx, input)
		if err != nil {
			return err
		}

		if err := assignDefaultRole(ctx, deps.Users, createdUser.ID); err != nil {
			return err
		}

		refreshInput.UserID = createdUser.ID
		_, err = deps.RefreshTokens.CreateRefreshToken(ctx, refreshInput)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &Session{
		User:         publicUserFromDomain(createdUser),
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
	}, nil
}

// SignIn verifies user credentials against database stores, handles login attempts,
// enforces timing side-channel protection, and issues a JWT token.
func (s *Service) SignIn(ctx context.Context, input SignInInput) (*Session, error) {
	// Normalize email
	email := strings.ToLower(strings.TrimSpace(input.Email))
	now := s.now().UTC().Truncate(time.Microsecond)

	// Reject over-long passwords before any store lookup or hashing work so an
	// oversized input cannot drive an Argon2id CPU/memory DoS.
	if len(input.Password) > maxPasswordLength {
		s.recordSignInFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidCredentials})
		return nil, ErrInvalidCredentials
	}

	// Get auth record
	user, err := s.store.GetUserByEmailForAuth(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			// Mitigate timing side-channels by performing a dummy verification
			_ = dummyVerify(input.Password)
			s.recordSignInFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInvalidCredentials})
			return nil, ErrInvalidCredentials
		}
		s.recordSignInFailed(ctx, AuditMetadata{Email: email, Reason: AuditReasonInternalError})
		return nil, fmt.Errorf("auth: failed to retrieve credentials: %w", err)
	}

	userIDStr := user.ID.String()

	// A locked account is rejected with the same generic error as bad
	// credentials so the lock state cannot be probed; the dummy verify keeps
	// the response time comparable to a genuine verification.
	if user.LockedUntil != nil && user.LockedUntil.After(now) {
		_ = dummyVerify(input.Password)
		s.recordSignInFailed(ctx, AuditMetadata{Email: email, UserID: userIDStr, Reason: AuditReasonAccountLocked})
		return nil, ErrInvalidCredentials
	}

	// Verify hashed password
	match, err := password.Verify(input.Password, user.PasswordHash)
	if err != nil {
		s.recordSignInFailed(ctx, AuditMetadata{
			Email:  email,
			UserID: userIDStr,
			Reason: AuditReasonInternalError,
		})
		return nil, fmt.Errorf("auth: failed to verify password: %w", err)
	}
	if !match {
		// Count the failure; the store locks the account once the threshold is
		// reached. A failure-counter error must not change the (already
		// failed) sign-in outcome, so it only influences the audit reason.
		reason := AuditReasonInvalidCredentials
		if locked, ferr := s.store.RegisterLoginFailure(ctx, user.ID, maxFailedLoginAttempts, now.Add(accountLockoutWindow), now); ferr != nil {
			slog.ErrorContext(ctx, "failed to record login failure", "error", ferr)
		} else if locked {
			reason = AuditReasonAccountLocked
		}
		s.recordSignInFailed(ctx, AuditMetadata{
			Email:  email,
			UserID: userIDStr,
			Reason: reason,
		})
		return nil, ErrInvalidCredentials
	}

	// Check status. A disabled account returns the same generic invalid
	// credentials error as a wrong password so sign-in cannot be used to
	// enumerate which accounts exist; the real reason is kept in the audit
	// event, which is internal-only.
	if user.Status == domain.UserStatusDisabled {
		s.recordSignInFailed(ctx, AuditMetadata{
			Email:  email,
			UserID: userIDStr,
			Reason: AuditReasonUserDisabled,
		})
		return nil, ErrInvalidCredentials
	}

	// The credentials are valid: clear any accumulated failure state. This is
	// best-effort — a clear failure does not invalidate a successful sign-in.
	if user.FailedLoginCount > 0 || user.LockedUntil != nil {
		if cerr := s.store.ClearLoginFailures(ctx, user.ID, now); cerr != nil {
			slog.ErrorContext(ctx, "failed to clear login failures", "error", cerr)
		}
	}

	session, err := s.issueSession(ctx, user.User, uuid.Nil)
	if err != nil {
		s.recordSignInFailed(ctx, AuditMetadata{
			Email:  email,
			UserID: userIDStr,
			Reason: AuditReasonInternalError,
		})
		return nil, fmt.Errorf("auth: failed to issue session: %w", err)
	}
	s.recordSignInSucceeded(ctx, AuditMetadata{
		Email:  session.User.Email,
		UserID: session.User.ID,
	})
	return session, nil
}

// Refresh validates and rotates an opaque refresh token, returning a new session.
func (s *Service) Refresh(ctx context.Context, rawRefreshToken string) (*Session, error) {
	if s.refreshTokenStore == nil {
		return nil, ErrInvalidToken
	}

	rawRefreshToken = strings.TrimSpace(rawRefreshToken)
	if rawRefreshToken == "" {
		return nil, ErrInvalidToken
	}

	currentToken, err := s.refreshTokenStore.GetRefreshTokenByHash(ctx, hashRefreshToken(rawRefreshToken))
	if err != nil {
		if errors.Is(err, domain.ErrRefreshTokenNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("auth: failed to retrieve refresh token: %w", err)
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	if currentToken.RevokedAt != nil {
		// An already-revoked refresh token being presented again is the
		// classic refresh-token-reuse signal: a legitimate rotation already
		// consumed this token, so whoever replays it now is using a stolen
		// value. Revoke the entire token family so the descendant token
		// minted by that rotation is invalidated too, forcing re-auth.
		if err := s.refreshTokenStore.RevokeRefreshTokenFamily(ctx, currentToken.FamilyID, now); err != nil {
			return nil, fmt.Errorf("auth: failed to revoke reused refresh token family: %w", err)
		}
		return nil, ErrInvalidToken
	}
	if !currentToken.ExpiresAt.After(now) {
		return nil, ErrInvalidToken
	}

	user, err := s.store.GetUserByID(ctx, currentToken.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, fmt.Errorf("auth: failed to retrieve refresh token user: %w", err)
	}
	if user.Status == domain.UserStatusDisabled {
		return nil, ErrUserDisabled
	}

	return s.rotateSession(ctx, user, currentToken, now)
}

// SignOut revokes the current refresh token when present.
func (s *Service) SignOut(ctx context.Context, rawRefreshToken string) error {
	if s.refreshTokenStore == nil {
		return nil
	}

	rawRefreshToken = strings.TrimSpace(rawRefreshToken)
	if rawRefreshToken == "" {
		return nil
	}

	err := s.refreshTokenStore.RevokeRefreshTokenByHash(ctx, hashRefreshToken(rawRefreshToken), s.now().UTC().Truncate(time.Microsecond))
	if err != nil && !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		return fmt.Errorf("auth: failed to revoke refresh token: %w", err)
	}
	return nil
}

// CurrentUser validates the raw access token, parses user identifier, and fetches the profile.
func (s *Service) CurrentUser(ctx context.Context, rawAccessToken string) (*PublicUser, error) {
	// Verify access token validity and expiration
	claims, err := s.tokenManager.VerifyAccessToken(rawAccessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// Parse and validate the subject claim as a valid UUID
	parsedUUID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("%w: subject is not a valid UUID", ErrInvalidToken)
	}

	// Retrieve user profile from store
	user, err := s.store.GetUserByID(ctx, parsedUUID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, fmt.Errorf("%w: user not found", ErrUserNotFound)
		}
		return nil, fmt.Errorf("auth: failed to retrieve user profile: %w", err)
	}

	// Enforce disabled user checks
	if user.Status == domain.UserStatusDisabled {
		return nil, ErrUserDisabled
	}

	// Resolve the user's roles and effective permissions fresh on every call
	// so a role or permission change takes effect on the next request.
	roles, err := s.store.GetUserRoles(ctx, parsedUUID)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to retrieve user roles: %w", err)
	}
	permissions, err := s.store.GetUserPermissions(ctx, parsedUUID)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to retrieve user permissions: %w", err)
	}

	return &PublicUser{
		ID:          parsedUUID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      string(user.Status),
		Roles:       roles,
		Permissions: permissions,
	}, nil
}

func (s *Service) issueSession(ctx context.Context, user domain.User, familyID uuid.UUID) (*Session, error) {
	accessToken, err := s.tokenManager.IssueAccessToken(user.ID.String())
	if err != nil {
		return nil, err
	}

	session := &Session{
		User:        publicUserFromDomain(user),
		AccessToken: accessToken,
	}
	if s.refreshTokenStore == nil {
		return session, nil
	}

	rawRefreshToken, input, err := s.newRefreshTokenInput(user.ID, familyID)
	if err != nil {
		return nil, err
	}

	if _, err := s.refreshTokenStore.CreateRefreshToken(ctx, input); err != nil {
		return nil, err
	}

	session.RefreshToken = rawRefreshToken
	return session, nil
}

func (s *Service) rotateSession(ctx context.Context, user domain.User, currentToken domain.RefreshToken, revokedAt time.Time) (*Session, error) {
	accessToken, err := s.tokenManager.IssueAccessToken(user.ID.String())
	if err != nil {
		return nil, err
	}

	rawRefreshToken, input, err := s.newRefreshTokenInput(user.ID, currentToken.FamilyID)
	if err != nil {
		return nil, err
	}

	if _, err := s.refreshTokenStore.RotateRefreshToken(ctx, currentToken.ID, input, revokedAt); err != nil {
		if errors.Is(err, domain.ErrRefreshTokenNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	return &Session{
		User:         publicUserFromDomain(user),
		AccessToken:  accessToken,
		RefreshToken: rawRefreshToken,
	}, nil
}

func publicUserFromDomain(user domain.User) PublicUser {
	return PublicUser{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      string(user.Status),
	}
}

func (s *Service) newRefreshTokenInput(userID uuid.UUID, familyID uuid.UUID) (string, domain.CreateRefreshTokenInput, error) {
	rawRefreshToken, err := newRefreshToken()
	if err != nil {
		return "", domain.CreateRefreshTokenInput{}, err
	}
	if familyID == uuid.Nil {
		familyID = uuid.New()
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	return rawRefreshToken, domain.CreateRefreshTokenInput{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashRefreshToken(rawRefreshToken),
		FamilyID:  familyID,
		ExpiresAt: now.Add(s.refreshTokenTTL),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func newRefreshToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// validateEmail performs a basic structural layout validation on the email address.
func validateEmail(email string) bool {
	if len(email) < 3 || !strings.Contains(email, "@") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

// dummyHash is a real Argon2id hash produced with the password package's
// default parameters. Verifying against it performs work byte-for-byte
// equivalent to a genuine verification (same salt and key lengths, same
// cost), unlike a hand-written placeholder with shorter fields.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	hash, err := password.Hash("timing-mitigation-placeholder")
	if err != nil {
		panic("auth: failed to precompute dummy password hash: " + err.Error())
	}
	return hash
}

// dummyVerify executes an Argon2id comparison against a pre-calculated dummy
// hash to balance timing differences between found and non-existent accounts.
func dummyVerify(passwordStr string) error {
	_, _ = password.Verify(passwordStr, dummyHash)
	return nil
}
