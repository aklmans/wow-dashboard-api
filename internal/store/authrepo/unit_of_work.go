package authrepo

import (
	"context"
	"errors"
	"fmt"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UnitOfWork struct {
	pool *pgxpool.Pool
}

func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork {
	return &UnitOfWork{pool: pool}
}

func (u *UnitOfWork) Do(ctx context.Context, fn func(context.Context, authservice.WorkDeps) error) error {
	if u == nil || u.pool == nil {
		return fmt.Errorf("authrepo: unit of work requires a database pool")
	}
	if fn == nil {
		return fmt.Errorf("authrepo: unit of work callback is nil")
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("authrepo: begin transaction: %w", err)
	}
	// Safety net: guarantees the transaction is released even if fn panics.
	// On the normal commit/rollback paths the transaction is already closed,
	// so this deferred Rollback is a no-op (returns pgx.ErrTxClosed).
	defer func() { _ = tx.Rollback(ctx) }()

	queries := query.New(tx)
	deps := authservice.WorkDeps{
		Users:         NewUserStore(queries),
		RefreshTokens: NewRefreshTokenStore(queries),
		AuthTokens:    NewAuthTokenStore(queries),
	}

	if err := fn(ctx, deps); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("authrepo: rollback transaction: %w", rollbackErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("authrepo: commit transaction: %w", err)
	}
	return nil
}
