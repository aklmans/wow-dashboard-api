package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/audit/auditctx"
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
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Status: domain.UserStatusDisabled, Roles: []string{"user"}}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Status:       strptr("DISABLED"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateUser was not called")
	}
	if store.updateInput.Status == nil || *store.updateInput.Status != domain.UserStatusDisabled {
		t.Fatalf("update status = %v, want disabled", store.updateInput.Status)
	}
	if store.updateInput.RoleIDs != nil {
		t.Fatalf("update RoleIDs = %v, want nil for a status-only update", store.updateInput.RoleIDs)
	}
	if len(audit.events) != 1 || audit.events[0].EventType != service.EventUserUpdated {
		t.Fatalf("audit = %#v, want one users.user.updated", audit.events)
	}
}

func TestServiceUpdateUserAttributesImpersonator(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	impersonatorID := uuid.New()
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Status: domain.UserStatusDisabled, Roles: []string{"user"}}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	// An impersonated request stamps the admin behind the "act as" session into
	// the audit metadata so the action is attributable to the real actor.
	ctx := auditctx.WithImpersonator(context.Background(), impersonatorID.String())
	if _, err := svc.UpdateUser(ctx, service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Status:       strptr("DISABLED"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	if got := audit.events[0].Metadata.ImpersonatorID; got != impersonatorID.String() {
		t.Fatalf("ImpersonatorID = %q, want %q", got, impersonatorID.String())
	}
}

func TestServiceUpdateUserOmitsImpersonatorWhenNotImpersonating(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Status: domain.UserStatusDisabled, Roles: []string{"user"}}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Status:       strptr("DISABLED"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if got := audit.events[0].Metadata.ImpersonatorID; got != "" {
		t.Fatalf("ImpersonatorID = %q, want empty for a non-impersonated update", got)
	}
}

func TestServiceUpdateUserReplacesRoles(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	roleA := uuid.New()
	roleB := uuid.New()
	store := &fakeUserStore{updateResult: domain.User{ID: targetID}}
	svc := service.NewService(store)

	// roleA is repeated to confirm the service de-duplicates.
	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		RoleIDs:      &[]string{roleA.String(), roleB.String(), roleA.String()},
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if len(store.updateInput.RoleIDs) != 2 {
		t.Fatalf("update RoleIDs = %v, want 2 (de-duplicated)", store.updateInput.RoleIDs)
	}
	if store.updateInput.Status != nil {
		t.Fatalf("update status = %v, want nil for a roles-only update", store.updateInput.Status)
	}
}

func TestServiceUpdateUserRejectsNonAdminGrantingSystemAdminRole(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	adminRoleID := uuid.MustParse("00000000-0000-0000-0000-0000000a0001")
	store := &fakeUserStore{}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:      actorID.String(),
		ActorRoles:       []string{"operator"},
		ActorPermissions: []string{"users:manage"},
		TargetUserID:     targetID.String(),
		RoleIDs:          &[]string{adminRoleID.String()},
	}); !errors.Is(err, service.ErrInsufficientPrivilege) {
		t.Fatalf("UpdateUser error = %v, want ErrInsufficientPrivilege", err)
	}
	if store.updateCalled {
		t.Fatal("store.UpdateUser was called for forbidden admin-role grant")
	}
}

func TestServiceUpdateUserRejectsNonAdminMutatingSystemAdminTarget(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: targetID, Roles: []string{"admin"}}}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:      actorID.String(),
		ActorRoles:       []string{"operator"},
		ActorPermissions: []string{"users:manage"},
		TargetUserID:     targetID.String(),
		Status:           strptr("disabled"),
	}); !errors.Is(err, service.ErrInsufficientPrivilege) {
		t.Fatalf("UpdateUser error = %v, want ErrInsufficientPrivilege", err)
	}
	if store.updateCalled {
		t.Fatal("store.UpdateUser was called for forbidden admin-target mutation")
	}
}

func TestServiceUpdateUserAllowsAdminMutatingSystemAdminTarget(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{
		getResult:    domain.User{ID: targetID, Roles: []string{"admin"}},
		updateResult: domain.User{ID: targetID, Roles: []string{"admin"}, Status: domain.UserStatusDisabled},
	}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:      actorID.String(),
		ActorRoles:       []string{"admin"},
		ActorPermissions: []string{"users:manage"},
		TargetUserID:     targetID.String(),
		Status:           strptr("disabled"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateUser was not called for admin-target mutation by admin")
	}
}

func TestServiceUpdateUserAllowsSystemAdminGrantWithWildcardPermission(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	adminRoleID := uuid.MustParse("00000000-0000-0000-0000-0000000a0001")
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Roles: []string{"admin"}}}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:      actorID.String(),
		ActorPermissions: []string{"*"},
		TargetUserID:     targetID.String(),
		RoleIDs:          &[]string{adminRoleID.String()},
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateUser was not called for wildcard actor")
	}
}

func TestServiceUpdateUserAllowsSystemAdminGrantFromAdminRole(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	adminRoleID := uuid.MustParse("00000000-0000-0000-0000-0000000a0001")
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Roles: []string{"admin"}}}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:      actorID.String(),
		ActorRoles:       []string{"admin"},
		ActorPermissions: []string{"users:manage"},
		TargetUserID:     targetID.String(),
		RoleIDs:          &[]string{adminRoleID.String()},
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("store.UpdateUser was not called for admin actor")
	}
}

func TestServiceUpdateUserChangesProfileFields(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{updateResult: domain.User{ID: targetID}}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Phone:        strptr("  +1 555 0100  "),
		JobTitle:     strptr("Engineer"),
		Company:      strptr("Acme"),
		AvatarURL:    strptr("https://cdn.example.com/a.png"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	// Profile fields are trimmed and forwarded to the store.
	if store.updateInput.Phone == nil || *store.updateInput.Phone != "+1 555 0100" {
		t.Fatalf("update Phone = %v, want trimmed value", store.updateInput.Phone)
	}
	if store.updateInput.JobTitle == nil || *store.updateInput.JobTitle != "Engineer" {
		t.Fatalf("update JobTitle = %v, want Engineer", store.updateInput.JobTitle)
	}
	if store.updateInput.Company == nil || *store.updateInput.Company != "Acme" {
		t.Fatalf("update Company = %v, want Acme", store.updateInput.Company)
	}
	if store.updateInput.AvatarURL == nil {
		t.Fatal("update AvatarURL was not forwarded to the store")
	}
	// A field that was not provided stays nil.
	if store.updateInput.Status != nil {
		t.Fatalf("update Status = %v, want nil for a profile-only update", store.updateInput.Status)
	}
}

func TestServiceUpdateUserRejectsOverlongProfileField(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		JobTitle:     strptr(strings.Repeat("x", 257)),
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
	}
	if store.updateCalled {
		t.Fatal("store.UpdateUser was called despite an invalid profile field")
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
	if store.updateCalled {
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
			store := &fakeUserStore{}
			svc := service.NewService(store)
			if _, err := svc.UpdateUser(context.Background(), tt.input); !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
			}
			if store.updateCalled {
				t.Fatal("store was mutated for invalid input")
			}
		})
	}
}

func TestServiceUpdateUserMapsNotFound(t *testing.T) {
	store := &fakeUserStore{updateErr: domain.ErrUserNotFound}
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
	store := &fakeUserStore{updateErr: domain.ErrRoleNotFound}
	svc := service.NewService(store)

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		RoleIDs:      &[]string{uuid.New().String()},
	}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
	}
}

// --- Transactional audit (unit of work) ------------------------------------

func TestServiceUpdateUserTransactionalRecordsAuditInSameUnit(t *testing.T) {
	mutator := &fakeUserStore{updateResult: domain.User{Status: domain.UserStatusDisabled}}
	audit := &fakeUserAuditRecorder{}
	uow := &fakeUnitOfWork{mutator: mutator, recorder: audit}
	svc := service.NewService(&fakeUserStore{}, service.WithUnitOfWork(uow))

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		Status:       strptr("disabled"),
	}); err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if !uow.called {
		t.Fatal("unit of work was not used when configured")
	}
	if !uow.committed {
		t.Fatal("unit of work did not commit")
	}
	if !mutator.updateCalled {
		t.Fatal("mutation did not run inside the unit of work")
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1 (recorded in the same unit)", len(audit.events))
	}
	if audit.events[0].EventType != service.EventUserUpdated {
		t.Fatalf("audit event type = %q, want %q", audit.events[0].EventType, service.EventUserUpdated)
	}
}

func TestServiceUpdateUserTransactionalAuditFailureRollsBack(t *testing.T) {
	mutator := &fakeUserStore{updateResult: domain.User{Status: domain.UserStatusActive}}
	audit := &fakeUserAuditRecorder{err: errors.New("audit insert failed")}
	uow := &fakeUnitOfWork{mutator: mutator, recorder: audit}
	svc := service.NewService(&fakeUserStore{}, service.WithUnitOfWork(uow))

	if _, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		Status:       strptr("active"),
	}); err == nil {
		t.Fatal("UpdateUser should fail when the audit write fails in the unit of work")
	}
	if !mutator.updateCalled {
		t.Fatal("mutation should have been attempted inside the unit of work")
	}
	if uow.committed {
		t.Fatal("unit of work must not commit when the audit write fails")
	}
}

// --- fakes ------------------------------------------------------------------

// fakeUnitOfWork runs the work function with the configured mutator and
// recorder, committing only when the function returns nil — mirroring a real
// transaction's commit/rollback semantics.
type fakeUnitOfWork struct {
	mutator   service.UserMutator
	recorder  service.AuditRecorder
	called    bool
	committed bool
}

func (f *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context, service.WorkDeps) error) error {
	f.called = true
	if err := fn(ctx, service.WorkDeps{Users: f.mutator, Audit: f.recorder}); err != nil {
		return err
	}
	f.committed = true
	return nil
}

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

	updateCalled bool
	updateInput  domain.UpdateUserInput
	updateResult domain.User
	updateErr    error
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

func (f *fakeUserStore) UpdateUser(ctx context.Context, input domain.UpdateUserInput) (domain.User, error) {
	f.updateCalled = true
	f.updateInput = input
	if f.updateErr != nil {
		return domain.User{}, f.updateErr
	}
	return f.updateResult, nil
}
