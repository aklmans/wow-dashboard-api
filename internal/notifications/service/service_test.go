package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/notifications/domain"
)

type fakeStore struct {
	listInput  domain.ListInput
	listResult domain.ListResult
	unread     int64
	markReadIn [2]uuid.UUID
	markAllIn  uuid.UUID
	created    domain.CreateInput
	createErr  error
}

func (f *fakeStore) ListNotifications(_ context.Context, input domain.ListInput) (domain.ListResult, error) {
	f.listInput = input
	return f.listResult, nil
}

func (f *fakeStore) CountUnread(context.Context, uuid.UUID) (int64, error) {
	return f.unread, nil
}

func (f *fakeStore) MarkRead(_ context.Context, userID, id uuid.UUID) (int64, error) {
	f.markReadIn = [2]uuid.UUID{userID, id}
	return 1, nil
}

func (f *fakeStore) MarkAllRead(_ context.Context, userID uuid.UUID) (int64, error) {
	f.markAllIn = userID
	return 1, nil
}

func (f *fakeStore) Create(_ context.Context, input domain.CreateInput) (domain.Notification, error) {
	f.created = input
	if f.createErr != nil {
		return domain.Notification{}, f.createErr
	}
	return domain.Notification{ID: uuid.New(), UserID: input.UserID, Type: input.Type, Title: input.Title}, nil
}

func testUserID() uuid.UUID { return uuid.MustParse("11111111-1111-1111-1111-111111111111") }

func makeNotifications(n int) []domain.Notification {
	out := make([]domain.Notification, n)
	for i := range out {
		out[i] = domain.Notification{ID: uuid.New(), Metadata: map[string]any{}}
	}
	return out
}

func TestListDefaultsLimitProbesForMoreAndAddsUnreadCount(t *testing.T) {
	// Return default+1 (21) rows so the service trims to 20 and flags HasMore.
	store := &fakeStore{listResult: domain.ListResult{Notifications: makeNotifications(21)}, unread: 3}
	svc := NewService(store)

	result, err := svc.List(context.Background(), ListInput{UserID: testUserID()})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if store.listInput.Limit != 21 {
		t.Fatalf("store limit = %d, want 21 (default 20 + probe row)", store.listInput.Limit)
	}
	if len(result.Notifications) != 20 {
		t.Fatalf("len(notifications) = %d, want 20 after trim", len(result.Notifications))
	}
	if !result.HasMore {
		t.Fatal("HasMore = false, want true when the probe row is present")
	}
	if result.Limit != 20 {
		t.Fatalf("Limit = %d, want 20", result.Limit)
	}
	if result.UnreadCount != 3 {
		t.Fatalf("UnreadCount = %d, want 3", result.UnreadCount)
	}
}

func TestListPropagatesUnreadOnlyAndReturnsEmptyNonNil(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)

	result, err := svc.List(context.Background(), ListInput{UserID: testUserID(), UnreadOnly: true})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if !store.listInput.UnreadOnly {
		t.Fatal("UnreadOnly was not propagated to the store")
	}
	if result.Notifications == nil {
		t.Fatal("Notifications is nil, want an empty non-nil slice")
	}
}

func TestListRejectsOutOfRangeLimit(t *testing.T) {
	svc := NewService(&fakeStore{})
	if _, err := svc.List(context.Background(), ListInput{UserID: testUserID(), Limit: 101, LimitProvided: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestListRequiresUser(t *testing.T) {
	svc := NewService(&fakeStore{})
	if _, err := svc.List(context.Background(), ListInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestMarkReadScopesToUserAndReturnsUnreadCount(t *testing.T) {
	store := &fakeStore{unread: 4}
	svc := NewService(store)
	id := uuid.New()

	count, err := svc.MarkRead(context.Background(), testUserID(), id)
	if err != nil {
		t.Fatalf("MarkRead returned error: %v", err)
	}
	if store.markReadIn != [2]uuid.UUID{testUserID(), id} {
		t.Fatalf("MarkRead args = %v, want [user, id]", store.markReadIn)
	}
	if count != 4 {
		t.Fatalf("count = %d, want the recomputed unread count 4", count)
	}
}

func TestMarkAllReadReturnsUnreadCount(t *testing.T) {
	store := &fakeStore{unread: 0}
	svc := NewService(store)

	count, err := svc.MarkAllRead(context.Background(), testUserID())
	if err != nil {
		t.Fatalf("MarkAllRead returned error: %v", err)
	}
	if store.markAllIn != testUserID() {
		t.Fatal("MarkAllRead did not pass the user id")
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

func TestCreateValidatesRequiredFields(t *testing.T) {
	svc := NewService(&fakeStore{})
	cases := []domain.CreateInput{
		{Type: "t", Title: "x"},            // missing user
		{UserID: testUserID(), Title: "x"}, // missing type
		{UserID: testUserID(), Type: "t"},  // missing title
	}
	for i, in := range cases {
		if _, err := svc.Create(context.Background(), in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d err = %v, want ErrInvalidInput", i, err)
		}
	}
}

func TestCreateDelegatesToStore(t *testing.T) {
	store := &fakeStore{}
	svc := NewService(store)

	if _, err := svc.Create(context.Background(), domain.CreateInput{
		UserID: testUserID(),
		Type:   "users.roles.updated",
		Title:  "Your roles were updated",
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if store.created.Type != "users.roles.updated" {
		t.Fatalf("created input = %#v, want the passed values", store.created)
	}
}
