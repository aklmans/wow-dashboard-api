//go:build integration

package retentionrepo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/auth/password"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/retentionrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

func TestRetentionStoreIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_retentionrepo_db", "../../../migrations")
	queries := query.New(pool)

	users := authrepo.NewUserStore(queries)
	refresh := authrepo.NewRefreshTokenStore(queries)
	authTokens := authrepo.NewAuthTokenStore(queries)
	store := retentionrepo.NewStore(queries)

	now := time.Now().UTC().Truncate(time.Microsecond)
	pwHash, err := password.Hash("retention-password")
	if err != nil {
		t.Fatalf("password.Hash failed: %v", err)
	}
	userID := uuid.New()
	if _, err := users.CreateUser(ctx, domain.CreateUserInput{
		ID:           userID,
		Email:        "retention@example.com",
		DisplayName:  "Retention User",
		PasswordHash: pwHash,
		Status:       domain.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// --- Refresh tokens: expired (purge), valid (keep), revoked-but-unexpired (keep) ---
	mkRefresh := func(hash string, expiresAt time.Time) {
		t.Helper()
		if _, err := refresh.CreateRefreshToken(ctx, domain.CreateRefreshTokenInput{
			ID:        uuid.New(),
			UserID:    userID,
			TokenHash: hash,
			FamilyID:  uuid.New(),
			ExpiresAt: expiresAt,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			t.Fatalf("CreateRefreshToken(%s) failed: %v", hash, err)
		}
	}
	mkRefresh("rt-expired", now.Add(-time.Hour))
	mkRefresh("rt-valid", now.Add(24*time.Hour))
	mkRefresh("rt-revoked", now.Add(24*time.Hour))
	if err := refresh.RevokeRefreshTokenByHash(ctx, "rt-revoked", now); err != nil {
		t.Fatalf("RevokeRefreshTokenByHash failed: %v", err)
	}

	deleted, err := store.PurgeExpiredRefreshTokens(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredRefreshTokens failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PurgeExpiredRefreshTokens deleted %d, want 1 (only the expired token)", deleted)
	}
	if _, err := refresh.GetRefreshTokenByHash(ctx, "rt-expired"); !errors.Is(err, domain.ErrRefreshTokenNotFound) {
		t.Fatalf("rt-expired lookup err = %v, want ErrRefreshTokenNotFound", err)
	}
	if _, err := refresh.GetRefreshTokenByHash(ctx, "rt-valid"); err != nil {
		t.Fatalf("rt-valid should be kept: %v", err)
	}
	// Revoked but not yet expired is kept so reuse detection still recognises it.
	if _, err := refresh.GetRefreshTokenByHash(ctx, "rt-revoked"); err != nil {
		t.Fatalf("rt-revoked (unexpired) should be kept for reuse detection: %v", err)
	}

	// --- Auth tokens: used (purge), expired (purge), valid (keep) ---
	mkAuth := func(hash string, expiresAt time.Time, markUsed bool) {
		t.Helper()
		id := uuid.New()
		if err := authTokens.CreateAuthToken(ctx, domain.CreateAuthTokenInput{
			ID:        id,
			UserID:    userID,
			Purpose:   domain.AuthTokenPurposePasswordReset,
			TokenHash: hash,
			ExpiresAt: expiresAt,
			CreatedAt: now,
		}); err != nil {
			t.Fatalf("CreateAuthToken(%s) failed: %v", hash, err)
		}
		if markUsed {
			if err := authTokens.MarkAuthTokenUsed(ctx, id, now); err != nil {
				t.Fatalf("MarkAuthTokenUsed(%s) failed: %v", hash, err)
			}
		}
	}
	mkAuth("at-used", now.Add(time.Hour), true)
	mkAuth("at-expired", now.Add(-time.Hour), false)
	mkAuth("at-valid", now.Add(time.Hour), false)

	deleted, err = store.PurgeConsumedOrExpiredAuthTokens(ctx)
	if err != nil {
		t.Fatalf("PurgeConsumedOrExpiredAuthTokens failed: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("PurgeConsumedOrExpiredAuthTokens deleted %d, want 2 (used + expired)", deleted)
	}
	if _, err := authTokens.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "at-valid"); err != nil {
		t.Fatalf("at-valid should be kept: %v", err)
	}
	if _, err := authTokens.GetAuthTokenByHash(ctx, domain.AuthTokenPurposePasswordReset, "at-used"); !errors.Is(err, domain.ErrAuthTokenNotFound) {
		t.Fatalf("at-used lookup err = %v, want ErrAuthTokenNotFound", err)
	}

	// --- System events: old (purge), recent (keep) ---
	mkEvent := func(createdAt time.Time) {
		t.Helper()
		if _, err := queries.CreateSystemEvent(ctx, query.CreateSystemEventParams{
			ID:        pgUUID(t, uuid.New()),
			EventType: "test.retention",
			Message:   "retention test event",
			Metadata:  []byte(`{}`),
			CreatedAt: pgTimestamp(t, createdAt),
		}); err != nil {
			t.Fatalf("CreateSystemEvent failed: %v", err)
		}
	}
	mkEvent(now.Add(-100 * 24 * time.Hour)) // older than the 90-day cutoff
	mkEvent(now.Add(-24 * time.Hour))       // recent, kept

	deleted, err = store.PurgeSystemEventsBefore(ctx, now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeSystemEventsBefore failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("PurgeSystemEventsBefore deleted %d, want 1 (only the aged-out event)", deleted)
	}
}

func pgUUID(t *testing.T, id uuid.UUID) pgtype.UUID {
	t.Helper()
	var out pgtype.UUID
	if err := out.Scan(id.String()); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return out
}

func pgTimestamp(t *testing.T, ts time.Time) pgtype.Timestamptz {
	t.Helper()
	var out pgtype.Timestamptz
	if err := out.Scan(ts); err != nil {
		t.Fatalf("scan timestamptz: %v", err)
	}
	return out
}
