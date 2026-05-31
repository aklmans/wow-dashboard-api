package password_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
)

func TestHash_ReturnsValidEncodedHash(t *testing.T) {
	encoded, err := password.Hash("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("encoded hash has unexpected prefix: %s", encoded)
	}

	// PHC format should have exactly 6 $-separated parts (first is empty).
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		t.Errorf("expected 6 $-delimited parts, got %d: %s", len(parts), encoded)
	}
}

func TestVerify_CorrectPassword(t *testing.T) {
	pw := "s3cure-P@ssw0rd!"
	encoded, err := password.Hash(pw)
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	match, err := password.Verify(pw, encoded)
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}
	if !match {
		t.Error("Verify should return true for the correct password")
	}
}

func TestVerify_WrongPassword(t *testing.T) {
	encoded, err := password.Hash("rightpassword")
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	match, err := password.Verify("wrongpassword", encoded)
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}
	if match {
		t.Error("Verify should return false for an incorrect password")
	}
}

func TestHash_UniqueSalts(t *testing.T) {
	pw := "samePasswordTwice"

	h1, err := password.Hash(pw)
	if err != nil {
		t.Fatalf("first Hash returned unexpected error: %v", err)
	}

	h2, err := password.Hash(pw)
	if err != nil {
		t.Fatalf("second Hash returned unexpected error: %v", err)
	}

	if h1 == h2 {
		t.Error("hashing the same password twice should produce different encoded hashes (unique salts)")
	}
}

func TestVerify_MalformedHash(t *testing.T) {
	_, err := password.Verify("anything", "not-a-valid-hash")
	if !errors.Is(err, password.ErrInvalidHash) {
		t.Errorf("expected ErrInvalidHash, got: %v", err)
	}
}

func TestVerify_UnsupportedAlgorithm(t *testing.T) {
	// Swap argon2id for argon2d to trigger algorithm check.
	encoded, err := password.Hash("test")
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	bad := strings.Replace(encoded, "$argon2id$", "$argon2d$", 1)

	_, err = password.Verify("test", bad)
	if !errors.Is(err, password.ErrUnsupportedAlgorithm) {
		t.Errorf("expected ErrUnsupportedAlgorithm, got: %v", err)
	}
}

func TestVerify_UnsupportedVersion(t *testing.T) {
	// Replace v=19 with v=16 to simulate an unsupported version.
	encoded, err := password.Hash("test")
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	bad := strings.Replace(encoded, "$v=19$", "$v=16$", 1)

	_, err = password.Verify("test", bad)
	if !errors.Is(err, password.ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got: %v", err)
	}
}

func TestVerify_RejectsExcessiveArgon2Parameters(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"memory", "$argon2id$v=19$m=262145,t=2,p=1$c2FsdA$aGFzaA"},
		{"iterations", "$argon2id$v=19$m=19456,t=11,p=1$c2FsdA$aGFzaA"},
		{"parallelism", "$argon2id$v=19$m=19456,t=2,p=9$c2FsdA$aGFzaA"},
		{"empty salt", "$argon2id$v=19$m=19456,t=2,p=1$$aGFzaA"},
		{"empty key", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := password.Verify("anything", tt.hash); !errors.Is(err, password.ErrInvalidHash) {
				t.Fatalf("Verify error = %v, want ErrInvalidHash", err)
			}
		})
	}
}

func TestVerify_EmptyPassword(t *testing.T) {
	encoded, err := password.Hash("notempty")
	if err != nil {
		t.Fatalf("Hash returned unexpected error: %v", err)
	}

	match, err := password.Verify("", encoded)
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}
	if match {
		t.Error("empty password should not match a hash of a non-empty password")
	}
}

func TestHash_EmptyPassword(t *testing.T) {
	encoded, err := password.Hash("")
	if err != nil {
		t.Fatalf("Hash with empty password returned unexpected error: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Errorf("encoded hash of empty password has unexpected prefix: %s", encoded)
	}

	// Verify the empty-password hash round-trips correctly.
	match, err := password.Verify("", encoded)
	if err != nil {
		t.Fatalf("Verify returned unexpected error: %v", err)
	}
	if !match {
		t.Error("Verify should return true when verifying an empty password against its own hash")
	}
}
