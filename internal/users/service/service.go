// Package service implements user management use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	"github.com/aklmans/wow-dashboard-api/internal/http/pagination"
	"github.com/aklmans/wow-dashboard-api/internal/http/pathparam"
	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
)

var (
	ErrInvalidInput          = errors.New("users: invalid input")
	ErrNotFound              = errors.New("users: not found")
	ErrInsufficientPrivilege = errors.New("users: insufficient privilege")
	// ErrSelfModification is returned when an admin attempts to change their
	// own roles or status. Disallowing self-changes guarantees the system
	// always retains at least one admin and prevents accidental self-lockout.
	ErrSelfModification = errors.New("users: self modification not allowed")
)

var systemAdminRoleID = uuid.MustParse("00000000-0000-0000-0000-0000000a0001")

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

// UserMutator is the transactional subset of user store operations a unit of
// work exposes. It runs on the unit of work's transaction, not the pool.
type UserMutator interface {
	UpdateUser(ctx context.Context, input domain.UpdateUserInput) (domain.User, error)
}

// WorkDeps contains transaction-scoped dependencies for a unit of work. The
// mutator and the audit recorder share one transaction, so a mutation and its
// audit event commit or roll back together.
type WorkDeps struct {
	Users UserMutator
	Audit AuditRecorder
}

// UnitOfWork runs fn inside a single database transaction. When configured, it
// makes user mutations and their audit events atomic.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context, WorkDeps) error) error
}

// UpdateUserInput is the raw admin update input the handler passes to the
// service. Status and RoleIDs are pointers so an omitted field stays
// unchanged; at least one must be provided. RoleIDs, when provided, replaces
// the user's entire role set.
type UpdateUserInput struct {
	ActorUserID      string
	ActorRoles       []string
	ActorPermissions []string
	TargetUserID     string
	Status           *string
	RoleIDs          *[]string
	AvatarURL        *string
	Phone            *string
	JobTitle         *string
	Company          *string
}

type Service struct {
	store         UserStore
	auditRecorder AuditRecorder
	unitOfWork    UnitOfWork
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

// WithUnitOfWork configures transactional user mutations. When set, UpdateUser
// records its audit event in the same transaction as the mutation, so the two
// commit or roll back together. Without it, the service falls back to a
// best-effort audit write after the mutation commits.
func WithUnitOfWork(uow UnitOfWork) Option {
	return func(s *Service) {
		if uow != nil {
			s.unitOfWork = uow
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
	if input.Status == nil && input.RoleIDs == nil && input.AvatarURL == nil &&
		input.Phone == nil && input.JobTitle == nil && input.Company == nil {
		return domain.User{}, fmt.Errorf("%w: at least one field must be provided", ErrInvalidInput)
	}

	// Normalize and validate every field up front so a malformed input never
	// leaves a partial change behind; the store then applies the update
	// atomically in a single transaction.
	update := domain.UpdateUserInput{ID: targetID, UpdatedAt: s.now().UTC().Truncate(time.Microsecond)}
	var changedFields []string
	var auditRoleIDs []string

	if input.Status != nil {
		status, err := normalizeStatus(*input.Status)
		if err != nil {
			return domain.User{}, err
		}
		update.Status = &status
		changedFields = append(changedFields, "status")
	}

	if input.RoleIDs != nil {
		roleIDs, err := parseRoleIDs(*input.RoleIDs)
		if err != nil {
			return domain.User{}, err
		}
		if slices.Contains(roleIDs, systemAdminRoleID) && !actorCanGrantSystemAdmin(input.ActorRoles, input.ActorPermissions) {
			return domain.User{}, ErrInsufficientPrivilege
		}
		update.RoleIDs = roleIDs
		changedFields = append(changedFields, "roles")
		auditRoleIDs = make([]string, len(roleIDs))
		for i, id := range roleIDs {
			auditRoleIDs[i] = id.String()
		}
	}

	for _, field := range []struct {
		name  string
		raw   *string
		apply func(*string)
	}{
		{"avatarUrl", input.AvatarURL, func(v *string) { update.AvatarURL = v }},
		{"phone", input.Phone, func(v *string) { update.Phone = v }},
		{"jobTitle", input.JobTitle, func(v *string) { update.JobTitle = v }},
		{"company", input.Company, func(v *string) { update.Company = v }},
	} {
		normalized, err := normalizeProfileField(field.name, field.raw)
		if err != nil {
			return domain.User{}, err
		}
		if normalized != nil {
			field.apply(normalized)
			changedFields = append(changedFields, field.name)
		}
	}

	targetUser, err := s.fetchUser(ctx, targetID)
	if err != nil {
		return domain.User{}, err
	}
	if userHasRole(targetUser, "admin") && !actorCanGrantSystemAdmin(input.ActorRoles, input.ActorPermissions) {
		return domain.User{}, ErrInsufficientPrivilege
	}

	metadata := func(user domain.User) AuditMetadata {
		return AuditMetadata{
			TargetUserID:  targetID.String(),
			ActorUserID:   actorID.String(),
			ChangedFields: changedFields,
			Status:        string(user.Status),
			RoleIDs:       auditRoleIDs,
		}
	}

	// Transactional path: the mutation and its audit event share one
	// transaction, so a failed audit write rolls back the update.
	if s.unitOfWork != nil {
		var result domain.User
		err := s.unitOfWork.Do(ctx, func(ctx context.Context, deps WorkDeps) error {
			user, err := deps.Users.UpdateUser(ctx, update)
			if err != nil {
				return err
			}
			result = user
			return deps.Audit.RecordUserEvent(ctx, buildUserUpdatedEvent(ctx, metadata(user)))
		})
		if err != nil {
			return domain.User{}, mapUpdateUserError(err)
		}
		return result, nil
	}

	// Fallback path: the mutation commits in the store, then the audit event is
	// recorded best-effort (a failure is logged but does not fail the update).
	user, err := s.store.UpdateUser(ctx, update)
	if err != nil {
		return domain.User{}, mapUpdateUserError(err)
	}
	s.recordUserUpdated(ctx, metadata(user))
	return user, nil
}

// mapUpdateUserError translates store/domain errors into the service's
// client-facing sentinel errors.
func mapUpdateUserError(err error) error {
	if errors.Is(err, domain.ErrUserNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, domain.ErrRoleNotFound) {
		return fmt.Errorf("%w: one or more role ids do not exist", ErrInvalidInput)
	}
	return err
}

func actorCanGrantSystemAdmin(roles []string, permissions []string) bool {
	if rbac.NewSet(permissions).Has(rbac.PermissionAll) {
		return true
	}
	for _, role := range roles {
		if strings.TrimSpace(role) == "admin" {
			return true
		}
	}
	return false
}

func userHasRole(user domain.User, name string) bool {
	for _, role := range user.Roles {
		if strings.TrimSpace(role) == name {
			return true
		}
	}
	return false
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

// normalizeProfileField trims an optional profile field and enforces a length
// cap. A nil pointer is returned unchanged so an omitted field stays omitted;
// a provided value (including an empty string, which clears the field) is
// trimmed and bounds-checked.
func normalizeProfileField(name string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	const (
		maxProfileFieldLen = 256
		maxAvatarFieldLen  = 256 * 1024 // 256KB — fits a resized inline image
	)
	maxLen := maxProfileFieldLen
	if name == "avatarUrl" {
		maxLen = maxAvatarFieldLen
	}
	trimmed := strings.TrimSpace(*value)
	if len(trimmed) > maxLen {
		return nil, fmt.Errorf("%w: %s must be %d characters or fewer", ErrInvalidInput, name, maxLen)
	}
	return &trimmed, nil
}
