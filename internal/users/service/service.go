// Package service implements user management use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/http/pagination"
	"github.com/aklmans/wow-dashboard-api/internal/http/pathparam"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
)

var (
	ErrInvalidInput = errors.New("users: invalid input")
	ErrNotFound     = errors.New("users: not found")
)

type ListUsersInput struct {
	Page     int
	PageSize int
	Search   string
	Role     string
	Status   string
}

type UserStore interface {
	ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

type Service struct {
	store UserStore
}

func NewService(store UserStore) *Service {
	return &Service{store: store}
}

func (s *Service) ListUsers(ctx context.Context, input ListUsersInput) (domain.ListUsersResult, error) {
	if s.store == nil {
		return domain.ListUsersResult{}, fmt.Errorf("users: store is nil")
	}

	normalized, err := normalizeListUsersInput(input)
	if err != nil {
		return domain.ListUsersResult{}, err
	}

	return s.store.ListUsers(ctx, normalized)
}

// GetUser fetches a single user by string id. The id is trimmed and
// validated as a UUID at the service boundary; malformed ids return
// ErrInvalidInput, and a missing row is mapped to ErrNotFound so handlers
// never see store-specific errors.
func (s *Service) GetUser(ctx context.Context, id string) (domain.User, error) {
	if s.store == nil {
		return domain.User{}, fmt.Errorf("users: store is nil")
	}

	parsed, err := pathparam.ParseUUID(id, "id")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return domain.User{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pathparam: invalid uuid: "))
		}
		return domain.User{}, err
	}

	user, err := s.store.GetUserByID(ctx, parsed)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}
	return user, nil
}

func normalizeListUsersInput(input ListUsersInput) (domain.ListUsersInput, error) {
	page, err := pagination.Normalize(pagination.Params{
		Page:     input.Page,
		PageSize: input.PageSize,
		Search:   input.Search,
	})
	if err != nil {
		if errors.Is(err, pagination.ErrInvalidPagination) {
			return domain.ListUsersInput{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pagination: invalid input: "))
		}
		return domain.ListUsersInput{}, err
	}

	role, err := normalizeRole(input.Role)
	if err != nil {
		return domain.ListUsersInput{}, err
	}
	status, err := normalizeStatus(input.Status)
	if err != nil {
		return domain.ListUsersInput{}, err
	}

	return domain.ListUsersInput{
		Page:     page.Page,
		PageSize: page.PageSize,
		Offset:   page.Offset,
		Search:   page.Search,
		Role:     role,
		Status:   status,
	}, nil
}

func normalizeRole(value string) (domain.UserRole, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case string(domain.UserRoleAdmin):
		return domain.UserRoleAdmin, nil
	case string(domain.UserRoleUser):
		return domain.UserRoleUser, nil
	default:
		return "", fmt.Errorf("%w: role must be admin or user", ErrInvalidInput)
	}
}

func normalizeStatus(value string) (domain.UserStatus, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case string(domain.UserStatusActive):
		return domain.UserStatusActive, nil
	case string(domain.UserStatusDisabled):
		return domain.UserStatusDisabled, nil
	default:
		return "", fmt.Errorf("%w: status must be active or disabled", ErrInvalidInput)
	}
}
