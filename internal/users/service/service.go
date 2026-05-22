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
	// own roles or status. Disallowing self-changes guarantees the system
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
	SetUserStatus(ctx context.Context, input domain.SetUserStatusInput) error
	ReplaceUserRoles(ctx context.Context, input domain.ReplaceUserRolesInput) error
}

// UpdateUserInput is the raw admin update input the handler passes to the
// service. Status and RoleIDs are pointers so an omitted field stays
// unchanged; at least one must be provided. RoleIDs, when provided, replaces
// the user's entire role set.
type UpdateUserInput struct {
	ActorUserID  string
	TargetUserID string
	Status       *string
	RoleIDs      *[]string
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

// GetUser fetches a single user, including their role names, by string id.
// Malformed ids return ErrInvalidInput; a missing row is mapped to ErrNotFound.
func (s *Service) GetUser(ctx context.Context, id string) (domain.User, error) {
	if s.store == nil {
		return domain.User{}, fmt.Errorf("users: store is nil")
	}

	parsed, err := parseUserID(id, "id")
	if err != nil {
		return domain.User{}, err
	}
	return s.fetchUser(ctx, parsed)
}

// UpdateUser applies an admin status change and/or role-set replacement to
// another user. The acting admin cannot target their own account
// (ErrSelfModification). At least one of status or roleIds must be provided;
// a missing target surfaces as ErrNotFound. A successful update records a
// best-effort users.user.updated audit event.
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
	if input.Status == nil && input.RoleIDs == nil {
		return domain.User{}, fmt.Errorf("%w: at least one of status or roleIds must be provided", ErrInvalidInput)
	}

	// Confirm the target exists up front so a roles-only update on a missing
	// user reports ErrNotFound rather than a foreign-key failure.
	if _, err := s.fetchUser(ctx, targetID); err != nil {
		return domain.User{}, err
	}

	var changedFields []string
	var auditRoleIDs []string

	if input.Status != nil {
		status, err := normalizeStatus(*input.Status)
		if err != nil {
			return domain.User{}, err
		}
		if err := s.store.SetUserStatus(ctx, domain.SetUserStatusInput{
			ID:        targetID,
			Status:    status,
			UpdatedAt: s.now().UTC().Truncate(time.Microsecond),
		}); err != nil {
			if errors.Is(err, domain.ErrUserNotFound) {
				return domain.User{}, ErrNotFound
			}
			return domain.User{}, err
		}
		changedFields = append(changedFields, "status")
	}

	if input.RoleIDs != nil {
		roleIDs, err := parseRoleIDs(*input.RoleIDs)
		if err != nil {
			return domain.User{}, err
		}
		if err := s.store.ReplaceUserRoles(ctx, domain.ReplaceUserRolesInput{
			UserID:  targetID,
			RoleIDs: roleIDs,
		}); err != nil {
			if errors.Is(err, domain.ErrRoleNotFound) {
				return domain.User{}, fmt.Errorf("%w: one or more role ids do not exist", ErrInvalidInput)
			}
			return domain.User{}, err
		}
		changedFields = append(changedFields, "roles")
		auditRoleIDs = make([]string, len(roleIDs))
		for i, id := range roleIDs {
			auditRoleIDs[i] = id.String()
		}
	}

	user, err := s.fetchUser(ctx, targetID)
	if err != nil {
		return domain.User{}, err
	}

	s.recordUserUpdated(ctx, AuditMetadata{
		TargetUserID:  targetID.String(),
		ActorUserID:   actorID.String(),
		ChangedFields: changedFields,
		Status:        string(user.Status),
		RoleIDs:       auditRoleIDs,
	})
	return user, nil
}

func (s *Service) fetchUser(ctx context.Context, id uuid.UUID) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}
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

// parseRoleIDs parses, de-duplicates, and validates the role ids for a role
// replacement. A user must always retain at least one role.
func parseRoleIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: at least one role id is required", ErrInvalidInput)
	}
	seen := make(map[uuid.UUID]struct{}, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, err := uuid.Parse(strings.TrimSpace(r))
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a valid role id", ErrInvalidInput, r)
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
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

	status, err := normalizeStatusFilter(input.Status)
	if err != nil {
		return domain.ListUsersInput{}, err
	}

	return domain.ListUsersInput{
		Page:     page.Page,
		PageSize: page.PageSize,
		Offset:   page.Offset,
		Search:   page.Search,
		// The role filter is a free-form role name; an unknown name simply
		// matches no users, so it needs no enum validation.
		Role:   strings.TrimSpace(input.Role),
		Status: status,
	}, nil
}

// normalizeStatus rejects an empty or unknown status; callers invoke it only
// when a status value is explicitly provided.
func normalizeStatus(value string) (domain.UserStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(domain.UserStatusActive):
		return domain.UserStatusActive, nil
	case string(domain.UserStatusDisabled):
		return domain.UserStatusDisabled, nil
	default:
		return "", fmt.Errorf("%w: status must be active or disabled", ErrInvalidInput)
	}
}

// normalizeStatusFilter allows an empty value (no filter) in addition to the
// valid statuses.
func normalizeStatusFilter(value string) (domain.UserStatus, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return normalizeStatus(value)
}
