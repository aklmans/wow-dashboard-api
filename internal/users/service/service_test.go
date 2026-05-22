package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/users/domain"
	"github.com/aklmans/wow-dashboard-api/internal/users/service"
)

func TestServiceListUsersDefaultsAndNormalizesInput(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Search: "  Demo  ",
		Role:   "ADMIN",
		Status: "ACTIVE",
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 1 {
		t.Fatalf("page = %d, want 1", store.input.Page)
	}
	if store.input.PageSize != 20 {
		t.Fatalf("pageSize = %d, want 20", store.input.PageSize)
	}
	if store.input.Search != "Demo" {
		t.Fatalf("search = %q, want trimmed Demo", store.input.Search)
	}
	if store.input.Role != domain.UserRoleAdmin {
		t.Fatalf("role = %q, want admin", store.input.Role)
	}
	if store.input.Status != domain.UserStatusActive {
		t.Fatalf("status = %q, want active", store.input.Status)
	}
}

func TestServiceListUsersAcceptsPaginationBoundary(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Page:     2,
		PageSize: 100,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 2 {
		t.Fatalf("page = %d, want 2", store.input.Page)
	}
	if store.input.PageSize != 100 {
		t.Fatalf("pageSize = %d, want 100", store.input.PageSize)
	}
	if store.input.Offset != 100 {
		t.Fatalf("offset = %d, want 100", store.input.Offset)
	}
}

func TestServiceListUsersDefaultsPageSizeWhenPageIsProvided(t *testing.T) {
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.ListUsers(context.Background(), service.ListUsersInput{
		Page: 2,
	})
	if err != nil {
		t.Fatalf("ListUsers returned error: %v", err)
	}

	if store.input.Page != 2 {
		t.Fatalf("page = %d, want 2", store.input.Page)
	}
	if store.input.PageSize != 20 {
		t.Fatalf("pageSize = %d, want 20", store.input.PageSize)
	}
}

func TestServiceListUsersRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input service.ListUsersInput
	}{
		{
			name:  "negative page",
			input: service.ListUsersInput{Page: -1},
		},
		{
			name:  "negative page size",
			input: service.ListUsersInput{Page: 1, PageSize: -1},
		},
		{
			name:  "too large page size",
			input: service.ListUsersInput{Page: 1, PageSize: 101},
		},
		{
			name:  "invalid role",
			input: service.ListUsersInput{Role: "owner"},
		},
		{
			name:  "invalid status",
			input: service.ListUsersInput{Status: "pending"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUserStore{}
			svc := service.NewService(store)

			_, err := svc.ListUsers(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("ListUsers error = %v, want ErrInvalidInput", err)
			}
			if store.called {
				t.Fatal("store was called for invalid input")
			}
		})
	}
}

func TestServiceGetUserPassesParsedID(t *testing.T) {
	want := uuid.New()
	store := &fakeUserStore{getResult: domain.User{ID: want, Email: "demo@example.com"}}
	svc := service.NewService(store)

	got, err := svc.GetUser(context.Background(), "  "+want.String()+"  ")
	if err != nil {
		t.Fatalf("GetUser returned error: %v", err)
	}
	if !store.getCalled {
		t.Fatal("store.GetUserByID was not called")
	}
	if store.getID != want {
		t.Fatalf("store.GetUserByID id = %s, want %s", store.getID, want)
	}
	if got.ID != want {
		t.Fatalf("returned user id = %s, want %s", got.ID, want)
	}
}

func TestServiceGetUserRejectsInvalidID(t *testing.T) {
	cases := []string{"", "   ", "not-a-uuid"}
	for _, in := range cases {
		store := &fakeUserStore{}
		svc := service.NewService(store)

		_, err := svc.GetUser(context.Background(), in)
		if !errors.Is(err, service.ErrInvalidInput) {
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

	_, err := svc.GetUser(context.Background(), uuid.New().String())
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("GetUser err = %v, want service.ErrNotFound", err)
	}
}

type fakeUserStore struct {
	called       bool
	input        domain.ListUsersInput
	result       domain.ListUsersResult
	err          error
	getCalled    bool
	getID        uuid.UUID
	getResult    domain.User
	getErr       error
	updateCalled bool
	updateInput  domain.UpdateUserInput
	updateResult domain.User
	updateErr    error
}

func (f *fakeUserStore) ListUsers(ctx context.Context, input domain.ListUsersInput) (domain.ListUsersResult, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return domain.ListUsersResult{}, f.err
	}
	return f.result, nil
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

type fakeUserAuditRecorder struct {
	events []service.AuditEvent
	err    error
}

func (f *fakeUserAuditRecorder) RecordUserEvent(ctx context.Context, event service.AuditEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func strptr(s string) *string { return &s }

func TestServiceUpdateUserChangesRoleAndStatus(t *testing.T) {
	actorID := uuid.New()
	targetID := uuid.New()
	store := &fakeUserStore{updateResult: domain.User{ID: targetID, Role: domain.UserRoleAdmin, Status: domain.UserStatusActive}}
	audit := &fakeUserAuditRecorder{}
	svc := service.NewService(store, service.WithAuditRecorder(audit))

	user, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  actorID.String(),
		TargetUserID: targetID.String(),
		Role:         strptr("ADMIN"),
		Status:       strptr("active"),
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if store.updateInput.ID != targetID {
		t.Fatalf("store update ID = %s, want %s", store.updateInput.ID, targetID)
	}
	if store.updateInput.Role == nil || *store.updateInput.Role != domain.UserRoleAdmin {
		t.Fatalf("store update role = %v, want admin (normalized)", store.updateInput.Role)
	}
	if store.updateInput.Status == nil || *store.updateInput.Status != domain.UserStatusActive {
		t.Fatalf("store update status = %v, want active", store.updateInput.Status)
	}
	if store.updateInput.UpdatedAt.IsZero() {
		t.Fatal("store update UpdatedAt was not set")
	}
	if user.ID != targetID {
		t.Fatalf("returned user id = %s, want %s", user.ID, targetID)
	}

	if len(audit.events) != 1 {
		t.Fatalf("recorded %d audit events, want 1", len(audit.events))
	}
	ev := audit.events[0]
	if ev.EventType != service.EventUserUpdated {
		t.Fatalf("audit event type = %q, want %q", ev.EventType, service.EventUserUpdated)
	}
	if ev.Metadata.TargetUserID != targetID.String() || ev.Metadata.ActorUserID != actorID.String() {
		t.Fatalf("audit ids = %s/%s, want target/actor %s/%s", ev.Metadata.TargetUserID, ev.Metadata.ActorUserID, targetID, actorID)
	}
	if len(ev.Metadata.ChangedFields) != 2 {
		t.Fatalf("audit changed_fields = %v, want role and status", ev.Metadata.ChangedFields)
	}
}

func TestServiceUpdateUserStatusOnlyLeavesRoleUnchanged(t *testing.T) {
	store := &fakeUserStore{updateResult: domain.User{ID: uuid.New()}}
	svc := service.NewService(store)

	_, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		Status:       strptr("disabled"),
	})
	if err != nil {
		t.Fatalf("UpdateUser returned error: %v", err)
	}
	if store.updateInput.Role != nil {
		t.Fatalf("store update role = %v, want nil (unchanged)", store.updateInput.Role)
	}
	if store.updateInput.Status == nil || *store.updateInput.Status != domain.UserStatusDisabled {
		t.Fatalf("store update status = %v, want disabled", store.updateInput.Status)
	}
}

func TestServiceUpdateUserRejectsSelfModification(t *testing.T) {
	id := uuid.New()
	store := &fakeUserStore{}
	svc := service.NewService(store)

	_, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  id.String(),
		TargetUserID: id.String(),
		Role:         strptr("user"),
	})
	if !errors.Is(err, service.ErrSelfModification) {
		t.Fatalf("UpdateUser error = %v, want ErrSelfModification", err)
	}
	if store.updateCalled {
		t.Fatal("store was called for a self modification")
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
		{"invalid role", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, Role: strptr("owner")}},
		{"empty role", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, Role: strptr("")}},
		{"invalid status", service.UpdateUserInput{ActorUserID: actor, TargetUserID: target, Status: strptr("pending")}},
		{"invalid target id", service.UpdateUserInput{ActorUserID: actor, TargetUserID: "not-a-uuid", Role: strptr("user")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeUserStore{}
			svc := service.NewService(store)
			_, err := svc.UpdateUser(context.Background(), tt.input)
			if !errors.Is(err, service.ErrInvalidInput) {
				t.Fatalf("UpdateUser error = %v, want ErrInvalidInput", err)
			}
			if store.updateCalled {
				t.Fatal("store was called for invalid input")
			}
		})
	}
}

func TestServiceUpdateUserPropagatesNotFound(t *testing.T) {
	store := &fakeUserStore{updateErr: domain.ErrUserNotFound}
	svc := service.NewService(store)

	_, err := svc.UpdateUser(context.Background(), service.UpdateUserInput{
		ActorUserID:  uuid.New().String(),
		TargetUserID: uuid.New().String(),
		Status:       strptr("disabled"),
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("UpdateUser error = %v, want service.ErrNotFound", err)
	}
}
