//go:build integration

package authrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/google/uuid"
)

func TestRefreshTokenStoreIntegration(t *testing.T) {
	ctx := context.Background()
	queries := newAuthRepoTestQueries(t, ctx, "test_authrepo_refresh_tokens_db")
	userRepo := authrepo.NewUserStore(queries)
	refreshRepo := authrepo.NewRefreshTokenStore(queries)

	userID := uuid.New()
	hash, err := password.Hash("correct-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = userRepo.CreateUser(ctx, domain.CreateUserInput{
		ID:           userID,
		Email:        "refresh@example.com",
		DisplayName:  "Refresh User",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	tokenID := uuid.New()
	familyID := uuid.New()
	created, err := refreshRepo.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
		ID:        tokenID,
		UserID:    userID,
		TokenHash: "hash-one",
		FamilyID:  familyID,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateRefreshToken failed: %v", err)
	}
	if created.ID != tokenID || created.UserID != userID || created.TokenHash != "hash-one" {
		t.Fatalf("created refresh token = %#v, want token/user/hash preserved", created)
	}
	if created.RevokedAt != nil || created.ReplacedByTokenID != nil {
		t.Fatalf("new refresh token revoke fields = %#v/%#v, want nil", created.RevokedAt, created.ReplacedByTokenID)
	}

	fetched, err := refreshRepo.GetRefreshTokenByHash(ctx, "hash-one")
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash failed: %v", err)
	}
	if fetched.ID != tokenID || fetched.FamilyID != familyID {
		t.Fatalf("fetched refresh token = %#v, want matching token/family", fetched)
	}

	_, err = refreshRepo.GetRefreshTokenByHash(ctx, "missing-hash")
	if !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Fatalf("missing refresh token error = %v, want ErrRefreshTokenNotFound", err)
	}

	replacementID := uuid.New()
	rotated, err := refreshRepo.RotateRefreshToken(ctx, tokenID, domain.CreateRefreshTokenInput{
		ID:        replacementID,
		UserID:    userID,
		TokenHash: "hash-two",
		FamilyID:  familyID,
		ExpiresAt: now.Add(48 * time.Hour),
		CreatedAt: now,
		UpdatedAt: now,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RotateRefreshToken failed: %v", err)
	}
	if rotated.ID != replacementID || rotated.TokenHash != "hash-two" {
		t.Fatalf("rotated token = %#v, want replacement token", rotated)
	}
	old, err := refreshRepo.GetRefreshTokenByHash(ctx, "hash-one")
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash old token failed: %v", err)
	}
	if old.RevokedAt == nil {
		t.Fatal("old refresh token was not revoked during rotation")
	}
	if old.ReplacedByTokenID == nil || *old.ReplacedByTokenID != replacementID {
		t.Fatalf("old replaced_by_token_id = %#v, want %s", old.ReplacedByTokenID, replacementID)
	}

	if err := refreshRepo.RevokeRefreshTokenByHash(ctx, "hash-two", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("RevokeRefreshTokenByHash failed: %v", err)
	}
	revoked, err := refreshRepo.GetRefreshTokenByHash(ctx, "hash-two")
	if err != nil {
		t.Fatalf("GetRefreshTokenByHash revoked token failed: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("refresh token was not revoked")
	}

	expired, err := refreshRepo.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: "expired-hash",
		FamilyID:  uuid.New(),
		ExpiresAt: now.Add(-time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateRefreshToken expired token failed: %v", err)
	}
	if !expired.ExpiresAt.Before(now) {
		t.Fatalf("expired token ExpiresAt = %s, want before %s", expired.ExpiresAt, now)
	}

	// Family revocation: every active token sharing a family is revoked in
	// one call, which backs refresh-token-reuse detection.
	famID := uuid.New()
	for _, hash := range []string{"fam-hash-a", "fam-hash-b"} {
		if _, err := refreshRepo.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
			ID:        uuid.New(),
			UserID:    userID,
			TokenHash: hash,
			FamilyID:  famID,
			ExpiresAt: now.Add(24 * time.Hour),
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRefreshToken family token %s failed: %v", hash, err)
		}
	}
	if err := refreshRepo.RevokeRefreshTokenFamily(ctx, famID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("RevokeRefreshTokenFamily failed: %v", err)
	}
	for _, hash := range []string{"fam-hash-a", "fam-hash-b"} {
		tok, err := refreshRepo.GetRefreshTokenByHash(ctx, hash)
		if err != nil {
			t.Fatalf("GetRefreshTokenByHash %s failed: %v", hash, err)
		}
		if tok.RevokedAt == nil {
			t.Fatalf("family token %s was not revoked by RevokeRefreshTokenFamily", hash)
		}
	}
}
