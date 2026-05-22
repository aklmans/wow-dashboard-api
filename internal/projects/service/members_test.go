package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/projects/domain"
	"github.com/aklmans/wow-dashboard-api/internal/projects/service"
)

func TestServiceAddMemberOwnerSucceeds(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	target := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{
		getAccess:       ownerAccess(domain.Project{ID: project, OwnerUserID: owner}),
		findEmailResult: target,
		addMemberResult: domain.ProjectMember{ProjectID: project, UserID: target, Role: domain.ProjectRoleEditor},
	}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	member, err := svc.AddMember(context.Background(), service.AddMemberInput{
		RequestingUserID: owner.String(),
		ProjectID:        project.String(),
		Email:            "  Teammate@Example.com ",
		Role:             "EDITOR",
	})
	if err != nil {
		t.Fatalf("AddMember error: %v", err)
	}
	if store.findEmailInput != "teammate@example.com" {
		t.Fatalf("email lookup = %q, want normalized", store.findEmailInput)
	}
	if store.addMemberInput.UserID != target || store.addMemberInput.Role != domain.ProjectRoleEditor {
		t.Fatalf("add input = %#v, want target/editor", store.addMemberInput)
	}
	if member.UserID != target {
		t.Fatalf("returned member user = %s, want %s", member.UserID, target)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventMemberAdded {
		t.Fatalf("audit = %#v, want one member.added", recorder.calls)
	}
	if recorder.calls[0].Metadata.TargetUserID != target.String() {
		t.Fatalf("audit target = %q, want %s", recorder.calls[0].Metadata.TargetUserID, target)
	}
}

func TestServiceAddMemberNonOwnerForbidden(t *testing.T) {
	project := uuid.New()
	for _, role := range []domain.AccessRole{domain.AccessRoleEditor, domain.AccessRoleViewer} {
		store := &fakeProjectStore{getAccess: domain.ProjectAccess{
			Project: domain.Project{ID: project}, AccessRole: role,
		}}
		svc := service.NewService(store)
		_, err := svc.AddMember(context.Background(), service.AddMemberInput{
			RequestingUserID: uuid.New().String(),
			ProjectID:        project.String(),
			Email:            "teammate@example.com",
			Role:             "viewer",
		})
		if !errors.Is(err, service.ErrForbidden) {
			t.Fatalf("role %q: err = %v, want ErrForbidden", role, err)
		}
		if store.addMemberCalled {
			t.Fatalf("role %q: store.AddProjectMember was called", role)
		}
	}
}

func TestServiceAddMemberRejectsOwnerEmail(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	store := &fakeProjectStore{
		getAccess:       ownerAccess(domain.Project{ID: project, OwnerUserID: owner}),
		findEmailResult: owner, // the email resolves to the project owner
	}
	svc := service.NewService(store)

	_, err := svc.AddMember(context.Background(), service.AddMemberInput{
		RequestingUserID: owner.String(),
		ProjectID:        project.String(),
		Email:            "owner@example.com",
		Role:             "editor",
	})
	if !errors.Is(err, service.ErrMemberConflict) {
		t.Fatalf("err = %v, want ErrMemberConflict", err)
	}
	if store.addMemberCalled {
		t.Fatal("store.AddProjectMember was called for the owner")
	}
}

func TestServiceAddMemberAlreadyMemberMapsConflict(t *testing.T) {
	owner := uuid.New()
	store := &fakeProjectStore{
		getAccess:       ownerAccess(domain.Project{ID: uuid.New(), OwnerUserID: owner}),
		findEmailResult: uuid.New(),
		addMemberErr:    domain.ErrMemberAlreadyExists,
	}
	svc := service.NewService(store)

	_, err := svc.AddMember(context.Background(), service.AddMemberInput{
		RequestingUserID: owner.String(),
		ProjectID:        uuid.New().String(),
		Email:            "teammate@example.com",
		Role:             "viewer",
	})
	if !errors.Is(err, service.ErrMemberConflict) {
		t.Fatalf("err = %v, want ErrMemberConflict", err)
	}
}

func TestServiceAddMemberUnknownEmailIsInvalidInput(t *testing.T) {
	owner := uuid.New()
	store := &fakeProjectStore{
		getAccess:    ownerAccess(domain.Project{ID: uuid.New(), OwnerUserID: owner}),
		findEmailErr: domain.ErrMemberUserNotFound,
	}
	svc := service.NewService(store)

	_, err := svc.AddMember(context.Background(), service.AddMemberInput{
		RequestingUserID: owner.String(),
		ProjectID:        uuid.New().String(),
		Email:            "ghost@example.com",
		Role:             "viewer",
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestServiceAddMemberRejectsInvalidRole(t *testing.T) {
	owner := uuid.New()
	for _, role := range []string{"", "owner", "admin"} {
		store := &fakeProjectStore{getAccess: ownerAccess(domain.Project{ID: uuid.New(), OwnerUserID: owner})}
		svc := service.NewService(store)
		_, err := svc.AddMember(context.Background(), service.AddMemberInput{
			RequestingUserID: owner.String(),
			ProjectID:        uuid.New().String(),
			Email:            "teammate@example.com",
			Role:             role,
		})
		if !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("role %q: err = %v, want ErrInvalidInput", role, err)
		}
	}
}

func TestServiceListMembersAllowsAnyAccessRole(t *testing.T) {
	project := uuid.New()
	want := []domain.ProjectMemberDetail{{UserID: uuid.New(), Role: domain.ProjectRoleViewer}}
	for _, role := range []domain.AccessRole{domain.AccessRoleOwner, domain.AccessRoleEditor, domain.AccessRoleViewer} {
		store := &fakeProjectStore{
			getAccess:         domain.ProjectAccess{Project: domain.Project{ID: project}, AccessRole: role},
			listMembersResult: want,
		}
		svc := service.NewService(store)
		members, err := svc.ListMembers(context.Background(), uuid.New().String(), project.String())
		if err != nil {
			t.Fatalf("role %q: ListMembers error: %v", role, err)
		}
		if len(members) != 1 {
			t.Fatalf("role %q: members = %d, want 1", role, len(members))
		}
	}
}

func TestServiceListMembersMapsNotFound(t *testing.T) {
	store := &fakeProjectStore{getErr: domain.ErrProjectNotFound}
	svc := service.NewService(store)
	if _, err := svc.ListMembers(context.Background(), uuid.New().String(), uuid.New().String()); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateMemberRoleOwnerSucceeds(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	target := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{
		getAccess:          ownerAccess(domain.Project{ID: project, OwnerUserID: owner}),
		updateMemberResult: domain.ProjectMember{ProjectID: project, UserID: target, Role: domain.ProjectRoleViewer},
	}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	if _, err := svc.UpdateMemberRole(context.Background(), service.UpdateMemberRoleInput{
		RequestingUserID: owner.String(),
		ProjectID:        project.String(),
		TargetUserID:     target.String(),
		Role:             "viewer",
	}); err != nil {
		t.Fatalf("UpdateMemberRole error: %v", err)
	}
	if store.updateMemberInput.UserID != target || store.updateMemberInput.Role != domain.ProjectRoleViewer {
		t.Fatalf("update input = %#v, want target/viewer", store.updateMemberInput)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventMemberRoleChanged {
		t.Fatalf("audit = %#v, want one member.role_changed", recorder.calls)
	}
}

func TestServiceUpdateMemberRoleNonOwnerForbidden(t *testing.T) {
	store := &fakeProjectStore{getAccess: domain.ProjectAccess{
		Project: domain.Project{ID: uuid.New()}, AccessRole: domain.AccessRoleEditor,
	}}
	svc := service.NewService(store)
	_, err := svc.UpdateMemberRole(context.Background(), service.UpdateMemberRoleInput{
		RequestingUserID: uuid.New().String(),
		ProjectID:        uuid.New().String(),
		TargetUserID:     uuid.New().String(),
		Role:             "viewer",
	})
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestServiceUpdateMemberRoleMapsNotFound(t *testing.T) {
	owner := uuid.New()
	store := &fakeProjectStore{
		getAccess:       ownerAccess(domain.Project{ID: uuid.New(), OwnerUserID: owner}),
		updateMemberErr: domain.ErrProjectMemberNotFound,
	}
	svc := service.NewService(store)
	_, err := svc.UpdateMemberRole(context.Background(), service.UpdateMemberRoleInput{
		RequestingUserID: owner.String(),
		ProjectID:        uuid.New().String(),
		TargetUserID:     uuid.New().String(),
		Role:             "editor",
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestServiceRemoveMemberOwnerSucceeds(t *testing.T) {
	owner := uuid.New()
	project := uuid.New()
	target := uuid.New()
	recorder := &fakeAuditRecorder{}
	store := &fakeProjectStore{getAccess: ownerAccess(domain.Project{ID: project, OwnerUserID: owner})}
	svc := service.NewService(store, service.WithAuditRecorder(recorder))

	if err := svc.RemoveMember(context.Background(), owner.String(), project.String(), target.String()); err != nil {
		t.Fatalf("RemoveMember error: %v", err)
	}
	if store.removeMemberProject != project || store.removeMemberUser != target {
		t.Fatalf("remove args = %s/%s, want %s/%s", store.removeMemberProject, store.removeMemberUser, project, target)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].EventType != service.EventMemberRemoved {
		t.Fatalf("audit = %#v, want one member.removed", recorder.calls)
	}
}

func TestServiceRemoveMemberNonOwnerForbidden(t *testing.T) {
	store := &fakeProjectStore{getAccess: domain.ProjectAccess{
		Project: domain.Project{ID: uuid.New()}, AccessRole: domain.AccessRoleViewer,
	}}
	svc := service.NewService(store)
	err := svc.RemoveMember(context.Background(), uuid.New().String(), uuid.New().String(), uuid.New().String())
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if store.removeMemberCalled {
		t.Fatal("store.RemoveProjectMember was called for a non-owner")
	}
}

func TestServiceRemoveMemberMapsNotFound(t *testing.T) {
	owner := uuid.New()
	store := &fakeProjectStore{
		getAccess:       ownerAccess(domain.Project{ID: uuid.New(), OwnerUserID: owner}),
		removeMemberErr: domain.ErrProjectMemberNotFound,
	}
	svc := service.NewService(store)
	err := svc.RemoveMember(context.Background(), owner.String(), uuid.New().String(), uuid.New().String())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
