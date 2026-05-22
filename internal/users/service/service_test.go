package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	"github.com/aklmans/wow-dashboard-api/internal/users/service"
)

func strptr(s string) *string { return &s }

// --- ListUsers --------------------------------------------------------------

func TestServiceListUsersDefaultsAndNormalizesInput(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	if _, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Search: "  Demo  ",
		Role:   "  admin  ",
		Status: "ACTIVE",
	}); err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.listInput.Page != 1 || store.listInput.PageSize != 20 {
		t.Fatalf("pagination = %d/%d, want 1/20", store.listInput.Page, store.listInput.PageSize)
	}
	if store.listInput.Search != "Demo" {
		t.Fatalf("search = %q, want trimmed Demo", store.listInput.Search)
	}
	if store.listInput.Role != "admin" {
		t.Fatalf("role = %q, want trimmed admin", store.listInput.Role)
	}
	if store.listInput.Status != domain.UserStatusActive {
		t.Fatalf("status = %q, want active", store.listInput.Status)
	}
}

func TestServiceListUsersAcceptsPaginationBoundary(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	if _, err := svc.ListUsers(context.Background(), service.ListUsersInput{Page: 2, PageSize: 100}); err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}
	if store.listInput.Page != 2 || store.listInput.PageSize != 100 || store.listInput.Offset != 100 {
		t.Fatalf("pagination = %#v, want page 2 size 100 offset 100", store.listInput)
	}
}

func TestServiceListUsersRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input service.ListUsersInput
	}{
		{"negative page", service.ListUsersInput{Page: -1}},
		{"too large page size", service.ListUsersInput{Page: 1, PageSize: 101}},
		{"invalid status", service.ListUsersInput{Status: "pending"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUserStore{}
			svc := service.NewService(store)
			if _, err := svc.ListUsers(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("ListUsers error = %v, want ErrInvalidInput", err)
			}
			if store.listCalled {
				t.Fatal("store was called for invalid input")
			}
		})
	}
}

// --- GetUser ----------------------------------------------------------------

func TestServiceGetUserPassesParsedID(t *testing.T) {
	want := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: want, Email: "demo@example.com", Roles: []string{"admin"}}}
	svc := service.NewService(store)

	got, err := svc.GetUser(context.Background(), "  "+want.String()+"  ")
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if store.getID != want || got.ID != want {
		t.Fatalf("store id %s / returned id %s, want %s", store.getID, got.ID, want)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Fatalf("returned roles = %v, want [admin]", got.Roles)
	}
}

func TestServiceGetUserRejectsInvalidID(t *testing.T) {
	for _, in := range []string{"", "   ", "not-a-uuid"} {
		store := &fakeUserStore{}
		svc := service.NewService(store)
		if _, err := svc.GetUser(context.Background(), in); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatalf("GetUser(%q) err = %v, want ErrInvalidInput", in, err)
		}
		if store.getCalled {
			t.Fatalf("GetUser(%q) called store for invalid id", in)
		}
	}
}

func TestServiceGetUserPropagatesNotFound(t *testing.T) {
	store := &fakeUserStore{getErr: domain.ErrUserNotFound}
	svc := service.NewService(store)
	if _, err := svc.GetUser(context.Background(), uuid.New().String()); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("GetUser err = %v, want service.ErrNotFound", err)
	}
}

// --- UpdateUser -------------------------------------------------------------

func TestServiceUpdateUserChangesStatus(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: targetID, Status: domain.UserStatusDisabled, Roles: []string{"user"}}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Status:       strptr("DISABLED"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.statusCalled || store.statusInput.Status != domain.UserStatusDisabled {
		t.Fatalf("SetUserStatus call = %v / %#v, want disabled", store.statusCalled, store.statusInput)
	}
	if store.rolesCalled {
		t.Fatal("ReplaceUserRoles was called for a status-only update")
	}
	if len(audit.events) != 1 || audit.events[0].EventType != service.EventUserUpdated {
		t.Fatalf("audit = %#v, want one users.user.updated", audit.events)
	}
}

func TestServiceUpdateUserReplacesRoles(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	roleA := uuid.New()
	roleB := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: targetID}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	// roleA is repeated to confirm the service de-duplicates.
	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		RoleIDs:      &[]string{roleA.String(), roleB.String(), roleA.String()},
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.rolesCalled {
		t.Fatal("ReplaceUserRoles was not called")
	}
	if len(store.rolesInput.RoleIDs) != 2 {
		t.Fatalf("replaced role ids = %v, want 2 (de-duplicated)", store.rolesInput.RoleIDs)
	}
	if store.statusCalled {
		t.Fatal("SetUserStatus was called for a roles-only update")
	}
}

func TestServiceUpdateUserRejectsSelfModification(t *testing.T) {
	id := uuid.New()
	store := &fakeUserStore{}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  id.String(),
		TargetUserID: id.String(),
		Status:       strptr("disabled"),
	}); !errors.Is(err, service.ErrSelfModification) {
		t.Fatalf("UpdateUser error = %v, want ErrSelfModification", err)
	}
	if store.statusCalled || store.rolesCalled {
		t.Fatal("store was mutated for a self modification")
	}
}

func TestServiceUpdateUserRejectsInvalidInput(t *testing.T) {
	actor := uuid.New().String()
	target := uuid.New().String()
	tests := []struct {
		name  string
		input service.UpdateUserInput
	}{
		{"no fields provided", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target}},
		{"invalid status", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, Status: strptr("pending")}},
		{"empty role set", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, RoleIDs: &[]string{}}},
		{"invalid role id", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, RoleIDs: &[]string{"not-a-uuid"}}},
		{"invalid target id", service.UpdateUserInput{ActorUserID: actor, TargetUserID: "nope", Status: strptr("active")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUserStore{getResult: domain.User{}}
			svc := service.NewService(store)
			if _, err := svc.UpdateUser(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
			}
			if store.statusCalled || store.rolesCalled {
				t.Fatal("store was mutated for invalid input")
			}
		})
	}
}

func TestServiceUpdateUserMapsNotFound(t *testing.T) {
	store := &fakeUserStore{getErr: domain.ErrUserNotFound}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		Status:       strptr("disabled"),
	}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("UpdateUser error = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateUserMapsUnknownRole(t *testing.T) {
	store := &fakeUserStore{getResult: domain.User{}, rolesErr: domain.ErrRoleNotFound}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		RoleIDs:      &[]string{uuid.New().String()},
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
	}
}

// --- fakes ------------------------------------------------------------------

type fakeUserAuditRecorder struct {
	events []service.AuditEvent
	err    error
}

func (f *fakeUserAuditRecorder) RecordUserEvent(ctx context.Context, event service.AuditEvent) error {
	f.events = append(f.events, event)
	return f.err
}

type fakeUserStore struct {
	listCalled bool
	listInput  domain.ListUsersInput
	listResult domain.ListUsersResult
	listErr    error

	getCalled bool
	getID     uuid.UUID
	getResult domain.User
	getErr    error

	statusCalled bool
	statusInput  domain.SetUserStatusInput
	statusErr    error

	rolesCalled bool
	rolesInput  domain.ReplaceUserRolesInput
	rolesErr    error
}

func (f *fakeUserStore) ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error) {
	f.listCalled = true
	f.listInput = input
	return f.listResult, f.listErr
}

func (f *fakeUserStore) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	f.getCalled = true
	f.getID = id
	if f.getErr != nil {
		return domain.User{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeUserStore) SetUserStatus(ctx context.Context, input domain.SetUserStatusInput) error {
	f.statusCalled = true
	f.statusInput = input
	return f.statusErr
}

func (f *fakeUserStore) ReplaceUserRoles(ctx context.Context, input domain.ReplaceUserRolesInput) error {
	f.rolesCalled = true
	f.rolesInput = input
	return f.rolesErr
}
