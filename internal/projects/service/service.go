// Package service implements project management use cases.
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
	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
)

var (
	ErrInvalidInput = errors.New("projects: invalid input")
	ErrNameConflict = errors.New("projects: name conflict")
	ErrNotFound     = errors.New("projects: not found")
)

const (
	maxNameLen        = 120
	maxDescriptionLen = 2000
)

// ListProjectsInput is the raw input the handler passes to the service.
type ListProjectsInput struct {
	OwnerUserID string
	Page        int
	PageSize    int
	Search      string
	Status      string
}

// CreateProjectInput is the raw input the handler passes to the service.
type CreateProjectInput struct {
	OwnerUserID string
	Name        string
	Description string
	Status      string
}

// UpdateProjectInput is the raw partial-update input the handler passes to
// the service. Pointer fields distinguish "not provided" (nil) from
// "provided" (non-nil, including an empty Description string used to clear
// the column).
type UpdateProjectInput struct {
	OwnerUserID string
	ID          string
	Name        *string
	Description *string
	Status      *string
}

// ProjectStore is the persistence port the service requires.
type ProjectStore interface {
	CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.Project, error)
	ListProjects(ctx context.Context, input domain.ListProjectsInput) (domain.ListProjectsResult, error)
	GetProjectByID(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID) (domain.Project, error)
	UpdateProject(ctx context.Context, input domain.UpdateProjectInput) (domain.Project, error)
	ArchiveProject(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID, updatedAt time.Time) (domain.Project, error)
}

// Service orchestrates the project use cases.
type Service struct {
	store         ProjectStore
	now           func() time.Time
	auditRecorder AuditRecorder
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

// NewService constructs a Service with sensible defaults.
func NewService(store ProjectStore, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now, auditRecorder: noopAuditRecorder{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ListProjects normalizes pagination/filter inputs and delegates to the store.
func (s *Service) ListProjects(ctx context.Context, input ListProjectsInput) (domain.ListProjectsResult, error) {
	if s.store == nil {
		return domain.ListProjectsResult{}, fmt.Errorf("projects: store is nil")
	}

	ownerID, err := parseOwnerID(input.OwnerUserID)
	if err != nil {
		return domain.ListProjectsResult{}, err
	}

	page, err := pagination.Normalize(pagination.Params{
		Page:     input.Page,
		PageSize: input.PageSize,
		Search:   input.Search,
	})
	if err != nil {
		if errors.Is(err, pagination.ErrInvalidPagination) {
			return domain.ListProjectsResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pagination: invalid input: "))
		}
		return domain.ListProjectsResult{}, err
	}

	status, err := normalizeStatusFilter(input.Status)
	if err != nil {
		return domain.ListProjectsResult{}, err
	}

	return s.store.ListProjects(ctx, domain.ListProjectsInput{
		OwnerUserID: ownerID,
		Page:        page.Page,
		PageSize:    page.PageSize,
		Offset:      page.Offset,
		Search:      page.Search,
		Status:      status,
	})
}

// GetProject fetches a project owned by the current user. Malformed ids
// surface as ErrInvalidInput; missing rows surface as ErrNotFound so the
// handler never sees store-specific sentinels.
func (s *Service) GetProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	ownerID, err := parseOwnerID(ownerUserID)
	if err != nil {
		return domain.Project{}, err
	}

	parsedID, err := pathparam.ParseUUID(id, "id")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return domain.Project{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pathparam: invalid uuid: "))
		}
		return domain.Project{}, err
	}

	project, err := s.store.GetProjectByID(ctx, ownerID, parsedID)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return domain.Project{}, ErrNotFound
		}
		return domain.Project{}, err
	}
	return project, nil
}

// CreateProject validates and inserts a new project for the current user.
func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	ownerID, err := parseOwnerID(input.OwnerUserID)
	if err != nil {
		return domain.Project{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Project{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if len(name) > maxNameLen {
		return domain.Project{}, fmt.Errorf("%w: name must be at most %d characters", ErrInvalidInput, maxNameLen)
	}

	description := strings.TrimSpace(input.Description)
	if len(description) > maxDescriptionLen {
		return domain.Project{}, fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxDescriptionLen)
	}

	status, err := normalizeStatusForCreate(input.Status)
	if err != nil {
		return domain.Project{}, err
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	project, err := s.store.CreateProject(ctx, domain.CreateProjectInput{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Status:      status,
		OwnerUserID: ownerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		if errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			return domain.Project{}, ErrNameConflict
		}
		return domain.Project{}, err
	}

	s.recordProjectCreated(ctx, AuditMetadata{
		ProjectID:   project.ID.String(),
		OwnerUserID: project.OwnerUserID.String(),
		Status:      string(project.Status),
	})
	return project, nil
}

// UpdateProject applies a partial update to a project owned by the current
// user. At least one field must be provided. Nil pointer fields leave the
// corresponding column unchanged; an empty Description pointer clears the
// stored description. Missing rows or rows owned by other users surface as
// ErrNotFound.
func (s *Service) UpdateProject(ctx context.Context, input UpdateProjectInput) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	ownerID, err := parseOwnerID(input.OwnerUserID)
	if err != nil {
		return domain.Project{}, err
	}

	parsedID, err := pathparam.ParseUUID(input.ID, "id")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return domain.Project{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pathparam: invalid uuid: "))
		}
		return domain.Project{}, err
	}

	if input.Name == nil && input.Description == nil && input.Status == nil {
		return domain.Project{}, fmt.Errorf("%w: at least one of name, description, or status must be provided", ErrInvalidInput)
	}

	update := domain.UpdateProjectInput{
		ID:          parsedID,
		OwnerUserID: ownerID,
		UpdatedAt:   s.now().UTC().Truncate(time.Microsecond),
	}

	var changedFields []string

	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return domain.Project{}, fmt.Errorf("%w: name must not be empty", ErrInvalidInput)
		}
		if len(trimmed) > maxNameLen {
			return domain.Project{}, fmt.Errorf("%w: name must be at most %d characters", ErrInvalidInput, maxNameLen)
		}
		update.Name = &trimmed
		changedFields = append(changedFields, "name")
	}

	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		if len(trimmed) > maxDescriptionLen {
			return domain.Project{}, fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxDescriptionLen)
		}
		update.Description = &trimmed
		changedFields = append(changedFields, "description")
	}

	if input.Status != nil {
		status, err := normalizeStatusForUpdate(*input.Status)
		if err != nil {
			return domain.Project{}, err
		}
		update.Status = &status
		changedFields = append(changedFields, "status")
	}

	project, err := s.store.UpdateProject(ctx, update)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			return domain.Project{}, ErrNameConflict
		}
		if errors.Is(err, domain.ErrProjectNotFound) {
			return domain.Project{}, ErrNotFound
		}
		return domain.Project{}, err
	}

	s.recordProjectUpdated(ctx, AuditMetadata{
		ProjectID:     project.ID.String(),
		OwnerUserID:   project.OwnerUserID.String(),
		Status:        string(project.Status),
		ChangedFields: changedFields,
	})
	return project, nil
}

// ArchiveProject archives a project owned by the current user. It is
// idempotent — archiving an already-archived row still succeeds and refreshes
// updated_at. Missing rows or rows owned by other users surface as
// ErrNotFound.
func (s *Service) ArchiveProject(ctx context.Context, ownerUserID string, id string) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	parsedOwner, err := parseOwnerID(ownerUserID)
	if err != nil {
		return domain.Project{}, err
	}

	parsedID, err := pathparam.ParseUUID(id, "id")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return domain.Project{}, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pathparam: invalid uuid: "))
		}
		return domain.Project{}, err
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	project, err := s.store.ArchiveProject(ctx, parsedOwner, parsedID, now)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return domain.Project{}, ErrNotFound
		}
		return domain.Project{}, err
	}

	s.recordProjectArchived(ctx, AuditMetadata{
		ProjectID:   project.ID.String(),
		OwnerUserID: project.OwnerUserID.String(),
		Status:      string(project.Status),
	})
	return project, nil
}

func parseOwnerID(value string) (uuid.UUID, error) {
	parsed, err := pathparam.ParseUUID(value, "ownerUserId")
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidInput, strings.TrimPrefix(err.Error(), "pathparam: invalid uuid: "))
		}
		return uuid.Nil, err
	}
	return parsed, nil
}

func normalizeStatusFilter(value string) (domain.ProjectStatus, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case string(domain.ProjectStatusActive):
		return domain.ProjectStatusActive, nil
	case string(domain.ProjectStatusArchived):
		return domain.ProjectStatusArchived, nil
	default:
		return "", fmt.Errorf("%w: status must be active or archived", ErrInvalidInput)
	}
}

func normalizeStatusForCreate(value string) (domain.ProjectStatus, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return domain.ProjectStatusActive, nil
	case string(domain.ProjectStatusActive):
		return domain.ProjectStatusActive, nil
	case string(domain.ProjectStatusArchived):
		return domain.ProjectStatusArchived, nil
	default:
		return "", fmt.Errorf("%w: status must be active or archived", ErrInvalidInput)
	}
}

// normalizeStatusForUpdate rejects empty status; the caller only invokes
// this when the patch body explicitly provides a status field.
func normalizeStatusForUpdate(value string) (domain.ProjectStatus, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case string(domain.ProjectStatusActive):
		return domain.ProjectStatusActive, nil
	case string(domain.ProjectStatusArchived):
		return domain.ProjectStatusArchived, nil
	default:
		return "", fmt.Errorf("%w: status must be active or archived", ErrInvalidInput)
	}
}
