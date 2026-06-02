// Package service implements TOTP-based MFA enrollment: generating a secret,
// confirming the first code, and issuing one-time recovery codes. The TOTP
// secret is encrypted at rest (the injected Cipher); recovery codes are stored
// as SHA-256 hashes and the raw values are returned to the caller exactly once.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
)

const (
	recoveryCodeCount = 10
	totpAlgorithm     = "SHA1"
	totpDigits        = 6
	totpPeriod        = 30
	// totpSkew tolerates one 30s step of clock drift on either side.
	totpSkew = 1
)

var (
	// ErrInvalidCode is returned when a TOTP code fails verification.
	ErrInvalidCode = errors.New("mfa: invalid code")
	// ErrNotEnrolling is returned when confirm runs with no pending secret.
	ErrNotEnrolling = errors.New("mfa: no enrollment in progress")
)

// Cipher encrypts/decrypts the TOTP secret at rest.
type Cipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

// Store is the persistence the MFA service needs. StoreSetupSecret and
// CompleteEnrollment serialize concurrent setup/confirm per user (a row lock)
// and refuse once MFA is enabled, so enrollment is race-safe.
type Store interface {
	GetMfaSecret(ctx context.Context, userID uuid.UUID) (domain.MfaSecret, error)
	StoreSetupSecret(ctx context.Context, input domain.UpsertMfaSecretInput) error
	CompleteEnrollment(ctx context.Context, userID uuid.UUID, codeHashes []string, confirmedAt time.Time, now time.Time) error
}

// Service orchestrates MFA enrollment.
type Service struct {
	store  Store
	cipher Cipher
	audit  authservice.AuditRecorder
	issuer string
	now    func() time.Time
}

// SetupResult is returned from Setup so the client can render the QR / manual key.
type SetupResult struct {
	OtpauthURL string
	Secret     string
}

// Option configures a Service.
type Option func(*Service)

// WithAuditRecorder enables best-effort MFA audit events.
func WithAuditRecorder(r authservice.AuditRecorder) Option {
	return func(s *Service) {
		if r != nil {
			s.audit = r
		}
	}
}

// WithClock overrides the clock for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// NewService constructs a Service. issuer is the TOTP issuer label shown in the
// authenticator app (e.g. "WOW Dashboard").
func NewService(store Store, cipher Cipher, issuer string, opts ...Option) *Service {
	s := &Service{
		store:  store,
		cipher: cipher,
		issuer: issuer,
		now:    time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Setup generates a fresh (unconfirmed) TOTP secret for the user, stores it
// encrypted, and returns the otpauth URI + raw secret for enrollment. Re-running
// setup before confirming replaces the pending secret.
func (s *Service) Setup(ctx context.Context, userID uuid.UUID, accountName string) (SetupResult, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.issuer,
		AccountName: accountName,
		Period:      totpPeriod,
		Digits:      totpDigits,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return SetupResult{}, fmt.Errorf("mfa: generate secret: %w", err)
	}

	encrypted, err := s.cipher.Encrypt(key.Secret())
	if err != nil {
		return SetupResult{}, fmt.Errorf("mfa: encrypt secret: %w", err)
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	if err := s.store.StoreSetupSecret(ctx, domain.UpsertMfaSecretInput{
		ID:              uuid.New(),
		UserID:          userID,
		SecretEncrypted: encrypted,
		Algorithm:       totpAlgorithm,
		Digits:          totpDigits,
		Period:          totpPeriod,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		return SetupResult{}, err
	}

	return SetupResult{OtpauthURL: key.URL(), Secret: key.Secret()}, nil
}

// Confirm verifies the first code against the pending secret, turns MFA on, and
// returns a fresh set of one-time recovery codes (shown to the user once). The
// raw codes are never persisted — only their hashes.
func (s *Service) Confirm(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	secret, err := s.store.GetMfaSecret(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrMfaSecretNotFound) {
			return nil, ErrNotEnrolling
		}
		return nil, err
	}

	plainSecret, err := s.cipher.Decrypt(secret.SecretEncrypted)
	if err != nil {
		return nil, fmt.Errorf("mfa: decrypt secret: %w", err)
	}

	valid, err := totp.ValidateCustom(strings.TrimSpace(code), plainSecret, s.now().UTC(), totp.ValidateOpts{
		Period:    uint(secret.Period),
		Skew:      totpSkew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !valid {
		return nil, ErrInvalidCode
	}

	// Generate the recovery codes up front, then persist their hashes and enable
	// MFA atomically — CompleteEnrollment locks the user row, so concurrent
	// confirms can't both write a set of codes or double-enable.
	rawCodes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		raw, err := newRecoveryCode()
		if err != nil {
			return nil, fmt.Errorf("mfa: generate recovery code: %w", err)
		}
		rawCodes = append(rawCodes, raw)
		hashes = append(hashes, HashRecoveryCode(raw))
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	if err := s.store.CompleteEnrollment(ctx, userID, hashes, now, now); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, authservice.EventAuthMfaEnabled, userID)
	return rawCodes, nil
}

func (s *Service) recordAudit(ctx context.Context, eventType string, userID uuid.UUID) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordAuthEvent(ctx, authservice.AuditEvent{
		EventType: eventType,
		Message:   "MFA state changed.",
		Metadata:  authservice.AuditMetadata{UserID: userID.String()},
	})
}

// newRecoveryCode returns a 13-char lowercase base32 recovery code (~64 bits).
func newRecoveryCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

// HashRecoveryCode hashes a recovery code for storage/lookup (SHA-256 hex), after
// trimming and lower-casing so user input is matched leniently.
func HashRecoveryCode(code string) string {
	normalized := strings.ToLower(strings.TrimSpace(code))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}
