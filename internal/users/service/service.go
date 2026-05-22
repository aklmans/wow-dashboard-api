// Package service implements user management use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/http/pagination"
	"github.com/aklmans/wow-dashboard-api/internal/http/pathparam"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
)

var (
	ErrInvalidInput = errors.New("users: invalid input")
	ErrNotFound     = errors.New("users: not found")
	// ErrSelfModification is returned when an admin attempts to change their
	// own role or status. Disallowing self-changes guarantees the system
	// always retains at least one admin and prevents accidental self-lockout.
	ErrSelfModification = errors.New("users: self modification not allowed")
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
	UpdateUser(ctx context.Context, input domain.UpdateUserInput) (domain.User, error)
}

// UpdateUserInput is the raw admin update input the handler passes to the
// service. Role and Status are pointers so an omitted field stays unchanged;
// at least one must be provided.
type UpdateUserInput struct {
	ActorUserID  string
	TargetUserID string
	Role         *string
	Status       *string
}

type Service struct {
	store         UserStore
	auditRecorder AuditRecorder
	now           func() time.Time
}

// Option configures Service dependencies.
type Option func(*Service)

// WithClock overrides the service clock for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func NewService(store UserStore, opts ...Option) *Service {
	s := &Service{store: store, auditRecorder: noopAuditRecorder{}, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
			return domain.User{}, fmt.Errorf("%w: %s", ErrInvalidInput, pathparam.Detail(err))
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

// UpdateUser applies an admin role/status change to another user. The acting
// admin cannot target their own account (ErrSelfModification). At least one of
// role or status must be provided; a missing target surfaces as ErrNotFound.
// A successful update records a best-effort users.user.updated audit event.
func (s *Service) UpdateUser(ctx context.Context, input UpdateUserInput) (domain.User, error) {
	if s.store == nil {
		return domain.User{}, fmt.Errorf("users: store is nil")
	}

	actorID, err := parseUserID(input.ActorUserID, "actorUserId")
	if err != nil {
		return domain.User{}, err
	}
	targetID, err := parseUserID(input.TargetUserID, "id")
	if err != nil {
		return domain.User{}, err
	}
	if actorID == targetID {
		return domain.User{}, ErrSelfModification
	}

	if input.Role == nil && input.Status == nil {
		return domain.User{}, fmt.Errorf("%w: at least one of role or status must be provided", ErrInvalidInput)
	}

	update := domain.UpdateUserInput{ID: targetID}
	var changedFields []string

	if input.Role != nil {
		role, err := normalizeRoleForUpdate(*input.Role)
		if err != nil {
			return domain.User{}, err
		}
		update.Role = &role
		changedFields = append(changedFields, "role")
	}
	if input.Status != nil {
		status, err := normalizeStatusForUpdate(*input.Status)
		if err != nil {
			return domain.User{}, err
		}
		update.Status = &status
		changedFields = append(changedFields, "status")
	}
	update.UpdatedAt = s.now().UTC().Truncate(time.Microsecond)

	user, err := s.store.UpdateUser(ctx, update)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}

	s.recordUserUpdated(ctx, AuditMetadata{
		TargetUserID:  user.ID.String(),
		ActorUserID:   actorID.String(),
		ChangedFields: changedFields,
		Role:          string(user.Role),
		Status:        string(user.Status),
	})
	return user, nil
}

func parseUserID(value string, field string) (uuid.UUID, error) {
	parsed, err := pathparam.ParseUUID(value, field)
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidInput, pathparam.Detail(err))
		}
		return uuid.Nil, err
	}
	return parsed, nil
}

func normalizeListUsersInput(input ListUsersInput) (domain.ListUsersInput, error) {
	page, err := pagination.Normalize(pagination.Params{
		Page:     input.Page,
		PageSize: input.PageSize,
		Search:   input.Search,
	})
	if err != nil {
		if errors.Is(err, pagination.ErrInvalidPagination) {
			return domain.ListUsersInput{}, fmt.Errorf("%w: %s", ErrInvalidInput, pagination.Detail(err))
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

// normalizeRoleForUpdate rejects an empty value; the caller only invokes it
// when the update body explicitly provides a role field.
func normalizeRoleForUpdate(value string) (domain.UserRole, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(domain.UserRoleAdmin):
		return domain.UserRoleAdmin, nil
	case string(domain.UserRoleUser):
		return domain.UserRoleUser, nil
	default:
		return "", fmt.Errorf("%w: role must be admin or user", ErrInvalidInput)
	}
}

// normalizeStatusForUpdate rejects an empty value; the caller only invokes it
// when the update body explicitly provides a status field.
func normalizeStatusForUpdate(value string) (domain.UserStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(domain.UserStatusActive):
		return domain.UserStatusActive, nil
	case string(domain.UserStatusDisabled):
		return domain.UserStatusDisabled, nil
	default:
		return "", fmt.Errorf("%w: status must be active or disabled", ErrInvalidInput)
	}
}
