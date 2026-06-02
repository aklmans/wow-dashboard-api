package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/crypto"
	"github.com/aklmans/wow-dashboard-api/internal/mfa/service"
)

const testKey = "test-mfa-encryption-key-at-least-32-chars"

var fixedNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T, store service.Store) *service.Service {
	t.Helper()
	cipher, err := crypto.NewCipher(testKey)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return service.NewService(store, cipher, "WOW Dashboard",
		service.WithClock(func() time.Time { return fixedNow }))
}

func TestSetupStoresAnEncryptedSecret(t *testing.T) {
	store := &fakeStore{}
	svc := newService(t, store)

	result, err := svc.Setup(context.Background(), uuid.New(), "demo@example.com")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !strings.HasPrefix(result.OtpauthURL, "otpauth://totp/") {
		t.Fatalf("OtpauthURL = %q, want an otpauth URI", result.OtpauthURL)
	}
	if !strings.Contains(result.OtpauthURL, "WOW%20Dashboard") && !strings.Contains(result.OtpauthURL, "WOW+Dashboard") {
		t.Fatalf("OtpauthURL = %q, want the issuer label", result.OtpauthURL)
	}
	if !store.hasSecret {
		t.Fatal("Setup did not store a secret")
	}
	// What's persisted is ciphertext, not the raw base32 secret.
	if store.secret.SecretEncrypted == result.Secret {
		t.Fatal("secret was stored in plaintext")
	}
	if store.enabled {
		t.Fatal("Setup enabled MFA before confirmation")
	}
}

func TestConfirmWithValidCodeEnablesMfaAndReturnsRecoveryCodes(t *testing.T) {
	store := &fakeStore{}
	svc := newService(t, store)

	setup, err := svc.Setup(context.Background(), uuid.New(), "demo@example.com")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	code, err := totp.GenerateCode(setup.Secret, fixedNow)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	codes, err := svc.Confirm(context.Background(), uuid.New(), code)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %d, want 10", len(codes))
	}
	if !store.enabled || store.confirmedAt == nil {
		t.Fatalf("MFA not enabled after confirm: enabled=%v confirmedAt=%v", store.enabled, store.confirmedAt)
	}
	if len(store.recoveryCodeHashes) != 10 {
		t.Fatalf("stored recovery hashes = %d, want 10", len(store.recoveryCodeHashes))
	}
	// Raw codes are never persisted — only their hashes.
	for _, raw := range codes {
		if contains(store.recoveryCodeHashes, raw) {
			t.Fatalf("a raw recovery code was stored: %q", raw)
		}
		if !contains(store.recoveryCodeHashes, service.HashRecoveryCode(raw)) {
			t.Fatalf("no stored hash matches recovery code %q", raw)
		}
	}
}

func TestConfirmRejectsAnInvalidCode(t *testing.T) {
	store := &fakeStore{}
	svc := newService(t, store)
	if _, err := svc.Setup(context.Background(), uuid.New(), "demo@example.com"); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if _, err := svc.Confirm(context.Background(), uuid.New(), "000000"); !errors.Is(err, service.ErrInvalidCode) {
		t.Fatalf("Confirm error = %v, want ErrInvalidCode", err)
	}
	if store.enabled {
		t.Fatal("MFA was enabled despite an invalid code")
	}
}

func TestConfirmWithoutSetupIsNotEnrolling(t *testing.T) {
	store := &fakeStore{}
	svc := newService(t, store)
	if _, err := svc.Confirm(context.Background(), uuid.New(), "123456"); !errors.Is(err, service.ErrNotEnrolling) {
		t.Fatalf("Confirm error = %v, want ErrNotEnrolling", err)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// --- fake store ---

type fakeStore struct {
	secret             domain.MfaSecret
	hasSecret          bool
	enabled            bool
	confirmedAt        *time.Time
	recoveryCodeHashes []string
}

func (f *fakeStore) GetMfaSecret(_ context.Context, _ uuid.UUID) (domain.MfaSecret, error) {
	if !f.hasSecret {
		return domain.MfaSecret{}, domain.ErrMfaSecretNotFound
	}
	return f.secret, nil
}

func (f *fakeStore) StoreSetupSecret(_ context.Context, input domain.UpsertMfaSecretInput) error {
	if f.enabled {
		return domain.ErrMfaAlreadyEnabled
	}
	f.secret = domain.MfaSecret{
		ID:              input.ID,
		UserID:          input.UserID,
		SecretEncrypted: input.SecretEncrypted,
		Algorithm:       input.Algorithm,
		Digits:          input.Digits,
		Period:          input.Period,
	}
	f.hasSecret = true
	return nil
}

func (f *fakeStore) CompleteEnrollment(_ context.Context, _ uuid.UUID, codeHashes []string, confirmedAt time.Time, _ time.Time) error {
	if f.enabled {
		return domain.ErrMfaAlreadyEnabled
	}
	f.enabled = true
	f.confirmedAt = &confirmedAt
	f.recoveryCodeHashes = codeHashes
	return nil
}
