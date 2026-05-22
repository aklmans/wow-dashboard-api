package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
)

// AddMemberInput is the raw input for granting a user access to a project.
type AddMemberInput struct {
	RequestingUserID string
	ProjectID        string
	Email            string
	Role             string
}

// UpdateMemberRoleInput is the raw input for changing a member's role.
type UpdateMemberRoleInput struct {
	RequestingUserID string
	ProjectID        string
	TargetUserID     string
	Role             string
}

// ListMembers returns the non-owner members of a project. Any user with access
// to the project (owner, editor, or viewer) may view its members.
func (s *Service) ListMembers(ctx context.Context, userID string, projectID string) ([]domain.ProjectMemberDetail, error) {
	if s.store == nil {
		return nil, fmt.Errorf("projects: store is nil")
	}

	parsedUser, err := parseUserID(userID)
	if err != nil {
		return nil, err
	}
	parsedProject, err := parseProjectID(projectID)
	if err != nil {
		return nil, err
	}

	if _, err := s.projectAccess(ctx, parsedUser, parsedProject); err != nil {
		return nil, err
	}
	return s.store.ListProjectMembers(ctx, parsedProject)
}

// AddMember grants a user, identified by email, access to a project. Only the
// project owner may add members. The owner cannot be added as a member, and a
// user who already has access surfaces as ErrMemberConflict.
func (s *Service) AddMember(ctx context.Context, input AddMemberInput) (domain.ProjectMember, error) {
	if s.store == nil {
		return domain.ProjectMember{}, fmt.Errorf("projects: store is nil")
	}

	access, err := s.requireOwner(ctx, input.RequestingUserID, input.ProjectID)
	if err != nil {
		return domain.ProjectMember{}, err
	}

	role, err := normalizeProjectRole(input.Role)
	if err != nil {
		return domain.ProjectMember{}, err
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	if email == "" {
		return domain.ProjectMember{}, fmt.Errorf("%w: email is required", ErrInvalidInput)
	}

	targetID, err := s.store.FindUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrMemberUserNotFound) {
			return domain.ProjectMember{}, fmt.Errorf("%w: no user found with that email address", ErrInvalidInput)
		}
		return domain.ProjectMember{}, err
	}
	if targetID == access.Project.OwnerUserID {
		return domain.ProjectMember{}, fmt.Errorf("%w: the project owner already has full access", ErrMemberConflict)
	}

	now := s.now().UTC().Truncate(time.Microsecond)
	member, err := s.store.AddProjectMember(ctx, domain.AddProjectMemberInput{
		ProjectID: access.Project.ID,
		UserID:    targetID,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		if errors.Is(err, domain.ErrMemberAlreadyExists) {
			return domain.ProjectMember{}, ErrMemberConflict
		}
		return domain.ProjectMember{}, err
	}

	s.recordMemberAdded(ctx, AuditMetadata{
		ProjectID:    access.Project.ID.String(),
		OwnerUserID:  access.Project.OwnerUserID.String(),
		TargetUserID: targetID.String(),
		Role:         string(role),
	})
	return member, nil
}

// UpdateMemberRole changes an existing member's role. Only the project owner
// may change member roles. A user who is not a member surfaces as ErrNotFound.
func (s *Service) UpdateMemberRole(ctx context.Context, input UpdateMemberRoleInput) (domain.ProjectMember, error) {
	if s.store == nil {
		return domain.ProjectMember{}, fmt.Errorf("projects: store is nil")
	}

	access, err := s.requireOwner(ctx, input.RequestingUserID, input.ProjectID)
	if err != nil {
		return domain.ProjectMember{}, err
	}

	targetID, err := parseUUIDField(input.TargetUserID, "userId")
	if err != nil {
		return domain.ProjectMember{}, err
	}

	role, err := normalizeProjectRole(input.Role)
	if err != nil {
		return domain.ProjectMember{}, err
	}

	member, err := s.store.UpdateProjectMemberRole(ctx, domain.UpdateProjectMemberRoleInput{
		ProjectID: access.Project.ID,
		UserID:    targetID,
		Role:      role,
		UpdatedAt: s.now().UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		if errors.Is(err, domain.ErrProjectMemberNotFound) {
			return domain.ProjectMember{}, ErrNotFound
		}
		return domain.ProjectMember{}, err
	}

	s.recordMemberRoleChanged(ctx, AuditMetadata{
		ProjectID:    access.Project.ID.String(),
		OwnerUserID:  access.Project.OwnerUserID.String(),
		TargetUserID: targetID.String(),
		Role:         string(role),
	})
	return member, nil
}

// RemoveMember revokes a user's access to a project. Only the project owner
// may remove members. A user who is not a member surfaces as ErrNotFound.
func (s *Service) RemoveMember(ctx context.Context, requestingUserID string, projectID string, targetUserID string) error {
	if s.store == nil {
		return fmt.Errorf("projects: store is nil")
	}

	access, err := s.requireOwner(ctx, requestingUserID, projectID)
	if err != nil {
		return err
	}

	targetID, err := parseUUIDField(targetUserID, "userId")
	if err != nil {
		return err
	}

	if err := s.store.RemoveProjectMember(ctx, access.Project.ID, targetID); err != nil {
		if errors.Is(err, domain.ErrProjectMemberNotFound) {
			return ErrNotFound
		}
		return err
	}

	s.recordMemberRemoved(ctx, AuditMetadata{
		ProjectID:    access.Project.ID.String(),
		OwnerUserID:  access.Project.OwnerUserID.String(),
		TargetUserID: targetID.String(),
	})
	return nil
}

// requireOwner parses the requester and project ids, loads the requester's
// access, and confirms they own the project. A project the requester cannot
// access is ErrNotFound; access without ownership is ErrForbidden.
func (s *Service) requireOwner(ctx context.Context, requestingUserID string, projectID string) (domain.ProjectAccess, error) {
	parsedUser, err := parseUserID(requestingUserID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	parsedProject, err := parseProjectID(projectID)
	if err != nil {
		return domain.ProjectAccess{}, err
	}

	access, err := s.projectAccess(ctx, parsedUser, parsedProject)
	if err != nil {
		return domain.ProjectAccess{}, err
	}
	if access.AccessRole != domain.AccessRoleOwner {
		return domain.ProjectAccess{}, ErrForbidden
	}
	return access, nil
}

func normalizeProjectRole(value string) (domain.ProjectRole, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(domain.ProjectRoleViewer):
		return domain.ProjectRoleViewer, nil
	case string(domain.ProjectRoleEditor):
		return domain.ProjectRoleEditor, nil
	default:
		return "", fmt.Errorf("%w: role must be viewer or editor", ErrInvalidInput)
	}
}
