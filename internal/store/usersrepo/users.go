package usersrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
)

// UserStore is the user management store adapter. It holds the pool directly
// because an admin update can change a user's status and role set together,
// which must be applied in one transaction.
type UserStore struct {
	pool    *pgxpool.Pool
	queries *query.Queries
}

func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool, queries: query.New(pool)}
}

func (s *UserStore) ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error) {
	search := pgText(escapeLikePattern(input.Search))
	role := pgText(input.Role)
	status := pgText(string(input.Status))

	total, err := s.queries.CountUsersPage(ctx, query.CountUsersPageParams{
		Search: search,
		Role:   role,
		Status: status,
	})
	if err != nil {
		return domain.ListUsersResult{}, fmt.Errorf("usersrepo: count users: %w", err)
	}

	rows, err := s.queries.ListUsersPage(ctx, query.ListUsersPageParams{
		Search:    search,
		Role:      role,
		Status:    status,
		LimitVal:  int32(input.PageSize),
		OffsetVal: int32(input.Offset),
	})
	if err != nil {
		return domain.ListUsersResult{}, fmt.Errorf("usersrepo: list users: %w", err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		user, err := userFromListRow(row)
		if err != nil {
			return domain.ListUsersResult{}, fmt.Errorf("usersrepo: convert user: %w", err)
		}
		users = append(users, user)
	}

	return domain.ListUsersResult{
		Users:    users,
		Page:     input.Page,
		PageSize: input.PageSize,
		Total:    int(total),
	}, nil
}

// GetUserByID fetches a single user together with the names of their roles.
func (s *UserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return getUserByID(ctx, s.queries, id)
}

func getUserByID(ctx context.Context, q *query.Queries, id uuid.UUID) (domain.User, error) {
	row, err := q.GetUserByID(ctx, pgUUIDFromDomain(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("usersrepo: get user: %w", err)
	}

	user, err := userFromDetailRow(row)
	if err != nil {
		return domain.User{}, fmt.Errorf("usersrepo: convert user: %w", err)
	}

	roleRows, err := q.ListUserRoles(ctx, pgUUIDFromDomain(id))
	if err != nil {
		return domain.User{}, fmt.Errorf("usersrepo: list user roles: %w", err)
	}
	user.Roles = make([]string, 0, len(roleRows))
	for _, r := range roleRows {
		user.Roles = append(user.Roles, r.Name)
	}
	return user, nil
}

// UpdateUser applies an admin status change and/or role-set replacement to a
// user in one transaction, then returns the resulting user. A missing user
// surfaces as domain.ErrUserNotFound; an unknown role id as
// domain.ErrRoleNotFound. On any error the transaction is rolled back, so the
// update is all-or-nothing.
func (s *UserStore) UpdateUser(ctx context.Context, input domain.UpdateUserInput) (domain.User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("usersrepo: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := query.New(tx)

	// Confirm the user exists inside the transaction so a roles-only update on
	// a missing user reports ErrUserNotFound rather than a foreign-key failure.
	if _, err := q.GetUserByID(ctx, pgUUIDFromDomain(input.ID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("usersrepo: load user: %w", err)
	}

	if input.Status != nil || input.AvatarURL != nil || input.Phone != nil ||
		input.JobTitle != nil || input.Company != nil {
		if err := q.UpdateUserFields(ctx, query.UpdateUserFieldsParams{
			ID:        pgUUIDFromDomain(input.ID),
			Status:    pgStatusPtr(input.Status),
			AvatarUrl: pgTextPtr(input.AvatarURL),
			Phone:     pgTextPtr(input.Phone),
			JobTitle:  pgTextPtr(input.JobTitle),
			Company:   pgTextPtr(input.Company),
			UpdatedAt: pgTimestamp(input.UpdatedAt),
		}); err != nil {
			return domain.User{}, fmt.Errorf("usersrepo: update user fields: %w", err)
		}
	}

	if input.RoleIDs != nil {
		roleIDs := pgUUIDsFromDomain(input.RoleIDs)
		found, err := q.CountRolesByIDs(ctx, roleIDs)
		if err != nil {
			return domain.User{}, fmt.Errorf("usersrepo: count roles: %w", err)
		}
		if int(found) != len(input.RoleIDs) {
			return domain.User{}, domain.ErrRoleNotFound
		}
		if err := q.ReplaceUserRoles(ctx, query.ReplaceUserRolesParams{
			UserID:  pgUUIDFromDomain(input.ID),
			RoleIds: roleIDs,
		}); err != nil {
			return domain.User{}, fmt.Errorf("usersrepo: replace user roles: %w", err)
		}
	}

	user, err := getUserByID(ctx, q, input.ID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.User{}, fmt.Errorf("usersrepo: commit: %w", err)
	}
	return user, nil
}
