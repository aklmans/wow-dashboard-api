//go:build integration

package authrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
)

func TestAuthTokenStoreIntegration(t *testing.T) {
	ctx := context.Background()
	queries := newAuthRepoTestQueries(t, ctx, "test_authrepo_tokens_db")
	userRepo := authrepo.NewUserStore(queries)
	tokenRepo := authrepo.NewAuthTokenStore(queries)

	hash, err := password.Hash("test-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	if _, err := userRepo.CreateUser(ctx, domain.CreateUserInput{
		ID:           userID,
		Email:        "tok@example.com",
		DisplayName:  "Token User",
		PasswordHash: hash,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if err := tokenRepo.CreateAuthToken(ctx, domain.CreateAuthTokenInput{
		ID:        uuid.New(),
		UserID:    userID,
		Purpose:   domain.AuthTokenPurposePasswordReset,
		TokenHash: "hash-1",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateAuthToken failed: %v", err)
	}

	tok, err := tokenRepo.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "hash-1")
	if err != nil || tok.UserID != userID || tok.UsedAt != nil {
		t.Fatalf("GetAuthTokenByHash = %#v, %v; want an unused token for the user", tok, err)
	}

	// The purpose is part of the lookup key: the same hash under another
	// purpose must not match.
	if _, err := tokenRepo.GetAuthTokenByHash(ctx, domain.AuthTokenPurposeEmailVerification, "hash-1"); !errors.Is(err, domain.ErrAuthTokenNotFound) {
		t.Fatalf("cross-purpose lookup err = %v, want ErrAuthTokenNotFound", err)
	}

	consumed, err := tokenRepo.ConsumeAuthToken(ctx, domain.AuthTokenPurposePasswordReset, "hash-1", now)
	if err != nil {
		t.Fatalf("ConsumeAuthToken failed: %v", err)
	}
	if consumed.ID != tok.ID || consumed.UsedAt == nil {
		t.Fatalf("consumed token = %#v, want same token with used_at set", consumed)
	}
	if _, err := tokenRepo.ConsumeAuthToken(ctx, domain.AuthTokenPurposePasswordReset, "hash-1", now); !errors.Is(err, domain.ErrAuthTokenNotFound) {
		t.Fatalf("second consume err = %v, want ErrAuthTokenNotFound", err)
	}
	used, err := tokenRepo.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "hash-1")
	if err != nil || used.UsedAt == nil {
		t.Fatalf("token = %#v, %v; want used_at set", used, err)
	}

	if err := tokenRepo.DeleteAuthTokensForUser(ctx, userID, domain.AuthTokenPurposePasswordReset); err != nil {
		t.Fatalf("DeleteAuthTokensForUser failed: %v", err)
	}
	if _, err := tokenRepo.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "hash-1"); !errors.Is(err, domain.ErrAuthTokenNotFound) {
		t.Fatalf("token still present after delete: %v", err)
	}

	if _, err := tokenRepo.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "no-such-hash"); !errors.Is(err, domain.ErrAuthTokenNotFound) {
		t.Fatalf("missing token err = %v, want ErrAuthTokenNotFound", err)
	}
}
