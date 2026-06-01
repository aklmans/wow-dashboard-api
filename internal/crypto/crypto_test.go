package crypto_test

import (
	"strings"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/crypto"
)

const testSecret = "test-mfa-encryption-key-at-least-32-chars"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := crypto.NewCipher(testSecret)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}

	const plaintext = "JBSWY3DPEHPK3PXP" // a TOTP-shaped secret
	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(sealed, plaintext) {
		t.Fatalf("ciphertext leaks the plaintext: %q", sealed)
	}

	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("round-trip = %q, want %q", got, plaintext)
	}
}

func TestEncryptUsesAFreshNonce(t *testing.T) {
	c, _ := crypto.NewCipher(testSecret)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("encrypting the same value twice produced identical ciphertext; nonce is not random")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	c, _ := crypto.NewCipher(testSecret)
	sealed, _ := c.Encrypt("secret")
	// Flip the last base64 char to corrupt the authentication tag.
	tampered := sealed[:len(sealed)-1] + map[bool]string{true: "A", false: "B"}[sealed[len(sealed)-1] != 'A']
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("Decrypt accepted a tampered ciphertext, want error")
	}
}

func TestDecryptFailsWithWrongKey(t *testing.T) {
	a, _ := crypto.NewCipher(testSecret)
	b, _ := crypto.NewCipher("a-different-mfa-encryption-key-32-chars")
	sealed, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(sealed); err == nil {
		t.Fatal("Decrypt with the wrong key succeeded, want error")
	}
}

func TestNewCipherRejectsShortSecret(t *testing.T) {
	if _, err := crypto.NewCipher("too-short"); err == nil {
		t.Fatal("NewCipher accepted a short secret, want error")
	}
}
