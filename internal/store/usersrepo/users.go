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

	filters := query.CountUsersPageParams{
		Search: pgText(escapeLikePattern(input.Search)),
		Role:   pgText(string(input.Role)),
		Status: pgText(string(input.Status)),
	}
	total, err := s.queries.CountUsersPage(ctx, filters)
	if err != nil {
		return domain.ListUsersResult{}, fmt.Errorf("usersrepo: count users: %w", err)
	}

	rows, err := s.queries.ListUsersPage(ctx, query.ListUsersPageParams{
		Search:    filters.Search,
		Role:      filters.Role,
		Status:    filters.Status,
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
	return user, nil
}
