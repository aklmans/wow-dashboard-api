// Package crypto provides authenticated symmetric encryption (AES-256-GCM) for
// secret material that must be recoverable at rest, such as the TOTP shared
// secret used for MFA. The 32-byte AES key is derived as SHA-256 of the
// configured secret, so that secret must be a high-entropy value (like the JWT
// signing secret) of at least minSecretLength characters.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// minSecretLength matches the JWT secret minimum: the derived AES key is only
// as strong as the configured secret.
const minSecretLength = 32

// ErrCiphertextTooShort is returned when decrypt input cannot contain a nonce.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext too short")

// Cipher seals and opens short secrets with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher derives an AES-256 key from secret (SHA-256) and returns a Cipher.
// It errors if the secret is shorter than minSecretLength.
func NewCipher(secret string) (*Cipher, error) {
	if len(secret) < minSecretLength {
		return nil, fmt.Errorf("crypto: encryption secret must be at least %d characters", minSecretLength)
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext and returns base64(nonce || ciphertext+tag). A fresh
// random nonce is used per call, so encrypting the same value twice yields
// different ciphertexts.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: read nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A tampered ciphertext or a wrong key fails the GCM
// authentication and returns an error rather than garbage plaintext.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: decode: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", ErrCiphertextTooShort
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}
