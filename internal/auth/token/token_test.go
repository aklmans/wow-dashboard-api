package token_test

import (
	"errors"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret   = "test-secret-that-is-at-least-32-characters-long"
	altSecret    = "different-secret-at-least-32-characters-long!!"
	testIssuer   = "wow-dashboard-api"
	testAudience = "spec-d-d-web"
	testUserID   = "user-abc-123"
	testTTL      = 15 * time.Minute
)

// newTestManager creates a Manager with standard test defaults and any
// additional options. It fails the test immediately if construction errors.
func newTestManager(t *testing.T, opts ...token.Option) *token.Manager {
	t.Helper()

	m, err := token.NewManager(testSecret, testIssuer, testAudience, testTTL, opts...)
	if err != nil {
		t.Fatalf("newTestManager: unexpected error: %v", err)
	}

	return m
}

func TestIssueAndVerify(t *testing.T) {
	m := newTestManager(t)

	raw, err := m.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken: unexpected error: %v", err)
	}

	if raw == "" {
		t.Fatal("IssueAccessToken: returned empty token string")
	}

	claims, err := m.VerifyAccessToken(raw)
	if err != nil {
		t.Fatalf("VerifyAccessToken: unexpected error: %v", err)
	}

	if claims.Subject != testUserID {
		t.Errorf("Subject = %q, want %q", claims.Subject, testUserID)
	}

	if claims.Issuer != testIssuer {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, testIssuer)
	}

	if len(claims.Audience) == 0 || claims.Audience[0] != testAudience {
		t.Errorf("Audience = %v, want [%q]", claims.Audience, testAudience)
	}
}

func TestImpersonationToken(t *testing.T) {
	m := newTestManager(t)

	raw, err := m.IssueImpersonationToken("target-user-id", "admin-actor-id")
	if err != nil {
		t.Fatalf("IssueImpersonationToken: unexpected error: %v", err)
	}

	claims, err := m.VerifyAccessToken(raw)
	if err != nil {
		t.Fatalf("VerifyAccessToken: unexpected error: %v", err)
	}
	if claims.Subject != "target-user-id" {
		t.Errorf("Subject = %q, want the target", claims.Subject)
	}
	if claims.Act != "admin-actor-id" {
		t.Errorf("Act = %q, want the actor", claims.Act)
	}

	// A normal access token carries no act claim.
	normal, _ := m.IssueAccessToken(testUserID)
	normalClaims, _ := m.VerifyAccessToken(normal)
	if normalClaims.Act != "" {
		t.Errorf("normal token Act = %q, want empty", normalClaims.Act)
	}

	// Both ids are required.
	if _, err := m.IssueImpersonationToken("", "actor"); !errors.Is(err, token.ErrEmptyUserID) {
		t.Errorf("empty target err = %v, want ErrEmptyUserID", err)
	}
	if _, err := m.IssueImpersonationToken("target", ""); !errors.Is(err, token.ErrEmptyUserID) {
		t.Errorf("empty actor err = %v, want ErrEmptyUserID", err)
	}
}

func TestParseClaimsAllowExpired(t *testing.T) {
	m := newTestManager(t)

	// An expired token still yields its claims (used to detect impersonation on
	// refresh) while VerifyAccessToken rejects it.
	past := newTestManager(t, token.WithClock(func() time.Time { return time.Now().Add(-24 * time.Hour) }))
	expired, err := past.IssueImpersonationToken("target-id", "actor-id")
	if err != nil {
		t.Fatalf("IssueImpersonationToken: %v", err)
	}

	claims, err := m.ParseClaimsAllowExpired(expired)
	if err != nil {
		t.Fatalf("ParseClaimsAllowExpired should tolerate expiry: %v", err)
	}
	if claims.Act != "actor-id" || claims.Subject != "target-id" {
		t.Fatalf("claims = %#v, want act/sub preserved", claims)
	}
	if _, err := m.VerifyAccessToken(expired); err == nil {
		t.Fatal("VerifyAccessToken must still reject an expired token")
	}

	// A token signed with a different secret is rejected even by the
	// expiry-tolerant parser — the signature must always be valid.
	alt, err := token.NewManager(altSecret, testIssuer, testAudience, testTTL)
	if err != nil {
		t.Fatalf("NewManager(alt): %v", err)
	}
	forged, _ := alt.IssueAccessToken("x")
	if _, err := m.ParseClaimsAllowExpired(forged); err == nil {
		t.Fatal("ParseClaimsAllowExpired must reject a token signed with a different secret")
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	// Issue with a clock set far in the past so the token is already expired.
	pastClock := func() time.Time {
		return time.Now().Add(-24 * time.Hour)
	}
	issuer := newTestManager(t, token.WithClock(pastClock))

	raw, err := issuer.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken: unexpected error: %v", err)
	}

	// Verify with a manager that uses the real clock.
	verifier := newTestManager(t)

	_, err = verifier.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for expired token, got nil")
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	issuer := newTestManager(t)

	raw, err := issuer.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken: unexpected error: %v", err)
	}

	verifier, err := token.NewManager(altSecret, testIssuer, testAudience, testTTL)
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}

	_, err = verifier.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for wrong secret, got nil")
	}
}

func TestVerify_AlgNone(t *testing.T) {
	// Craft a token signed with alg=none.
	claims := jwt.RegisteredClaims{
		Subject:   testUserID,
		Issuer:    testIssuer,
		Audience:  jwt.ClaimStrings{testAudience},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(testTTL)),
	}

	noneTok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)

	raw, err := noneTok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString (none): unexpected error: %v", err)
	}

	m := newTestManager(t)

	_, err = m.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for alg=none token, got nil")
	}
}

func TestVerify_AlgHS384(t *testing.T) {
	// Craft a token signed with HS384 instead of HS256.
	claims := jwt.RegisteredClaims{
		Subject:   testUserID,
		Issuer:    testIssuer,
		Audience:  jwt.ClaimStrings{testAudience},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		NotBefore: jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(testTTL)),
	}

	hs384Tok := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)

	raw, err := hs384Tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("SignedString (HS384): unexpected error: %v", err)
	}

	m := newTestManager(t)

	_, err = m.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for HS384 token, got nil")
	}
}

func TestVerify_WrongIssuer(t *testing.T) {
	// Issue with issuer A.
	issuerA, err := token.NewManager(testSecret, "issuer-a", testAudience, testTTL)
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}

	raw, err := issuerA.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken: unexpected error: %v", err)
	}

	// Verify with issuer B.
	issuerB, err := token.NewManager(testSecret, "issuer-b", testAudience, testTTL)
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}

	_, err = issuerB.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for wrong issuer, got nil")
	}
}

func TestVerify_WrongAudience(t *testing.T) {
	// Issue with audience A.
	audA, err := token.NewManager(testSecret, testIssuer, "audience-a", testTTL)
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}

	raw, err := audA.IssueAccessToken(testUserID)
	if err != nil {
		t.Fatalf("IssueAccessToken: unexpected error: %v", err)
	}

	// Verify with audience B.
	audB, err := token.NewManager(testSecret, testIssuer, "audience-b", testTTL)
	if err != nil {
		t.Fatalf("NewManager: unexpected error: %v", err)
	}

	_, err = audB.VerifyAccessToken(raw)
	if err == nil {
		t.Fatal("VerifyAccessToken: expected error for wrong audience, got nil")
	}
}

func TestIssue_EmptyUserID(t *testing.T) {
	m := newTestManager(t)

	_, err := m.IssueAccessToken("")
	if err == nil {
		t.Fatal("IssueAccessToken: expected error for empty userID, got nil")
	}

	if !errors.Is(err, token.ErrEmptyUserID) {
		t.Errorf("error = %v, want %v", err, token.ErrEmptyUserID)
	}
}

func TestNewManager_SecretTooShort(t *testing.T) {
	_, err := token.NewManager("short", testIssuer, testAudience, testTTL)
	if err == nil {
		t.Fatal("NewManager: expected error for short secret, got nil")
	}

	if !errors.Is(err, token.ErrSecretTooShort) {
		t.Errorf("error = %v, want %v", err, token.ErrSecretTooShort)
	}
}

func TestNewManager_EmptyIssuer(t *testing.T) {
	_, err := token.NewManager(testSecret, "", testAudience, testTTL)
	if err == nil {
		t.Fatal("NewManager: expected error for empty issuer, got nil")
	}

	if !errors.Is(err, token.ErrEmptyIssuer) {
		t.Errorf("error = %v, want %v", err, token.ErrEmptyIssuer)
	}
}

func TestNewManager_EmptyAudience(t *testing.T) {
	_, err := token.NewManager(testSecret, testIssuer, "", testTTL)
	if err == nil {
		t.Fatal("NewManager: expected error for empty audience, got nil")
	}

	if !errors.Is(err, token.ErrEmptyAudience) {
		t.Errorf("error = %v, want %v", err, token.ErrEmptyAudience)
	}
}

func TestNewManager_InvalidTTL(t *testing.T) {
	_, err := token.NewManager(testSecret, testIssuer, testAudience, 0)
	if err == nil {
		t.Fatal("NewManager: expected error for zero TTL, got nil")
	}

	if !errors.Is(err, token.ErrInvalidTTL) {
		t.Errorf("error = %v, want %v", err, token.ErrInvalidTTL)
	}

	_, err = token.NewManager(testSecret, testIssuer, testAudience, -1*time.Minute)
	if err == nil {
		t.Fatal("NewManager: expected error for negative TTL, got nil")
	}

	if !errors.Is(err, token.ErrInvalidTTL) {
		t.Errorf("error = %v, want %v", err, token.ErrInvalidTTL)
	}
}
