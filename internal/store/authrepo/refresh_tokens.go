package authrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
)

type RefreshTokenStore struct {
	queries *query.Queries
}

func NewRefreshTokenStore(q *query.Queries) *RefreshTokenStore {
	return &RefreshTokenStore{queries: q}
}

func NewRefreshTokenStoreFromDB(db query.DBTX) *RefreshTokenStore {
	return NewRefreshTokenStore(query.New(db))
}

func (s *RefreshTokenStore) CreateRefreshToken(ctx context.Context, input domain.CreateRefreshTokenInput) (domain.RefreshToken, error) {
	row, err := s.queries.CreateRefreshToken(ctx, query.CreateRefreshTokenParams{
		ID:         pgUUID(input.ID),
		UserID:     pgUUID(input.UserID),
		TokenHash:  input.TokenHash,
		FamilyID:   pgUUID(input.FamilyID),
		ExpiresAt:  pgTimestamp(input.ExpiresAt),
		UserAgent:  pgText(input.UserAgent),
		IpAddress:  pgText(input.IPAddress),
		LastUsedAt: pgTimestamp(input.LastUsedAt),
		CreatedAt:  pgTimestamp(input.CreatedAt),
		UpdatedAt:  pgTimestamp(input.UpdatedAt),
	})
	if err != nil {
		return domain.RefreshToken{}, mapRefreshTokenError(err)
	}

	token, err := refreshTokenFromRow(row)
	if err != nil {
		return domain.RefreshToken{}, fmt.Errorf("authrepo: convert created refresh token: %w", err)
	}
	return token, nil
}

func (s *RefreshTokenStore) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (domain.RefreshToken, error) {
	row, err := s.queries.GetRefreshTokenByHash(ctx, tokenHash)
	if err != nil {
		return domain.RefreshToken{}, mapRefreshTokenError(err)
	}

	token, err := refreshTokenFromRow(row)
	if err != nil {
		return domain.RefreshToken{}, fmt.Errorf("authrepo: convert refresh token by hash: %w", err)
	}
	return token, nil
}

func (s *RefreshTokenStore) RotateRefreshToken(ctx context.Context, oldTokenID uuid.UUID, input domain.CreateRefreshTokenInput, revokedAt time.Time) (domain.RefreshToken, error) {
	row, err := s.queries.RotateRefreshToken(ctx, query.RotateRefreshTokenParams{
		NewID:      pgUUID(input.ID),
		UserID:     pgUUID(input.UserID),
		TokenHash:  input.TokenHash,
		FamilyID:   pgUUID(input.FamilyID),
		ExpiresAt:  pgTimestamp(input.ExpiresAt),
		UserAgent:  pgText(input.UserAgent),
		IpAddress:  pgText(input.IPAddress),
		LastUsedAt: pgTimestamp(input.LastUsedAt),
		CreatedAt:  pgTimestamp(input.CreatedAt),
		UpdatedAt:  pgTimestamp(input.UpdatedAt),
		RevokedAt:  pgTimestamp(revokedAt),
		OldID:      pgUUID(oldTokenID),
	})
	if err != nil {
		return domain.RefreshToken{}, mapRefreshTokenError(err)
	}

	token, err := refreshTokenFromRow(row)
	if err != nil {
		return domain.RefreshToken{}, fmt.Errorf("authrepo: convert rotated refresh token: %w", err)
	}
	return token, nil
}

func (s *RefreshTokenStore) RevokeRefreshTokenByHash(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	err := s.queries.RevokeRefreshTokenByHash(ctx, query.RevokeRefreshTokenByHashParams{
		TokenHash: tokenHash,
		RevokedAt: pgTimestamp(revokedAt),
	})
	if err != nil {
		return mapRefreshTokenError(err)
	}
	return nil
}

func (s *RefreshTokenStore) RevokeRefreshTokenFamily(ctx context.Context, familyID uuid.UUID, revokedAt time.Time) error {
	err := s.queries.RevokeRefreshTokenFamily(ctx, query.RevokeRefreshTokenFamilyParams{
		FamilyID:  pgUUID(familyID),
		RevokedAt: pgTimestamp(revokedAt),
	})
	if err != nil {
		return mapRefreshTokenError(err)
	}
	return nil
}

// RevokeAllForUser revokes every active refresh token belonging to a user —
// used to end all of that user's sessions, e.g. after a password change.
func (s *RefreshTokenStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID, revokedAt time.Time) error {
	err := s.queries.RevokeAllUserRefreshTokens(ctx, query.RevokeAllUserRefreshTokensParams{
		UserID:    pgUUID(userID),
		RevokedAt: pgTimestamp(revokedAt),
	})
	if err != nil {
		return mapRefreshTokenError(err)
	}
	return nil
}

// RevokeAllForUserExceptFamily revokes every active refresh token belonging to a
// user except the given token family (the caller's current session), so "sign
// out other sessions" leaves the calling device signed in.
func (s *RefreshTokenStore) RevokeAllForUserExceptFamily(ctx context.Context, userID uuid.UUID, familyID uuid.UUID, revokedAt time.Time) error {
	err := s.queries.RevokeUserRefreshTokensExceptFamily(ctx, query.RevokeUserRefreshTokensExceptFamilyParams{
		UserID:    pgUUID(userID),
		FamilyID:  pgUUID(familyID),
		RevokedAt: pgTimestamp(revokedAt),
	})
	if err != nil {
		return mapRefreshTokenError(err)
	}
	return nil
}
