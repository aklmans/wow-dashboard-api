// Package service implements role management use cases.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/auth/rbac"
	"github.com/aklmans/wow-dashboard-api/internal/http/pathparam"
	"github.com/aklmans/wow-dashboard-api/internal/roles/domain"
)

var (
	ErrInvalidInput = errors.New("roles: invalid input")
	ErrNotFound     = errors.New("roles: not found")
	ErrNameConflict = errors.New("roles: name conflict")
	ErrRoleInUse    = errors.New("roles: role in use")
	// ErrSystemRole is returned when a caller tries to modify or delete one of
	// the built-in system roles, which are immutable through the API.
	ErrSystemRole = errors.New("roles: system role is immutable")
)

const (
	maxRoleNameLength        = 50
	maxRoleDescriptionLength = 280
)

// RoleStore is the persistence port for role management.
type RoleStore interface {
	ListRoles(ctx context.Context) ([]domain.Role, error)
	GetRoleByID(ctx context.Context, id uuid.UUID) (domain.Role, error)
	CreateRole(ctx context.Context, input domain.CreateRoleInput) (domain.Role, error)
	UpdateRole(ctx context.Context, input domain.UpdateRoleInput) (domain.Role, error)
	DeleteRole(ctx context.Context, id uuid.UUID) error
}

// CreateRoleInput is the raw create input the handler passes to the service.
type CreateRoleInput struct {
	ActorUserID string
	Name        string
	Description string
	Permissions []string
}

// UpdateRoleInput is the raw update input the handler passes to the service.
// An omitted (nil) field is left unchanged; Permissions, when set, replaces
// the role's entire permission set.
type UpdateRoleInput struct {
	ActorUserID string
	RoleID      string
	Name        *string
	Description *string
	Permissions *[]string
}

type Service struct {
	store         RoleStore
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

func NewService(store RoleStore, opts ...Option) *Service {
	s := &Service{store: store, auditRecorder: noopAuditRecorder{}, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListRoles returns every role with its permissions and assigned-user count.
func (s *Service) ListRoles(ctx context.Context) ([]domain.Role, error) {
	if s.store == nil {
		return nil, fmt.Errorf("roles: store is nil")
	}
	return s.store.ListRoles(ctx)
}

// GetRole returns a single role by string id.
func (s *Service) GetRole(ctx context.Context, id string) (domain.Role, error) {
	if s.store == nil {
		return domain.Role{}, fmt.Errorf("roles: store is nil")
	}
	roleID, err := parseRoleID(id)
	if err != nil {
		return domain.Role{}, err
	}
	role, err := s.store.GetRoleByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			return domain.Role{}, ErrNotFound
		}
		return domain.Role{}, err
	}
	return role, nil
}

// CreateRole creates a custom role. The name must be unique and every
// permission must belong to the rbac catalog.
func (s *Service) CreateRole(ctx context.Context, input CreateRoleInput) (domain.Role, error) {
	if s.store == nil {
		return domain.Role{}, fmt.Errorf("roles: store is nil")
	}

	name, err := normalizeName(input.Name)
	if err != nil {
		return domain.Role{}, err
	}
	description, err := normalizeDescription(input.Description)
	if err != nil {
		return domain.Role{}, err
	}
	permissions, err := normalizePermissions(input.Permissions)
	if err != nil {
		return domain.Role{}, err
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	role, err := s.store.CreateRole(ctx, domain.CreateRoleInput{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Permissions: permissions,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNameConflict) {
			return domain.Role{}, ErrNameConflict
		}
		return domain.Role{}, err
	}

	s.recordRoleEvent(ctx, EventRoleCreated, "Role created.", role, input.ActorUserID)
	return role, nil
}

// UpdateRole updates a custom role's name, description, and/or permission set.
// System roles are immutable and surface as ErrSystemRole.
func (s *Service) UpdateRole(ctx context.Context, input UpdateRoleInput) (domain.Role, error) {
	if s.store == nil {
		return domain.Role{}, fmt.Errorf("roles: store is nil")
	}

	roleID, err := parseRoleID(input.RoleID)
	if err != nil {
		return domain.Role{}, err
	}
	if input.Name == nil && input.Description == nil && input.Permissions == nil {
		return domain.Role{}, fmt.Errorf("%w: at least one of name, description, or permissions must be provided", ErrInvalidInput)
	}

	existing, err := s.store.GetRoleByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			return domain.Role{}, ErrNotFound
		}
		return domain.Role{}, err
	}
	if existing.IsSystem {
		return domain.Role{}, ErrSystemRole
	}

	update := domain.UpdateRoleInput{ID: roleID, UpdatedAt: s.now().UTC().Truncate(time.Microsecond)}
	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return domain.Role{}, err
		}
		update.Name = &name
	}
	if input.Description != nil {
		description, err := normalizeDescription(*input.Description)
		if err != nil {
			return domain.Role{}, err
		}
		update.Description = &description
	}
	if input.Permissions != nil {
		permissions, err := normalizePermissions(*input.Permissions)
		if err != nil {
			return domain.Role{}, err
		}
		update.Permissions = &permissions
	}

	role, err := s.store.UpdateRole(ctx, update)
	if err != nil {
		if errors.Is(err, domain.ErrNameConflict) {
			return domain.Role{}, ErrNameConflict
		}
		if errors.Is(err, domain.ErrRoleNotFound) {
			return domain.Role{}, ErrNotFound
		}
		return domain.Role{}, err
	}

	s.recordRoleEvent(ctx, EventRoleUpdated, "Role updated.", role, input.ActorUserID)
	return role, nil
}

// DeleteRole deletes a custom role. System roles cannot be deleted, nor can a
// role still assigned to any user.
func (s *Service) DeleteRole(ctx context.Context, actorUserID string, id string) error {
	if s.store == nil {
		return fmt.Errorf("roles: store is nil")
	}

	roleID, err := parseRoleID(id)
	if err != nil {
		return err
	}

	existing, err := s.store.GetRoleByID(ctx, roleID)
	if err != nil {
		if errors.Is(err, domain.ErrRoleNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.IsSystem {
		return ErrSystemRole
	}
	if existing.UserCount > 0 {
		return ErrRoleInUse
	}

	if err := s.store.DeleteRole(ctx, roleID); err != nil {
		if errors.Is(err, domain.ErrRoleInUse) {
			return ErrRoleInUse
		}
		return err
	}

	s.recordRoleEvent(ctx, EventRoleDeleted, "Role deleted.", existing, actorUserID)
	return nil
}

func parseRoleID(value string) (uuid.UUID, error) {
	parsed, err := pathparam.ParseUUID(value, "id")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidInput, pathparam.Detail(err))
		}
		return uuid.Nil, err
	}
	return parsed, nil
}

func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: role name is required", ErrInvalidInput)
	}
	if utf8.RuneCountInString(value) > maxRoleNameLength {
		return "", fmt.Errorf("%w: role name must not exceed %d characters", ErrInvalidInput, maxRoleNameLength)
	}
	return value, nil
}

func normalizeDescription(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxRoleDescriptionLength {
		return "", fmt.Errorf("%w: description must not exceed %d characters", ErrInvalidInput, maxRoleDescriptionLength)
	}
	return value, nil
}

// normalizePermissions trims, validates, and de-duplicates a permission set.
// Every entry must be an assignable catalog permission — the "*" wildcard and
// unknown strings are rejected.
func normalizePermissions(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if !rbac.IsAssignable(rbac.Permission(p)) {
			return nil, fmt.Errorf("%w: %q is not an assignable permission", ErrInvalidInput, p)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}
