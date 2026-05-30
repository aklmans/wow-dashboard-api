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
	// ErrForbidden is returned when the requester has some access to a
	// project but not enough for the attempted operation — e.g. a viewer
	// trying to edit, or a non-owner trying to archive or manage members.
	ErrForbidden = errors.New("projects: forbidden")
	// ErrMemberConflict is returned when granting access to a user who is
	// already a member or is the project owner.
	ErrMemberConflict = errors.New("projects: member conflict")
)

const (
	maxNameLen        = 120
	maxDescriptionLen = 2000
)

// ListProjectsInput is the raw input the handler passes to the service.
// UserID is the requesting user; the result includes projects they own or
// are a member of.
type ListProjectsInput struct {
	UserID   string
	Page     int
	PageSize int
	Search   string
	Status   string
}

// CreateProjectInput is the raw input the handler passes to the service.
type CreateProjectInput struct {
	OwnerUserID string
	Name        string
	Description string
	Status      string
}

// UpdateProjectInput is the raw partial-update input the handler passes to
// the service. UserID is the requesting user (owner or editor). Pointer
// fields distinguish "not provided" (nil) from "provided" (non-nil, including
// an empty Description string used to clear the column).
type UpdateProjectInput struct {
	UserID      string
	ID          string
	Name        *string
	Description *string
	Status      *string
}

// ProjectStore is the persistence port the service requires.
type ProjectStore interface {
	CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.Project, error)
	ListProjects(ctx context.Context, input domain.ListProjectsInput) (domain.ListProjectsResult, error)
	GetProjectWithAccess(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) (domain.ProjectAccess, error)
	UpdateProject(ctx context.Context, input domain.UpdateProjectInput) (domain.Project, error)
	ArchiveProject(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID, updatedAt time.Time) (domain.Project, error)
	AddProjectMember(ctx context.Context, input domain.AddProjectMemberInput) (domain.ProjectMember, error)
	ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]domain.ProjectMemberDetail, error)
	GetProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) (domain.ProjectMember, error)
	UpdateProjectMemberRole(ctx context.Context, input domain.UpdateProjectMemberRoleInput) (domain.ProjectMember, error)
	RemoveProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) error
	FindUserByEmail(ctx context.Context, email string) (uuid.UUID, error)
}

// ProjectMutator is the transactional subset of project store operations a
// unit of work exposes. It runs on the unit of work's transaction, not the pool.
type ProjectMutator interface {
	CreateProject(ctx context.Context, input domain.CreateProjectInput) (domain.Project, error)
	UpdateProject(ctx context.Context, input domain.UpdateProjectInput) (domain.Project, error)
	ArchiveProject(ctx context.Context, ownerUserID uuid.UUID, id uuid.UUID, updatedAt time.Time) (domain.Project, error)
	AddProjectMember(ctx context.Context, input domain.AddProjectMemberInput) (domain.ProjectMember, error)
	UpdateProjectMemberRole(ctx context.Context, input domain.UpdateProjectMemberRoleInput) (domain.ProjectMember, error)
	RemoveProjectMember(ctx context.Context, projectID uuid.UUID, userID uuid.UUID) error
}

// WorkDeps contains transaction-scoped dependencies for a unit of work. The
// mutator and the audit recorder share one transaction, so a mutation and its
// audit event commit or roll back together.
type WorkDeps struct {
	Projects ProjectMutator
	Audit    AuditRecorder
}

// UnitOfWork runs fn inside a single database transaction. When configured, it
// makes project mutations and their audit events atomic.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context, WorkDeps) error) error
}

// Service orchestrates the project use cases.
type Service struct {
	store         ProjectStore
	now           func() time.Time
	auditRecorder AuditRecorder
	unitOfWork    UnitOfWork
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

// WithUnitOfWork configures transactional project mutations. When set, each
// mutation records its audit event in the same transaction as the mutation, so
// the two commit or roll back together. Without it, the service falls back to a
// best-effort audit write after the mutation commits.
func WithUnitOfWork(uow UnitOfWork) Option {
	return func(s *Service) {
		if uow != nil {
			s.unitOfWork = uow
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

	userID, err := parseUserID(input.UserID)
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
			return domain.ListProjectsResult{}, fmt.Errorf("%w: %s", ErrInvalidInput, pagination.Detail(err))
		}
		return domain.ListProjectsResult{}, err
	}

	status, err := normalizeStatusFilter(input.Status)
	if err != nil {
		return domain.ListProjectsResult{}, err
	}

	return s.store.ListProjects(ctx, domain.ListProjectsInput{
		UserID:   userID,
		Page:     page.Page,
		PageSize: page.PageSize,
		Offset:   page.Offset,
		Search:   page.Search,
		Status:   status,
	})
}

// GetProject fetches a project the current user owns or is a member of.
// Malformed ids surface as ErrInvalidInput; a project the user cannot access
// surfaces as ErrNotFound, identical to a genuinely missing project.
func (s *Service) GetProject(ctx context.Context, userID string, id string) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	parsedUser, err := parseUserID(userID)
	if err != nil {
		return domain.Project{}, err
	}
	parsedID, err := parseProjectID(id)
	if err != nil {
		return domain.Project{}, err
	}

	access, err := s.projectAccess(ctx, parsedUser, parsedID)
	if err != nil {
		return domain.Project{}, err
	}
	return access.Project, nil
}

// projectAccess loads the requesting user's access to a project. A project the
// user cannot access (or one that does not exist) is mapped to ErrNotFound so
// callers never leak a foreign project's existence.
func (s *Service) projectAccess(ctx context.Context, userID uuid.UUID, projectID uuid.UUID) (domain.ProjectAccess, error) {
	access, err := s.store.GetProjectWithAccess(ctx, projectID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return domain.ProjectAccess{}, ErrNotFound
		}
		return domain.ProjectAccess{}, err
	}
	return access, nil
}

// CreateProject validates and inserts a new project for the current user.
func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	ownerID, err := parseUserID(input.OwnerUserID)
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
	createInput := domain.CreateProjectInput{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Status:      status,
		OwnerUserID: ownerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mapErr := func(err error) error {
		if errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			return ErrNameConflict
		}
		return err
	}
	auditMeta := func(project domain.Project) AuditMetadata {
		return AuditMetadata{
			ProjectID:   project.ID.String(),
			OwnerUserID: project.OwnerUserID.String(),
			Status:      string(project.Status),
		}
	}

	if s.unitOfWork != nil {
		var result domain.Project
		err := s.unitOfWork.Do(ctx, func(ctx context.Context, deps WorkDeps) error {
			project, err := deps.Projects.CreateProject(ctx, createInput)
			if err != nil {
				return err
			}
			result = project
			return recordProjectEventTx(ctx, deps.Audit, EventProjectCreated, "Project created.", auditMeta(project))
		})
		if err != nil {
			return domain.Project{}, mapErr(err)
		}
		return result, nil
	}

	project, err := s.store.CreateProject(ctx, createInput)
	if err != nil {
		return domain.Project{}, mapErr(err)
	}
	s.recordProjectCreated(ctx, auditMeta(project))
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

	userID, err := parseUserID(input.UserID)
	if err != nil {
		return domain.Project{}, err
	}

	parsedID, err := parseProjectID(input.ID)
	if err != nil {
		return domain.Project{}, err
	}

	if input.Name == nil && input.Description == nil && input.Status == nil {
		return domain.Project{}, fmt.Errorf("%w: at least one of name, description, or status must be provided", ErrInvalidInput)
	}

	access, err := s.projectAccess(ctx, userID, parsedID)
	if err != nil {
		return domain.Project{}, err
	}
	if !access.AccessRole.CanEdit() {
		return domain.Project{}, ErrForbidden
	}

	update := domain.UpdateProjectInput{
		ID:        parsedID,
		UpdatedAt: s.now().UTC().Truncate(time.Microsecond),
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

	mapErr := func(err error) error {
		if errors.Is(err, domain.ErrProjectNameAlreadyExists) {
			return ErrNameConflict
		}
		if errors.Is(err, domain.ErrProjectNotFound) {
			return ErrNotFound
		}
		return err
	}
	auditMeta := func(project domain.Project) AuditMetadata {
		return AuditMetadata{
			ProjectID:     project.ID.String(),
			OwnerUserID:   project.OwnerUserID.String(),
			Status:        string(project.Status),
			ChangedFields: changedFields,
		}
	}

	if s.unitOfWork != nil {
		var result domain.Project
		err := s.unitOfWork.Do(ctx, func(ctx context.Context, deps WorkDeps) error {
			project, err := deps.Projects.UpdateProject(ctx, update)
			if err != nil {
				return err
			}
			result = project
			return recordProjectEventTx(ctx, deps.Audit, EventProjectUpdated, "Project updated.", auditMeta(project))
		})
		if err != nil {
			return domain.Project{}, mapErr(err)
		}
		return result, nil
	}

	project, err := s.store.UpdateProject(ctx, update)
	if err != nil {
		return domain.Project{}, mapErr(err)
	}
	s.recordProjectUpdated(ctx, auditMeta(project))
	return project, nil
}

// ArchiveProject archives a project. Only the project owner may archive; a
// non-owner member receives ErrForbidden. It is idempotent — archiving an
// already-archived row still succeeds and refreshes updated_at. A project the
// user cannot access surfaces as ErrNotFound.
func (s *Service) ArchiveProject(ctx context.Context, userID string, id string) (domain.Project, error) {
	if s.store == nil {
		return domain.Project{}, fmt.Errorf("projects: store is nil")
	}

	parsedUser, err := parseUserID(userID)
	if err != nil {
		return domain.Project{}, err
	}

	parsedID, err := parseProjectID(id)
	if err != nil {
		return domain.Project{}, err
	}

	access, err := s.projectAccess(ctx, parsedUser, parsedID)
	if err != nil {
		return domain.Project{}, err
	}
	if access.AccessRole != domain.AccessRoleOwner {
		return domain.Project{}, ErrForbidden
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	mapErr := func(err error) error {
		if errors.Is(err, domain.ErrProjectNotFound) {
			return ErrNotFound
		}
		return err
	}
	auditMeta := func(project domain.Project) AuditMetadata {
		return AuditMetadata{
			ProjectID:   project.ID.String(),
			OwnerUserID: project.OwnerUserID.String(),
			Status:      string(project.Status),
		}
	}

	if s.unitOfWork != nil {
		var result domain.Project
		err := s.unitOfWork.Do(ctx, func(ctx context.Context, deps WorkDeps) error {
			project, err := deps.Projects.ArchiveProject(ctx, parsedUser, parsedID, now)
			if err != nil {
				return err
			}
			result = project
			return recordProjectEventTx(ctx, deps.Audit, EventProjectArchived, "Project archived.", auditMeta(project))
		})
		if err != nil {
			return domain.Project{}, mapErr(err)
		}
		return result, nil
	}

	project, err := s.store.ArchiveProject(ctx, parsedUser, parsedID, now)
	if err != nil {
		return domain.Project{}, mapErr(err)
	}
	s.recordProjectArchived(ctx, auditMeta(project))
	return project, nil
}

func parseUserID(value string) (uuid.UUID, error) {
	return parseUUIDField(value, "userId")
}

func parseProjectID(value string) (uuid.UUID, error) {
	return parseUUIDField(value, "id")
}

func parseUUIDField(value string, field string) (uuid.UUID, error) {
	parsed, err := pathparam.ParseUUID(value, field)
	if err != nil {
		if errors.Is(err, pathparam.ErrInvalidUUID) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrInvalidInput, pathparam.Detail(err))
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
