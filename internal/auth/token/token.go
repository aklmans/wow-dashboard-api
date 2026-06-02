// Package token provides JWT access token issuance and verification for the
// Spec-D-D API. It enforces HS256 signing, validates issuer and audience
// claims, and requires token expiration. All tokens are symmetric-key signed
// using a shared secret that must be at least 32 characters long.
package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sentinel errors returned by the token package.
var (
	// ErrSecretTooShort is returned when the signing secret is shorter than
	// the minimum required length.
	ErrSecretTooShort = errors.New("token: secret must be at least 32 characters")

	// ErrEmptyUserID is returned when an empty user ID is passed to
	// IssueAccessToken.
	ErrEmptyUserID = errors.New("token: user ID must not be empty")

	// ErrEmptyIssuer is returned when an empty issuer is passed to NewManager.
	ErrEmptyIssuer = errors.New("token: issuer must not be empty")

	// ErrEmptyAudience is returned when an empty audience is passed to NewManager.
	ErrEmptyAudience = errors.New("token: audience must not be empty")

	// ErrInvalidTTL is returned when a non-positive TTL is passed to NewManager.
	ErrInvalidTTL = errors.New("token: TTL must be positive")
)

// Claims holds the application-specific JWT claims.
type Claims struct {
	// Act, when present, is the user id of the actor (an admin) impersonating
	// the subject. It is informational only: authorization always resolves from
	// the subject's current database roles/permissions, never from this claim.
	// It is used for audit attribution and to surface impersonation in /me.
	Act string `json:"act,omitempty"`

	// MfaPending marks a short-lived token issued after a correct password when
	// the account has MFA: it is NOT a session token and grants no access — it
	// only authorizes the /api/auth/mfa/verify step that completes sign-in.
	MfaPending bool `json:"mfa_pending,omitempty"`

	jwt.RegisteredClaims
}

// Manager handles JWT access token issuance and verification.
type Manager struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
	now      func() time.Time // injectable clock for testing
}

// Option configures a Manager.
type Option func(*Manager)

// WithClock sets a custom clock function for testing token expiration.
func WithClock(fn func() time.Time) Option {
	return func(m *Manager) {
		m.now = fn
	}
}

// NewManager creates a new token Manager.
// It returns error if inputs are invalid.
func NewManager(secret string, issuer string, audience string, ttl time.Duration, opts ...Option) (*Manager, error) {
	if len(secret) < 32 {
		return nil, ErrSecretTooShort
	}
	if issuer == "" {
		return nil, ErrEmptyIssuer
	}
	if audience == "" {
		return nil, ErrEmptyAudience
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}

	m := &Manager{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: audience,
		ttl:      ttl,
		now:      time.Now,
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// IssueAccessToken creates a signed JWT for the given user ID.
// It returns ErrEmptyUserID if userID is empty.
func (m *Manager) IssueAccessToken(userID string) (string, error) {
	if userID == "" {
		return "", ErrEmptyUserID
	}

	now := m.now()

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := t.SignedString(m.secret)
	if err != nil {
		return "", errors.New("token: failed to sign token")
	}

	return signed, nil
}

// IssueImpersonationToken creates a signed JWT whose subject is the impersonated
// user (targetID) and which carries an `act` (actor) claim identifying the admin
// (actorID) performing the impersonation. The token resolves the target's
// permissions (loaded per-request from the subject); the act claim is purely
// informational, used for audit and to surface impersonation in /me.
func (m *Manager) IssueImpersonationToken(targetID, actorID string) (string, error) {
	if targetID == "" || actorID == "" {
		return "", ErrEmptyUserID
	}

	now := m.now()

	claims := Claims{
		Act: actorID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   targetID,
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := t.SignedString(m.secret)
	if err != nil {
		return "", errors.New("token: failed to sign token")
	}

	return signed, nil
}

// IssueMfaPendingToken creates a short-lived JWT (its own ttl, shorter than an
// access token) carrying the mfa_pending claim. It is exchanged at
// /api/auth/mfa/verify for a real session and grants no access on its own.
func (m *Manager) IssueMfaPendingToken(userID string, ttl time.Duration) (string, error) {
	if userID == "" {
		return "", ErrEmptyUserID
	}

	now := m.now()

	claims := Claims{
		MfaPending: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{m.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := t.SignedString(m.secret)
	if err != nil {
		return "", errors.New("token: failed to sign token")
	}

	return signed, nil
}

// VerifyAccessToken parses and validates a JWT string. It enforces HS256
// signing method and validates issuer, audience, and expiration. It rejects
// mfa_pending tickets, which are valid only at /api/auth/mfa/verify, so a
// pre-second-factor ticket can never authorize a normal request. It returns the
// Claims on success.
func (m *Manager) VerifyAccessToken(raw string) (*Claims, error) {
	claims, err := m.parseSignedClaims(raw)
	if err != nil {
		return nil, err
	}
	if claims.MfaPending {
		return nil, errors.New("token: invalid or expired token")
	}
	return claims, nil
}

// VerifyMfaPendingToken validates an mfa_pending ticket issued by
// IssueMfaPendingToken. It enforces the same signature/issuer/audience/expiry
// checks as an access token but additionally REQUIRES the mfa_pending marker,
// so a normal access token can never be replayed at /api/auth/mfa/verify.
func (m *Manager) VerifyMfaPendingToken(raw string) (*Claims, error) {
	claims, err := m.parseSignedClaims(raw)
	if err != nil {
		return nil, err
	}
	if !claims.MfaPending {
		return nil, errors.New("token: invalid or expired token")
	}
	return claims, nil
}

// parseSignedClaims parses a JWT and validates its signature, method, issuer,
// audience, and expiry. It does not interpret the mfa_pending marker — callers
// decide whether a pending ticket is acceptable for their flow.
func (m *Manager) parseSignedClaims(raw string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return nil, errors.New("token: invalid or expired token")
	}

	return claims, nil
}

// ParseClaimsAllowExpired parses a token and returns its claims even when the
// token has expired, but still requires a valid signature, issuer, and
// audience. It exists so the refresh path can detect an impersonation session
// from its (by then expired) access token; never use it to authorize a request.
func (m *Manager) ParseClaimsAllowExpired(raw string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(m.issuer),
		jwt.WithAudience(m.audience),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		// Tolerate ONLY a pure expiry error; a bad signature, wrong issuer or
		// audience, or malformed token is untrustworthy and rejected.
		if !errors.Is(err, jwt.ErrTokenExpired) ||
			errors.Is(err, jwt.ErrTokenSignatureInvalid) ||
			errors.Is(err, jwt.ErrTokenMalformed) ||
			errors.Is(err, jwt.ErrTokenInvalidIssuer) ||
			errors.Is(err, jwt.ErrTokenInvalidAudience) {
			return nil, errors.New("token: invalid token")
		}
	}

	return claims, nil
}
