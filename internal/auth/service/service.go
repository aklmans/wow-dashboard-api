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
	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/email"
	"github.com/google/uuid"
)

// Domain sentinel errors for upstream mapping to API error envelopes.
var (
	ErrInvalidInput        = errors.New("auth: invalid input")
	ErrEmailAlreadyExists  = errors.New("auth: email already exists")
	ErrInvalidCredentials  = errors.New("auth: invalid credentials")
	ErrUserDisabled        = errors.New("auth: user disabled")
	ErrInvalidToken        = errors.New("auth: invalid token")
	ErrUserNotFound        = errors.New("auth: user not found")
	ErrCannotImpersonate   = errors.New("auth: cannot impersonate this user")
	ErrImpersonationActive = errors.New("auth: stop impersonation before refreshing")
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
	// passwordResetTokenTTL bounds how long a password-reset link is valid.
	passwordResetTokenTTL = time.Hour
	// emailVerificationTokenTTL bounds how long an email-verification link is valid.
	emailVerificationTokenTTL = 24 * time.Hour
)

// defaultRoleName is the role assigned to every newly registered user.
const defaultRoleName = "user"

// PublicUser defines the client-facing user presentation. It never exposes
// password hashes or other sensitive internal state. Roles and Permissions
// are populated by CurrentUser; sign-in/sign-up leave them empty (clients
// fetch the resolved set from /api/auth/me).
type PublicUser struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	DisplayName   string     `json:"displayName"`
	Status        string     `json:"status,omitempty"`
	EmailVerified bool       `json:"emailVerified"`
	AvatarURL     string     `json:"avatarUrl"`
	Phone         string     `json:"phone"`
	JobTitle      string     `json:"jobTitle"`
	Company       string     `json:"company"`
	LastLoginAt   *time.Time `json:"lastLoginAt,omitempty"`
	Roles         []string   `json:"roles,omitempty"`
	Permissions   []string   `json:"permissions,omitempty"`
	// ImpersonatorID / ImpersonatorEmail are set only while this session is an
	// impersonation: the admin (actor) acting as this user. They are derived from
	// the token's `act` claim and let the client show an impersonation banner.
	ImpersonatorID    string `json:"impersonatorId,omitempty"`
	ImpersonatorEmail string `json:"impersonatorEmail,omitempty"`
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
	GetUserByIDForAuth(ctx context.Context, id uuid.UUID) (domain.AuthUser, error)
	UpdateUserProfile(ctx context.Context, userID uuid.UUID, input domain.UpdateProfileInput, now time.Time) error
	UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordHash string, updatedAt time.Time) error
	SetEmailVerified(ctx context.Context, userID uuid.UUID, verifiedAt time.Time, updatedAt time.Time) error
}

// AuthTokenStore is the persistence port for the one-time tokens backing the
// password-reset and email-verification flows.
type AuthTokenStore interface {
	CreateAuthToken(ctx context.Context, input domain.CreateAuthTokenInput) error
	GetAuthTokenByHash(ctx context.Context, purpose string, tokenHash string) (domain.AuthToken, error)
	MarkAuthTokenUsed(ctx context.Context, id uuid.UUID, usedAt time.Time) error
	DeleteAuthTokensForUser(ctx context.Context, userID uuid.UUID, purpose string) error
}

// RefreshTokenStore defines the persistence port for opaque refresh tokens.
type RefreshTokenStore interface {
	CreateRefreshToken(ctx context.Context, input domain.CreateRefreshTokenInput) (domain.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (domain.RefreshToken, error)
	RotateRefreshToken(ctx context.Context, oldTokenID uuid.UUID, input domain.CreateRefreshTokenInput, revokedAt time.Time) (domain.RefreshToken, error)
	RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt time.Time) error
	RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID, revokedAt time.Time) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) error
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
	IssueImpersonationToken(targetID, actorID string) (string, error)
	VerifyAccessToken(raw string) (*token.Claims, error)
	ParseClaimsAllowExpired(raw string) (*token.Claims, error)
}

// Service provides the orchestration layer for auth business logic.
type Service struct {
	store             UserStore
	refreshTokenStore RefreshTokenStore
	refreshTokenTTL   time.Duration
	unitOfWork        UnitOfWork
	tokenManager      TokenManager
	authTokenStore    AuthTokenStore
	emailSender       email.Sender
	appBaseURL        string
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

// WithAuthTokenStore enables the password-reset and email-verification flows.
func WithAuthTokenStore(store AuthTokenStore) Option {
	return func(s *Service) {
		if store != nil {
			s.authTokenStore = store
		}
	}
}

// WithEmailSender configures transactional email delivery. The default is a
// log-only sender.
func WithEmailSender(sender email.Sender) Option {
	return func(s *Service) {
		if sender != nil {
			s.emailSender = sender
		}
	}
}

// WithAppBaseURL sets the frontend base URL used to build links in emails.
func WithAppBaseURL(baseURL string) Option {
	return func(s *Service) {
		if baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/"); baseURL != "" {
			s.appBaseURL = baseURL
		}
	}
}

// NewService constructs a new Service instance with injected dependencies.
func NewService(store UserStore, tokenManager TokenManager, opts ...Option) *Service {
	s := &Service{
		store:           store,
		refreshTokenTTL: 14 * 24 * time.Hour,
		tokenManager:    tokenManager,
		emailSender:     email.LogSender{},
		appBaseURL:      "http://localhost:3000",
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
		s.sendEmailVerification(ctx, userID, email)
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
	s.sendEmailVerification(ctx, createdUser.ID, email)
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

	// The credentials are valid: record the successful sign-in. This clears any
	// accumulated failure/lock state and stamps last_login_at, so it runs on
	// every sign-in, not only when failures had accrued. Best-effort — a
	// failure here does not invalidate the sign-in.
	if cerr := s.store.ClearLoginFailures(ctx, user.ID, now); cerr != nil {
		slog.ErrorContext(ctx, "failed to record successful sign-in", "error", cerr)
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

	publicUser := &PublicUser{
		ID:            parsedUUID.String(),
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		Status:        string(user.Status),
		EmailVerified: user.EmailVerified,
		AvatarURL:     user.AvatarURL,
		Phone:         user.Phone,
		JobTitle:      user.JobTitle,
		Company:       user.Company,
		LastLoginAt:   user.LastLoginAt,
		Roles:         roles,
		Permissions:   permissions,
	}

	// Surface impersonation: when the token carries an `act` (actor) claim, this
	// session is an admin acting as the subject. Authorization already resolved
	// from the subject above; here we only attach the actor for display. A
	// missing/deleted actor leaves the email blank but keeps the id, so the
	// client still renders the impersonation banner.
	if actorID := strings.TrimSpace(claims.Act); actorID != "" {
		publicUser.ImpersonatorID = actorID
		if actorUUID, parseErr := uuid.Parse(actorID); parseErr == nil {
			if actor, lookupErr := s.store.GetUserByID(ctx, actorUUID); lookupErr == nil {
				publicUser.ImpersonatorEmail = actor.Email
			}
		}
	}

	return publicUser, nil
}

// Impersonate issues a short-lived access token that lets an admin (actor) act
// as another user (targetID). The token's subject is the target, so the session
// resolves the TARGET's permissions, while an `act` claim records the actor for
// audit and the banner. No refresh token is issued, so the admin's own refresh
// session is preserved and the impersonation simply expires; StopImpersonation
// (a normal refresh) restores the admin.
//
// Guards: the target must exist and be active, the target must NOT be an admin
// (no lateral movement between administrators), and an admin cannot impersonate
// themselves. The actor-is-admin check is enforced by the handler's permission
// gate before this is called.
func (s *Service) Impersonate(ctx context.Context, actor *PublicUser, targetID string) (*Session, error) {
	if actor == nil {
		return nil, ErrInvalidInput
	}
	targetUUID, err := uuid.Parse(strings.TrimSpace(targetID))
	if err != nil {
		return nil, fmt.Errorf("%w: target id is not a valid UUID", ErrInvalidInput)
	}
	if targetUUID.String() == actor.ID {
		return nil, fmt.Errorf("%w: you cannot impersonate yourself", ErrCannotImpersonate)
	}

	target, err := s.store.GetUserByID(ctx, targetUUID)
	if err != nil {
		// A missing target is a bad request against this endpoint, not an auth
		// failure of the (authorized) admin — surface it as cannot-impersonate.
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, fmt.Errorf("%w: the user does not exist", ErrCannotImpersonate)
		}
		return nil, fmt.Errorf("auth: impersonate lookup: %w", err)
	}
	if target.Status == domain.UserStatusDisabled {
		return nil, fmt.Errorf("%w: the user is disabled", ErrCannotImpersonate)
	}

	// Resolve the target's effective roles/permissions: used both to block
	// impersonating another admin and to populate the returned session.
	targetRoles, err := s.store.GetUserRoles(ctx, targetUUID)
	if err != nil {
		return nil, fmt.Errorf("auth: impersonate roles: %w", err)
	}
	targetPerms, err := s.store.GetUserPermissions(ctx, targetUUID)
	if err != nil {
		return nil, fmt.Errorf("auth: impersonate permissions: %w", err)
	}
	if isAdmin(targetRoles, targetPerms) {
		return nil, fmt.Errorf("%w: administrators cannot be impersonated", ErrCannotImpersonate)
	}

	accessToken, err := s.tokenManager.IssueImpersonationToken(targetUUID.String(), actor.ID)
	if err != nil {
		return nil, fmt.Errorf("auth: issue impersonation token: %w", err)
	}

	s.recordAudit(ctx, AuditEvent{
		EventType: EventAuthImpersonationStarted,
		Message:   "Impersonation started.",
		Metadata: AuditMetadata{
			ActorUserID:  actor.ID,
			TargetUserID: targetUUID.String(),
		},
	})

	return &Session{
		User: PublicUser{
			ID:                targetUUID.String(),
			Email:             target.Email,
			DisplayName:       target.DisplayName,
			Status:            string(target.Status),
			EmailVerified:     target.EmailVerified,
			AvatarURL:         target.AvatarURL,
			Phone:             target.Phone,
			JobTitle:          target.JobTitle,
			Company:           target.Company,
			LastLoginAt:       target.LastLoginAt,
			Roles:             targetRoles,
			Permissions:       targetPerms,
			ImpersonatorID:    actor.ID,
			ImpersonatorEmail: actor.Email,
		},
		AccessToken:  accessToken,
		RefreshToken: "", // never rotate the admin's refresh session
	}, nil
}

// RefreshSession backs the generic POST /api/auth/refresh. It refuses to refresh
// while impersonating: the current (impersonation) access token carries an `act`
// claim, and minting a fresh session from the preserved admin refresh token here
// would silently restore admin privileges without an explicit, audited stop.
// Callers leave an impersonation session through StopImpersonation instead.
func (s *Service) RefreshSession(ctx context.Context, rawCurrentAccessToken, rawRefreshToken string) (*Session, error) {
	if strings.TrimSpace(rawCurrentAccessToken) != "" {
		if claims, err := s.tokenManager.ParseClaimsAllowExpired(rawCurrentAccessToken); err == nil && strings.TrimSpace(claims.Act) != "" {
			return nil, ErrImpersonationActive
		}
	}
	return s.Refresh(ctx, rawRefreshToken)
}

// StopImpersonation ends an impersonation session and restores the admin. It
// refreshes using the admin's preserved refresh token first, then — only after a
// successful restore — audits the stop, so the start/stop bracketing is never
// broken by a failed restore. An expired impersonation token is still read for
// the audit so a late stop is recorded.
func (s *Service) StopImpersonation(ctx context.Context, rawCurrentToken, rawRefreshToken string) (*Session, error) {
	session, err := s.Refresh(ctx, rawRefreshToken)
	if err != nil {
		return nil, err
	}

	if claims, perr := s.tokenManager.ParseClaimsAllowExpired(rawCurrentToken); perr == nil && strings.TrimSpace(claims.Act) != "" {
		s.recordAudit(ctx, AuditEvent{
			EventType: EventAuthImpersonationStopped,
			Message:   "Impersonation stopped.",
			Metadata: AuditMetadata{
				ActorUserID:  claims.Act,
				TargetUserID: claims.Subject,
			},
		})
	}

	return session, nil
}

// isAdmin reports whether the given roles/permissions denote an administrator —
// the wildcard permission or the built-in "admin" role. Mirrors the users
// service's admin check.
func isAdmin(roles, permissions []string) bool {
	if rbac.NewSet(permissions).Has(rbac.PermissionAll) {
		return true
	}
	for _, role := range roles {
		if strings.TrimSpace(role) == "admin" {
			return true
		}
	}
	return false
}

// ChangePassword verifies a signed-in user's current password and replaces it
// with a new one. Every refresh token for the user is then revoked so other
// sessions must re-authenticate; the caller's current session keeps its
// (short-lived) access token until it expires.
func (s *Service) ChangePassword(ctx context.Context, rawAccessToken string, currentPassword string, newPassword string) error {
	claims, err := s.tokenManager.VerifyAccessToken(rawAccessToken)
	if err != nil {
		return ErrInvalidToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return fmt.Errorf("%w: subject is not a valid UUID", ErrInvalidToken)
	}

	// Reject over-long inputs before any hashing work, and enforce the same
	// length policy as sign-up on the new password.
	if len(currentPassword) > maxPasswordLength || len(newPassword) > maxPasswordLength {
		return fmt.Errorf("%w: password must not exceed %d characters", ErrInvalidInput, maxPasswordLength)
	}
	if len(newPassword) < minPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLength)
	}

	user, err := s.store.GetUserByIDForAuth(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return ErrInvalidToken
		}
		return fmt.Errorf("auth: failed to retrieve user: %w", err)
	}
	if user.Status == domain.UserStatusDisabled {
		return ErrUserDisabled
	}

	match, err := password.Verify(currentPassword, user.PasswordHash)
	if err != nil {
		return fmt.Errorf("auth: failed to verify current password: %w", err)
	}
	if !match {
		return ErrInvalidCredentials
	}

	newHash, err := password.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: failed to hash password: %w", err)
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	if err := s.store.UpdateUserPassword(ctx, userID, newHash, now); err != nil {
		return fmt.Errorf("auth: failed to update password: %w", err)
	}

	// Revoke every refresh token so a compromised session cannot survive a
	// password change. This is best-effort — the password is already changed.
	if s.refreshTokenStore != nil {
		if err := s.refreshTokenStore.RevokeAllForUser(ctx, userID, now); err != nil {
			slog.ErrorContext(ctx, "failed to revoke refresh tokens after password change", "error", err)
		}
	}

	s.recordPasswordChanged(ctx, AuditMetadata{UserID: userID.String()})
	return nil
}

// ForgotPassword issues a password-reset token and emails it. To avoid account
// enumeration the result is always nil whether or not the email matches an
// active account.
func (s *Service) ForgotPassword(ctx context.Context, rawEmail string) error {
	if s.authTokenStore == nil {
		return fmt.Errorf("auth: password reset is not configured")
	}
	emailAddr := strings.ToLower(strings.TrimSpace(rawEmail))

	user, err := s.store.GetUserByEmailForAuth(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil // No enumeration: report success regardless.
		}
		return fmt.Errorf("auth: failed to look up user: %w", err)
	}
	if user.Status == domain.UserStatusDisabled {
		return nil // Do not issue resets for disabled accounts.
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	rawToken, err := newRefreshToken()
	if err != nil {
		return fmt.Errorf("auth: failed to generate reset token: %w", err)
	}
	_ = s.authTokenStore.DeleteAuthTokensForUser(ctx, user.ID, domain.AuthTokenPurposePasswordReset)
	if err := s.authTokenStore.CreateAuthToken(ctx, domain.CreateAuthTokenInput{
		ID:        uuid.New(),
		UserID:    user.ID,
		Purpose:   domain.AuthTokenPurposePasswordReset,
		TokenHash: hashRefreshToken(rawToken),
		ExpiresAt: now.Add(passwordResetTokenTTL),
		CreatedAt: now,
	}); err != nil {
		return fmt.Errorf("auth: failed to store reset token: %w", err)
	}

	s.recordPasswordResetRequested(ctx, AuditMetadata{Email: emailAddr, UserID: user.ID.String()})

	if err := s.emailSender.Send(ctx, email.Message{
		To:      user.Email,
		Subject: "Reset your password",
		Body: fmt.Sprintf("Reset your password by opening:\n%s/reset-password?token=%s\n\n"+
			"The link expires in 1 hour. If you did not request this, ignore this email.", s.appBaseURL, rawToken),
	}); err != nil {
		slog.ErrorContext(ctx, "failed to send password reset email", "error", err)
	}
	return nil
}

// ResetPassword consumes a password-reset token and sets a new password,
// revoking every existing session for the user.
func (s *Service) ResetPassword(ctx context.Context, rawToken string, newPassword string) error {
	if s.authTokenStore == nil {
		return fmt.Errorf("auth: password reset is not configured")
	}
	if len(newPassword) > maxPasswordLength {
		return fmt.Errorf("%w: password must not exceed %d characters", ErrInvalidInput, maxPasswordLength)
	}
	if len(newPassword) < minPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidInput, minPasswordLength)
	}

	authToken, err := s.consumeAuthToken(ctx, domain.AuthTokenPurposePasswordReset, rawToken)
	if err != nil {
		s.recordPasswordResetFailed(ctx, AuditMetadata{Reason: AuditReasonInvalidToken})
		return err
	}

	newHash, err := password.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: failed to hash password: %w", err)
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if err := s.store.UpdateUserPassword(ctx, authToken.UserID, newHash, now); err != nil {
		return fmt.Errorf("auth: failed to update password: %w", err)
	}
	if err := s.authTokenStore.MarkAuthTokenUsed(ctx, authToken.ID, now); err != nil {
		slog.ErrorContext(ctx, "failed to mark reset token used", "error", err)
	}
	if s.refreshTokenStore != nil {
		if err := s.refreshTokenStore.RevokeAllForUser(ctx, authToken.UserID, now); err != nil {
			slog.ErrorContext(ctx, "failed to revoke refresh tokens after password reset", "error", err)
		}
	}
	s.recordPasswordReset(ctx, AuditMetadata{UserID: authToken.UserID.String()})
	return nil
}

// VerifyEmail consumes an email-verification token and marks the user's email
// address as verified.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	if s.authTokenStore == nil {
		return fmt.Errorf("auth: email verification is not configured")
	}
	authToken, err := s.consumeAuthToken(ctx, domain.AuthTokenPurposeEmailVerification, rawToken)
	if err != nil {
		s.recordEmailVerificationFailed(ctx, AuditMetadata{Reason: AuditReasonInvalidToken})
		return err
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if err := s.store.SetEmailVerified(ctx, authToken.UserID, now, now); err != nil {
		return fmt.Errorf("auth: failed to mark email verified: %w", err)
	}
	if err := s.authTokenStore.MarkAuthTokenUsed(ctx, authToken.ID, now); err != nil {
		slog.ErrorContext(ctx, "failed to mark verification token used", "error", err)
	}
	s.recordEmailVerified(ctx, AuditMetadata{UserID: authToken.UserID.String()})
	return nil
}

// ResendVerification issues a fresh email-verification token for the
// signed-in user. It is a no-op if the email is already verified.
func (s *Service) ResendVerification(ctx context.Context, rawAccessToken string) error {
	if s.authTokenStore == nil {
		return fmt.Errorf("auth: email verification is not configured")
	}
	claims, err := s.tokenManager.VerifyAccessToken(rawAccessToken)
	if err != nil {
		return ErrInvalidToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return fmt.Errorf("%w: subject is not a valid UUID", ErrInvalidToken)
	}
	user, err := s.store.GetUserByIDForAuth(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return ErrInvalidToken
		}
		return fmt.Errorf("auth: failed to retrieve user: %w", err)
	}
	if user.EmailVerified {
		return nil // Already verified — nothing to do.
	}
	s.sendEmailVerification(ctx, userID, user.Email)
	return nil
}

// consumeAuthToken looks up a token by purpose and confirms it is unused and
// unexpired. A missing, used, or expired token surfaces as ErrInvalidToken.
func (s *Service) consumeAuthToken(ctx context.Context, purpose string, rawToken string) (domain.AuthToken, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return domain.AuthToken{}, ErrInvalidToken
	}
	authToken, err := s.authTokenStore.GetAuthTokenByHash(ctx, purpose, hashRefreshToken(rawToken))
	if err != nil {
		if errors.Is(err, domain.ErrAuthTokenNotFound) {
			return domain.AuthToken{}, ErrInvalidToken
		}
		return domain.AuthToken{}, fmt.Errorf("auth: failed to retrieve token: %w", err)
	}
	if authToken.UsedAt != nil {
		return domain.AuthToken{}, ErrInvalidToken
	}
	if !authToken.ExpiresAt.After(s.now().UTC()) {
		return domain.AuthToken{}, ErrInvalidToken
	}
	return authToken, nil
}

// sendEmailVerification issues an email-verification token for the user and
// delivers it. It is best-effort — failures are logged, never fatal.
func (s *Service) sendEmailVerification(ctx context.Context, userID uuid.UUID, recipientEmail string) {
	if s.authTokenStore == nil {
		return
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	rawToken, err := newRefreshToken()
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate email verification token", "error", err)
		return
	}
	_ = s.authTokenStore.DeleteAuthTokensForUser(ctx, userID, domain.AuthTokenPurposeEmailVerification)
	if err := s.authTokenStore.CreateAuthToken(ctx, domain.CreateAuthTokenInput{
		ID:        uuid.New(),
		UserID:    userID,
		Purpose:   domain.AuthTokenPurposeEmailVerification,
		TokenHash: hashRefreshToken(rawToken),
		ExpiresAt: now.Add(emailVerificationTokenTTL),
		CreatedAt: now,
	}); err != nil {
		slog.ErrorContext(ctx, "failed to store email verification token", "error", err)
		return
	}
	if err := s.emailSender.Send(ctx, email.Message{
		To:      recipientEmail,
		Subject: "Verify your email address",
		Body: fmt.Sprintf("Confirm your email address by opening:\n%s/verify-email?token=%s\n\n"+
			"The link expires in 24 hours.", s.appBaseURL, rawToken),
	}); err != nil {
		slog.ErrorContext(ctx, "failed to send email verification", "error", err)
	}
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

// UpdateMyProfileInput captures the optional fields a user can edit on their
// own profile. Each nil pointer leaves the corresponding field unchanged.
type UpdateMyProfileInput struct {
	DisplayName *string
	AvatarURL   *string
	Phone       *string
	JobTitle    *string
	Company     *string
}

// UpdateMyProfile applies a self-service profile update for the bearer-token
// user. Status and role assignments are NOT editable through this path.
func (s *Service) UpdateMyProfile(ctx context.Context, rawAccessToken string, input UpdateMyProfileInput) (*PublicUser, error) {
	claims, err := s.tokenManager.VerifyAccessToken(rawAccessToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("%w: subject is not a valid UUID", ErrInvalidToken)
	}

	normalized, err := normalizeProfileInput(input)
	if err != nil {
		return nil, err
	}

	if err := s.store.UpdateUserProfile(ctx, userID, normalized, s.now()); err != nil {
		return nil, fmt.Errorf("auth: update profile: %w", err)
	}
	return s.CurrentUser(ctx, rawAccessToken)
}

// normalizeProfileInput trims each provided profile field, enforces a length
// cap, and rejects an empty displayName so a user cannot strip themselves of
// a usable name. The avatar URL cap is larger so an inline data URL from a
// modest upload (a resized 256×256 JPEG) fits.
func normalizeProfileInput(input UpdateMyProfileInput) (domain.UpdateProfileInput, error) {
	const (
		maxProfileFieldLen = 256
		maxAvatarFieldLen  = 256 * 1024 // 256KB — fits a resized inline image
	)
	out := domain.UpdateProfileInput{}

	fields := []struct {
		name  string
		value *string
		apply func(*string)
	}{
		{"displayName", input.DisplayName, func(v *string) { out.DisplayName = v }},
		{"avatarUrl", input.AvatarURL, func(v *string) { out.AvatarURL = v }},
		{"phone", input.Phone, func(v *string) { out.Phone = v }},
		{"jobTitle", input.JobTitle, func(v *string) { out.JobTitle = v }},
		{"company", input.Company, func(v *string) { out.Company = v }},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*f.value)
		maxLen := maxProfileFieldLen
		if f.name == "avatarUrl" {
			maxLen = maxAvatarFieldLen
		}
		if len(trimmed) > maxLen {
			return domain.UpdateProfileInput{}, fmt.Errorf("%w: %s must be %d characters or fewer", ErrInvalidInput, f.name, maxLen)
		}
		if f.name == "displayName" && trimmed == "" {
			return domain.UpdateProfileInput{}, fmt.Errorf("%w: displayName cannot be empty", ErrInvalidInput)
		}
		f.apply(&trimmed)
	}
	return out, nil
}
