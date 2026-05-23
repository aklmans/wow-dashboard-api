package authrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// RegisterLoginFailure records a failed sign-in for the user. When the running
// failure count reaches maxAttempts the counter resets and the account is
// locked until lockUntil. It reports whether the account is now locked.
func (s *UserStore) RegisterLoginFailure(ctx context.Context, userID uuid.UUID, maxAttempts int, lockUntil time.Time, now time.Time) (bool, error) {
	locked, err := s.queries.RegisterLoginFailure(ctx, query.RegisterLoginFailureParams{
		ID:          pgUUID(userID),
		MaxAttempts: int32(maxAttempts),
		LockedUntil: pgTimestamp(lockUntil),
		UpdatedAt:   pgTimestamp(now),
	})
	if err != nil {
		return false, mapStoreError(err)
	}
	return locked.Valid && locked.Time.After(now), nil
}

// ClearLoginFailures resets a user's failure counter and lock after a
// successful sign-in.
func (s *UserStore) ClearLoginFailures(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := s.queries.ClearLoginFailures(ctx, query.ClearLoginFailuresParams{
		ID:        pgUUID(userID),
		UpdatedAt: pgTimestamp(now),
	}); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// GetUserByIDForAuth fetches the full auth record, including the password
// hash, by user id. A missing user surfaces as domain.ErrUserNotFound.
func (s *UserStore) GetUserByIDForAuth(ctx context.Context, id uuid.UUID) (domain.AuthUser, error) {
	row, err := s.queries.GetUserByIDForAuth(ctx, pgUUID(id))
	if err != nil {
		return domain.AuthUser{}, mapStoreError(err)
	}
	user, err := authUserFromRow(row)
	if err != nil {
		return domain.AuthUser{}, fmt.Errorf("authrepo: convert auth user: %w", err)
	}
	return user, nil
}

// UpdateUserPassword sets a user's password hash.
func (s *UserStore) UpdateUserPassword(ctx context.Context, userID uuid.UUID, passwordHash string, now time.Time) error {
	if err := s.queries.UpdateUserPassword(ctx, query.UpdateUserPasswordParams{
		ID:           pgUUID(userID),
		PasswordHash: passwordHash,
		UpdatedAt:    pgTimestamp(now),
	}); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// SetEmailVerified marks a user's email address as verified.
func (s *UserStore) SetEmailVerified(ctx context.Context, userID uuid.UUID, verifiedAt time.Time, now time.Time) error {
	if err := s.queries.SetEmailVerified(ctx, query.SetEmailVerifiedParams{
		ID:         pgUUID(userID),
		VerifiedAt: pgTimestamp(verifiedAt),
		UpdatedAt:  pgTimestamp(now),
	}); err != nil {
		return mapStoreError(err)
	}
	return nil
}

// UpdateUserProfile applies the user's own profile-edit. Each nil field is
// left unchanged; status and role assignments are never touched here (those
// belong to the admin path).
func (s *UserStore) UpdateUserProfile(ctx context.Context, userID uuid.UUID, input domain.UpdateProfileInput, now time.Time) error {
	if err := s.queries.UpdateUserFields(ctx, query.UpdateUserFieldsParams{
		ID:          pgUUID(userID),
		DisplayName: pgTextPtr(input.DisplayName),
		AvatarUrl:   pgTextPtr(input.AvatarURL),
		Phone:       pgTextPtr(input.Phone),
		JobTitle:    pgTextPtr(input.JobTitle),
		Company:     pgTextPtr(input.Company),
		UpdatedAt:   pgTimestamp(now),
	}); err != nil {
		return mapStoreError(err)
	}
	return nil
}
