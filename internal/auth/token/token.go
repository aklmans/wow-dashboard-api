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

// VerifyAccessToken parses and validates a JWT string. It enforces HS256
// signing method and validates issuer, audience, and expiration. It returns
// the Claims on success.
func (m *Manager) VerifyAccessToken(raw string) (*Claims, error) {
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
