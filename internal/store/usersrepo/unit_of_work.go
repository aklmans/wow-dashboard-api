package usersrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// UnitOfWork runs a user mutation and its audit event inside one database
// transaction, so a failure to record the audit event rolls back the mutation
// (and vice versa). It mirrors the auth domain's unit of work.
type UnitOfWork struct {
	pool *pgxpool.Pool
}

// NewUnitOfWork constructs a UnitOfWork over the given pool.
func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork {
	return &UnitOfWork{pool: pool}
}

// Do begins a transaction, builds transaction-scoped dependencies, runs fn, and
// commits — rolling back on any error or panic.
func (u *UnitOfWork) Do(ctx context.Context, fn func(context.Context, userservice.WorkDeps) error) error {
	if u == nil || u.pool == nil {
		return fmt.Errorf("usersrepo: unit of work requires a database pool")
	}
	if fn == nil {
		return fmt.Errorf("usersrepo: unit of work callback is nil")
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("usersrepo: begin transaction: %w", err)
	}
	// Safety net: releases the transaction even if fn panics. On the normal
	// commit/rollback paths the transaction is already closed, so this is a
	// no-op (returns pgx.ErrTxClosed).
	defer func() { _ = tx.Rollback(ctx) }()

	queries := query.New(tx)
	deps := userservice.WorkDeps{
		Users: txUserStore{queries: queries},
		Audit: NewSystemEventRecorder(queries),
	}

	if err := fn(ctx, deps); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("usersrepo: rollback transaction: %w", rollbackErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("usersrepo: commit transaction: %w", err)
	}
	return nil
}

// txUserStore adapts transaction-scoped queries to the service's transactional
// user mutator. It reuses the same update logic as the pool-backed UserStore
// but runs it on the caller-provided transaction.
type txUserStore struct {
	queries *query.Queries
}

func (s txUserStore) UpdateUser(ctx context.Context, input domain.UpdateUserInput) (domain.User, error) {
	return updateUserOnTx(ctx, s.queries, input)
}
