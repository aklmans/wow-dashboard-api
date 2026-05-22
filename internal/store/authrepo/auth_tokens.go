package authrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// AuthTokenStore is the store adapter for the one-time tokens backing the
// password-reset and email-verification flows.
type AuthTokenStore struct {
	queries *query.Queries
}

func NewAuthTokenStore(q *query.Queries) *AuthTokenStore {
	return &AuthTokenStore{queries: q}
}

func (s *AuthTokenStore) CreateAuthToken(ctx context.Context, input domain.CreateAuthTokenInput) error {
	if err := s.queries.CreateAuthToken(ctx, query.CreateAuthTokenParams{
		ID:        pgUUID(input.ID),
		UserID:    pgUUID(input.UserID),
		Purpose:   input.Purpose,
		TokenHash: input.TokenHash,
		ExpiresAt: pgTimestamp(input.ExpiresAt),
		CreatedAt: pgTimestamp(input.CreatedAt),
	}); err != nil {
		return fmt.Errorf("authrepo: create auth token: %w", err)
	}
	return nil
}

// GetAuthTokenByHash looks up a token by its hash and purpose. A missing token
// surfaces as domain.ErrAuthTokenNotFound.
func (s *AuthTokenStore) GetAuthTokenByHash(ctx context.Context, purpose string, tokenHash string) (domain.AuthToken, error) {
	row, err := s.queries.GetAuthTokenByHash(ctx, query.GetAuthTokenByHashParams{
		TokenHash: tokenHash,
		Purpose:   purpose,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuthToken{}, domain.ErrAuthTokenNotFound
		}
		return domain.AuthToken{}, fmt.Errorf("authrepo: get auth token: %w", err)
	}
	return authTokenFromRow(row)
}

func (s *AuthTokenStore) MarkAuthTokenUsed(ctx context.Context, id uuid.UUID, usedAt time.Time) error {
	if err := s.queries.MarkAuthTokenUsed(ctx, query.MarkAuthTokenUsedParams{
		ID:     pgUUID(id),
		UsedAt: pgTimestamp(usedAt),
	}); err != nil {
		return fmt.Errorf("authrepo: mark auth token used: %w", err)
	}
	return nil
}

// DeleteAuthTokensForUser removes all of a user's tokens of a purpose, so a
// newly issued token supersedes any earlier ones.
func (s *AuthTokenStore) DeleteAuthTokensForUser(ctx context.Context, userID uuid.UUID, purpose string) error {
	if err := s.queries.DeleteAuthTokensForUser(ctx, query.DeleteAuthTokensForUserParams{
		UserID:  pgUUID(userID),
		Purpose: purpose,
	}); err != nil {
		return fmt.Errorf("authrepo: delete auth tokens: %w", err)
	}
	return nil
}

func authTokenFromRow(row query.AuthToken) (domain.AuthToken, error) {
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.AuthToken{}, err
	}
	userID, err := domainUUID(row.UserID)
	if err != nil {
		return domain.AuthToken{}, err
	}
	expiresAt, err := domainTimestamp(row.ExpiresAt)
	if err != nil {
		return domain.AuthToken{}, err
	}
	createdAt, err := domainTimestamp(row.CreatedAt)
	if err != nil {
		return domain.AuthToken{}, err
	}
	return domain.AuthToken{
		ID:        id,
		UserID:    userID,
		Purpose:   row.Purpose,
		TokenHash: row.TokenHash,
		ExpiresAt: expiresAt,
		UsedAt:    nullableDomainTimestamp(row.UsedAt),
		CreatedAt: createdAt,
	}, nil
}
