package projectsrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// UnitOfWork runs a project mutation and its audit event inside one database
// transaction, so a failure to record the audit event rolls back the mutation
// (and vice versa). It mirrors the auth domain's unit of work.
//
// The project store already operates on an injected *query.Queries, so a
// transaction-scoped store is simply NewProjectStore(query.New(tx)) — no
// per-method transaction extraction is needed.
type UnitOfWork struct {
	pool *pgxpool.Pool
}

// NewUnitOfWork constructs a UnitOfWork over the given pool.
func NewUnitOfWork(pool *pgxpool.Pool) *UnitOfWork {
	return &UnitOfWork{pool: pool}
}

// Do begins a transaction, builds transaction-scoped dependencies, runs fn, and
// commits — rolling back on any error or panic.
func (u *UnitOfWork) Do(ctx context.Context, fn func(context.Context, projectservice.WorkDeps) error) error {
	if u == nil || u.pool == nil {
		return fmt.Errorf("projectsrepo: unit of work requires a database pool")
	}
	if fn == nil {
		return fmt.Errorf("projectsrepo: unit of work callback is nil")
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("projectsrepo: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := query.New(tx)
	deps := projectservice.WorkDeps{
		Projects: NewProjectStore(queries),
		Audit:    NewSystemEventRecorder(queries),
	}

	if err := fn(ctx, deps); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("projectsrepo: rollback transaction: %w", rollbackErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("projectsrepo: commit transaction: %w", err)
	}
	return nil
}
