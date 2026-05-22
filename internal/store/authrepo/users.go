package authrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/aklmans/wow-dashboard-api/internal/auth/domain"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// GetRoleByName resolves a role by its unique name; a missing role surfaces as
// domain.ErrRoleNotFound. Used to look up the default role at sign-up.
func (s *UserStore) GetRoleByName(ctx context.Context, name string) (domain.Role, error) {
	row, err := s.queries.GetRoleByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Role{}, domain.ErrRoleNotFound
		}
		return domain.Role{}, mapStoreError(err)
	}
	id, err := domainUUID(row.ID)
	if err != nil {
		return domain.Role{}, err
	}
	return domain.Role{ID: id, Name: row.Name}, nil
}

// AddUserRole assigns a role to a user. It is idempotent.
func (s *UserStore) AddUserRole(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	if err := s.queries.AddUserRole(ctx, query.AddUserRoleParams{
		UserID: pgUUID(userID),
		RoleID: pgUUID(roleID),
	}); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// GetUserRoles returns the names of every role assigned to a user.
func (s *UserStore) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	rows, err := s.queries.ListUserRoles(ctx, pgUUID(userID))
	if err != nil {
		return nil, mapStoreError(err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	return names, nil
}

// GetUserPermissions returns the user's effective permission strings — the
// union across every role assigned to them.
func (s *UserStore) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	perms, err := s.queries.ListUserPermissions(ctx, pgUUID(userID))
	if err != nil {
		return nil, mapStoreError(err)
	}
	return perms, nil
}
