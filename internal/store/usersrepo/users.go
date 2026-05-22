package usersrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
)

type UserStore struct {
	queries *query.Queries
}

func NewUserStore(q *query.Queries) *UserStore {
	return &UserStore{queries: q}
}

func NewUserStoreFromDB(db query.DBTX) *UserStore {
	return NewUserStore(query.New(db))
}

func (s *UserStore) ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error) {
	if s.queries == nil {
		return domain.ListUsersResult{}, fmt.Errorf("usersrepo: queries is nil")
	}

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
	if s.queries == nil {
		return domain.User{}, fmt.Errorf("usersrepo: queries is nil")
	}

	row, err := s.queries.GetUserByID(ctx, pgUUIDFromDomain(id))
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

	roleRows, err := s.queries.ListUserRoles(ctx, pgUUIDFromDomain(id))
	if err != nil {
		return domain.User{}, fmt.Errorf("usersrepo: list user roles: %w", err)
	}
	user.Roles = make([]string, 0, len(roleRows))
	for _, r := range roleRows {
		user.Roles = append(user.Roles, r.Name)
	}
	return user, nil
}

// SetUserStatus updates a user's status. A missing user surfaces as
// domain.ErrUserNotFound.
func (s *UserStore) SetUserStatus(ctx context.Context, input domain.SetUserStatusInput) error {
	if s.queries == nil {
		return fmt.Errorf("usersrepo: queries is nil")
	}

	_, err := s.queries.UpdateUserStatus(ctx, query.UpdateUserStatusParams{
		ID:        pgUUIDFromDomain(input.ID),
		Status:    string(input.Status),
		UpdatedAt: pgTimestamp(input.UpdatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrUserNotFound
		}
		return fmt.Errorf("usersrepo: set user status: %w", err)
	}
	return nil
}

// ReplaceUserRoles replaces a user's full set of role assignments. Every role
// id must exist or the call fails with domain.ErrRoleNotFound and no change is
// made.
func (s *UserStore) ReplaceUserRoles(ctx context.Context, input domain.ReplaceUserRolesInput) error {
	if s.queries == nil {
		return fmt.Errorf("usersrepo: queries is nil")
	}

	roleIDs := pgUUIDsFromDomain(input.RoleIDs)
	found, err := s.queries.CountRolesByIDs(ctx, roleIDs)
	if err != nil {
		return fmt.Errorf("usersrepo: count roles: %w", err)
	}
	if int(found) != len(input.RoleIDs) {
		return domain.ErrRoleNotFound
	}

	if err := s.queries.ReplaceUserRoles(ctx, query.ReplaceUserRolesParams{
		UserID:  pgUUIDFromDomain(input.UserID),
		RoleIds: roleIDs,
	}); err != nil {
		return fmt.Errorf("usersrepo: replace user roles: %w", err)
	}
	return nil
}
