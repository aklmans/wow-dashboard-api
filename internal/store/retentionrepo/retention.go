// Package retentionrepo runs the data-retention purge deletes used by the
// background cleanup job: it drops expired refresh tokens, consumed/expired auth
// tokens, and aged-out audit events so those tables do not grow without bound.
package retentionrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// Store wraps the generated Queries with retention-purge operations.
type Store struct {
	queries *query.Queries
}

// NewStore wraps an existing generated Queries handle.
func NewStore(q *query.Queries) *Store {
	return &Store{queries: q}
}

// NewStoreFromDB builds a Store from a raw pool or transaction.
func NewStoreFromDB(db query.DBTX) *Store {
	return NewStore(query.New(db))
}

// PurgeExpiredRefreshTokens deletes refresh tokens whose expiry has passed.
// Revoked-but-unexpired tokens are intentionally kept so reuse detection still
// recognises them until they expire.
func (s *Store) PurgeExpiredRefreshTokens(ctx context.Context) (int64, error) {
	n, err := s.queries.DeleteExpiredRefreshTokens(ctx)
	if err != nil {
		return 0, fmt.Errorf("retentionrepo: purge expired refresh tokens: %w", err)
	}
	return n, nil
}

// PurgeConsumedOrExpiredAuthTokens deletes auth tokens that are already used or
// have expired.
func (s *Store) PurgeConsumedOrExpiredAuthTokens(ctx context.Context) (int64, error) {
	n, err := s.queries.DeleteConsumedOrExpiredAuthTokens(ctx)
	if err != nil {
		return 0, fmt.Errorf("retentionrepo: purge auth tokens: %w", err)
	}
	return n, nil
}

// PurgeSystemEventsBefore deletes system_events recorded before cutoff.
func (s *Store) PurgeSystemEventsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	n, err := s.queries.DeleteSystemEventsBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("retentionrepo: purge system events: %w", err)
	}
	return n, nil
}
