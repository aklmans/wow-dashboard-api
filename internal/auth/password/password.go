// Package password provides Argon2id password hashing and verification.
//
// The default parameters follow the OWASP baseline recommendation for
// Argon2id: 19 MiB of memory, 2 iterations, and a parallelism degree of 1.
// These values target a reasonable balance between resistance to brute-force
// attacks and acceptable latency on commodity hardware. Adjust upward if
// your deployment environment can tolerate longer hashing times.
//
// Encoded hashes use the PHC string format:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64 salt>$<base64 hash>
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Sentinel errors returned by Verify when the encoded hash is malformed or
// uses an algorithm/version this package does not support.
var (
	ErrInvalidHash          = errors.New("password: encoded hash is not in the correct format")
	ErrUnsupportedAlgorithm = errors.New("password: unsupported hashing algorithm")
	ErrUnsupportedVersion   = errors.New("password: unsupported argon2 version")
)

// params holds every tuneable Argon2id parameter in one place so the rest of
// the package never scatters magic numbers.
type params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// defaultParams encodes the OWASP baseline for Argon2id.
var defaultParams = params{
	Memory:      19 * 1024, // 19 MiB expressed in KiB
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

const (
	maxEncodedMemory      uint32 = 256 * 1024 // 256 MiB expressed in KiB
	maxEncodedIterations  uint32 = 10
	maxEncodedParallelism uint8  = 8
	maxEncodedSaltLength  uint32 = 128
	maxEncodedKeyLength   uint32 = 128
	maxEncodedB64Length          = 256
)

// Hash returns an Argon2id encoded hash of the given password using the
// default parameters. The returned string is safe to store directly in a
// database column. An error is returned only when the system's CSPRNG fails
// to produce a salt.
func Hash(password string) (string, error) {
	salt := make([]byte, defaultParams.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password: failed to generate salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		defaultParams.Iterations,
		defaultParams.Memory,
		defaultParams.Parallelism,
		defaultParams.KeyLength,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		defaultParams.Memory,
		defaultParams.Iterations,
		defaultParams.Parallelism,
		b64Salt,
		b64Hash,
	)

	return encoded, nil
}

// Verify checks whether the supplied plaintext password matches the encoded
// Argon2id hash. It returns (true, nil) on a match, (false, nil) on a
// mismatch, and (false, err) when the encoded hash cannot be parsed or uses
// an unsupported algorithm or version. Comparison is performed in constant
// time to prevent timing side-channels.
func Verify(password string, encodedHash string) (bool, error) {
	p, salt, hash, err := decode(encodedHash)
	if err != nil {
		return false, err
	}

	candidate := argon2.IDKey(
		[]byte(password),
		salt,
		p.Iterations,
		p.Memory,
		p.Parallelism,
		p.KeyLength,
	)

	if subtle.ConstantTimeCompare(hash, candidate) == 1 {
		return true, nil
	}

	return false, nil
}

// decode parses a PHC-formatted Argon2id hash string and returns the
// parameters, salt, and derived key. It is intentionally unexported so
// callers interact only through Hash and Verify.
func decode(encodedHash string) (p params, salt, hash []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	// Expected layout: ["", "argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<hash>"]
	if len(parts) != 6 {
		return p, nil, nil, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return p, nil, nil, ErrUnsupportedAlgorithm
	}

	var version int
	_, err = fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return p, nil, nil, ErrUnsupportedVersion
	}

	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism)
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if p.Memory == 0 || p.Memory > maxEncodedMemory ||
		p.Iterations == 0 || p.Iterations > maxEncodedIterations ||
		p.Parallelism == 0 || p.Parallelism > maxEncodedParallelism {
		return p, nil, nil, ErrInvalidHash
	}
	if len(parts[4]) > maxEncodedB64Length || len(parts[5]) > maxEncodedB64Length {
		return p, nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.SaltLength = uint32(len(salt))
	if p.SaltLength == 0 || p.SaltLength > maxEncodedSaltLength {
		return p, nil, nil, ErrInvalidHash
	}

	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.KeyLength = uint32(len(hash))
	if p.KeyLength == 0 || p.KeyLength > maxEncodedKeyLength {
		return p, nil, nil, ErrInvalidHash
	}

	return p, salt, hash, nil
}
