package rolesrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	roleservice "github.com/aklmans/wow-dashboard-api/internal/roles/service"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// UnitOfWork runs a role mutation and its audit event inside one database
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
func (u *UnitOfWork) Do(ctx context.Context, fn func(context.Context, roleservice.WorkDeps) error) error {
	if u == nil || u.pool == nil {
		return fmt.Errorf("rolesrepo: unit of work requires a database pool")
	}
	if fn == nil {
		return fmt.Errorf("rolesrepo: unit of work callback is nil")
	}

	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("rolesrepo: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := query.New(tx)
	deps := roleservice.WorkDeps{
		Roles: txRoleStore{queries: queries},
		Audit: NewSystemEventRecorder(queries),
	}

	if err := fn(ctx, deps); err != nil {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rolesrepo: rollback transaction: %w", rollbackErr))
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("rolesrepo: commit transaction: %w", err)
	}
	return nil
}

// txRoleStore adapts transaction-scoped queries to the service's transactional
// role mutator, reusing the same mutation logic as the pool-backed RoleStore.
type txRoleStore struct {
	queries *query.Queries
}

func (s txRoleStore) CreateRole(ctx context.Context, input domain.CreateRoleInput) (domain.Role, error) {
	return createRoleOnTx(ctx, s.queries, input)
}

func (s txRoleStore) UpdateRole(ctx context.Context, input domain.UpdateRoleInput) (domain.Role, error) {
	return updateRoleOnTx(ctx, s.queries, input)
}

func (s txRoleStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	return deleteRoleOnTx(ctx, s.queries, id)
}
