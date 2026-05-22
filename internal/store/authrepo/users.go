package authrepo

import (
	"context"
	"fmt"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
)

type UserStore struct {
	queries *query.Queries
}

type DBTX = query.DBTX

func NewUserStore(q *query.Queries) *UserStore {
	return &UserStore{queries: q}
}

func NewUserStoreFromDB(db query.DBTX) *UserStore {
	return NewUserStore(query.New(db))
}

func (s *UserStore) CreateUser(ctx context.Context, input domain.CreateUserInput) (domain.User, error) {
	row, err := s.queries.CreateUser(ctx, query.CreateUserParams{
		ID:           pgUUID(input.ID),
		Email:        input.Email,
		DisplayName:  input.DisplayName,
		PasswordHash: input.PasswordHash,
		Status:       string(input.Status),
		Role:         string(input.Role),
		CreatedAt:    pgTimestamp(input.CreatedAt),
		UpdatedAt:    pgTimestamp(input.UpdatedAt),
	})
	if err != nil {
		return domain.User{}, mapStoreError(err)
	}
	user, err := userFromCreateRow(row)
	if err != nil {
		return domain.User{}, fmt.Errorf("authrepo: convert created user: %w", err)
	}
	return user, nil
}

func (s *UserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := s.queries.GetUserByID(ctx, pgUUID(id))
	if err != nil {
		return domain.User{}, mapStoreError(err)
	}
	user, err := userFromGetByIDRow(row)
	if err != nil {
		return domain.User{}, fmt.Errorf("authrepo: convert user by id: %w", err)
	}
	return user, nil
}

func (s *UserStore) GetUserByEmailForAuth(ctx context.Context, email string) (domain.AuthUser, error) {
	row, err := s.queries.GetUserByEmailForAuth(ctx, email)
	if err != nil {
		return domain.AuthUser{}, mapStoreError(err)
	}
	user, err := authUserFromRow(row)
	if err != nil {
		return domain.AuthUser{}, fmt.Errorf("authrepo: convert auth user: %w", err)
	}
	return user, nil
}
