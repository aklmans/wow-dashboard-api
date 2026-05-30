// Package rolesrepo is the PostgreSQL store adapter for role management.
package rolesrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
)

// rolesNameUniqueConstraint is the PostgreSQL-generated name of the UNIQUE
// constraint on roles.name.
const rolesNameUniqueConstraint = "roles_name_key"

// RoleStore is the role management store adapter. It holds the pool directly
// because creating and updating a role spans multiple statements that must run
// in one transaction.
type RoleStore struct {
	pool    *pgxpool.Pool
	queries *query.Queries
}

func NewRoleStore(pool *pgxpool.Pool) *RoleStore {
	return &RoleStore{pool: pool, queries: query.New(pool)}
}

func (s *RoleStore) ListRoles(ctx context.Context) ([]domain.Role, error) {
	rows, err := s.queries.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("rolesrepo: list roles: %w", err)
	}
	roles := make([]domain.Role, 0, len(rows))
	for _, row := range rows {
		role, err := roleFromListRow(row)
		if err != nil {
			return nil, fmt.Errorf("rolesrepo: convert role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, nil
}

func (s *RoleStore) GetRoleByID(ctx context.Context, id uuid.UUID) (domain.Role, error) {
	return getRoleByID(ctx, s.queries, id)
}

// getRoleByID reads a role using the provided queries, which may be pool- or
// transaction-scoped. Inside a transaction it sees that transaction's own
// uncommitted writes, so the create/update paths can read back the row they
// just wrote before the commit.
func getRoleByID(ctx context.Context, q *query.Queries, id uuid.UUID) (domain.Role, error) {
	row, err := q.GetRoleByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Role{}, domain.ErrRoleNotFound
		}
		return domain.Role{}, fmt.Errorf("rolesrepo: get role: %w", err)
	}
	role, err := roleFromGetRow(row)
	if err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: convert role: %w", err)
	}
	return role, nil
}

// CreateRole inserts a role and its permissions in one transaction, then
// returns the role in its canonical shape.
func (s *RoleStore) CreateRole(ctx context.Context, input domain.CreateRoleInput) (domain.Role, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	role, err := createRoleOnTx(ctx, query.New(tx), input)
	if err != nil {
		return domain.Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: commit: %w", err)
	}
	return role, nil
}

// createRoleOnTx inserts the role and its permissions using the caller-scoped
// queries, performing no Begin/Commit so it can run inside a service-level unit
// of work that records the audit event in the same transaction.
func createRoleOnTx(ctx context.Context, q *query.Queries, input domain.CreateRoleInput) (domain.Role, error) {
	if _, err := q.CreateRole(ctx, query.CreateRoleParams{
		ID:          pgUUID(input.ID),
		Name:        input.Name,
		Description: input.Description,
		CreatedAt:   pgTimestamp(input.CreatedAt),
		UpdatedAt:   pgTimestamp(input.UpdatedAt),
	}); err != nil {
		return domain.Role{}, mapRoleError(err)
	}
	if err := q.AddRolePermissions(ctx, query.AddRolePermissionsParams{
		RoleID:      pgUUID(input.ID),
		Permissions: input.Permissions,
	}); err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: add role permissions: %w", err)
	}
	return getRoleByID(ctx, q, input.ID)
}

// UpdateRole applies the role detail and/or permission-set changes in one
// transaction. The caller validates that the role exists and is not a system
// role before calling.
func (s *RoleStore) UpdateRole(ctx context.Context, input domain.UpdateRoleInput) (domain.Role, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	role, err := updateRoleOnTx(ctx, query.New(tx), input)
	if err != nil {
		return domain.Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Role{}, fmt.Errorf("rolesrepo: commit: %w", err)
	}
	return role, nil
}

// updateRoleOnTx applies the role detail and/or permission-set changes using
// the caller-scoped queries, performing no Begin/Commit.
func updateRoleOnTx(ctx context.Context, q *query.Queries, input domain.UpdateRoleInput) (domain.Role, error) {
	if input.Name != nil || input.Description != nil {
		if err := q.UpdateRoleDetails(ctx, query.UpdateRoleDetailsParams{
			ID:          pgUUID(input.ID),
			Name:        pgTextPtr(input.Name),
			Description: pgTextPtr(input.Description),
			UpdatedAt:   pgTimestamp(input.UpdatedAt),
		}); err != nil {
			return domain.Role{}, mapRoleError(err)
		}
	}
	if input.Permissions != nil {
		if err := q.ReplaceRolePermissions(ctx, query.ReplaceRolePermissionsParams{
			RoleID:      pgUUID(input.ID),
			Permissions: *input.Permissions,
		}); err != nil {
			return domain.Role{}, fmt.Errorf("rolesrepo: replace role permissions: %w", err)
		}
	}
	return getRoleByID(ctx, q, input.ID)
}

// DeleteRole deletes a non-system role that has no users assigned. The caller
// validates existence and the system-role guard first; a zero-row result here
// therefore means a user was assigned after that check.
func (s *RoleStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	return deleteRoleOnTx(ctx, s.queries, id)
}

// deleteRoleOnTx deletes the role using the caller-scoped queries. Delete is a
// single statement, so it needs no transaction of its own, but accepting the
// queries lets a unit of work run it on the same transaction as the audit event.
func deleteRoleOnTx(ctx context.Context, q *query.Queries, id uuid.UUID) error {
	rows, err := q.DeleteRole(ctx, pgUUID(id))
	if err != nil {
		return fmt.Errorf("rolesrepo: delete role: %w", err)
	}
	if rows == 0 {
		return domain.ErrRoleInUse
	}
	return nil
}

func mapRoleError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == rolesNameUniqueConstraint {
		return domain.ErrNameConflict
	}
	return fmt.Errorf("rolesrepo: store operation failed: %w", err)
}
