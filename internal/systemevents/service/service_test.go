package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
)

func TestServiceListUserActivity(t *testing.T) {
	userID := uuid.New()

	t.Run("scopes to the user, defaults the limit, and probes for more", func(t *testing.T) {
		store := &fakeEventStore{}
		svc := NewService(store)

		result, err := svc.ListUserActivity(context.Background(), ListUserActivityInput{UserID: userID})
		if err != nil {
			t.Fatalf("ListUserActivity returned error: %v", err)
		}
		if store.activityInput.UserID != userID {
			t.Fatalf("store user = %s, want %s", store.activityInput.UserID, userID)
		}
		if store.activityInput.Limit != 21 {
			t.Fatalf("store limit = %d, want 21 (page size + 1 probe)", store.activityInput.Limit)
		}
		if result.Limit != 20 {
			t.Fatalf("result limit = %d, want 20", result.Limit)
		}
	})

	t.Run("trims the probe row and reports HasMore", func(t *testing.T) {
		store := &fakeEventStore{result: domain.ListEventsResult{Events: make([]domain.Event, 21)}}
		svc := NewService(store)

		result, err := svc.ListUserActivity(context.Background(), ListUserActivityInput{UserID: userID})
		if err != nil {
			t.Fatalf("ListUserActivity returned error: %v", err)
		}
		if len(result.Events) != 20 || !result.HasMore {
			t.Fatalf("events=%d hasMore=%v, want 20 + HasMore", len(result.Events), result.HasMore)
		}
	})

	t.Run("rejects an out-of-range limit", func(t *testing.T) {
		svc := NewService(&fakeEventStore{})
		_, err := svc.ListUserActivity(context.Background(), ListUserActivityInput{UserID: userID, Limit: 9999, LimitProvided: true})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})
}

func TestServiceListEventsNormalizesLimit(t *testing.T) {
	t.Run("default limit is 20", func(t *testing.T) {
		store := &fakeEventStore{}
		svc := NewService(store)

		result, err := svc.ListEvents(context.Background(), ListEventsInput{})
		if err != nil {
			t.Fatalf("ListEvents returned error: %v", err)
		}

		// The service fetches one extra probe row to detect a further page.
		if store.input.Limit != 21 {
			t.Fatalf("store limit = %d, want 21 (page size + 1 probe)", store.input.Limit)
		}
		if result.Limit != 20 {
			t.Fatalf("result limit = %d, want 20", result.Limit)
		}
	})

	t.Run("max limit is accepted", func(t *testing.T) {
		store := &fakeEventStore{}
		svc := NewService(store)

		result, err := svc.ListEvents(context.Background(), ListEventsInput{Limit: 100, LimitProvided: true})
		if err != nil {
			t.Fatalf("ListEvents returned error: %v", err)
		}

		if store.input.Limit != 101 {
			t.Fatalf("store limit = %d, want 101 (page size + 1 probe)", store.input.Limit)
		}
		if result.Limit != 100 {
			t.Fatalf("result limit = %d, want 100", result.Limit)
		}
	})
}

func TestServiceListEventsHasMoreTrimsProbeRow(t *testing.T) {
	// Store returns limit+1 rows -> the service trims to limit and reports HasMore.
	events := make([]domain.Event, 21)
	store := &fakeEventStore{result: domain.ListEventsResult{Events: events}}
	svc := NewService(store)

	result, err := svc.ListEvents(context.Background(), ListEventsInput{})
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(result.Events) != 20 {
		t.Fatalf("returned %d events, want 20 (probe row trimmed)", len(result.Events))
	}
	if !result.HasMore {
		t.Fatal("HasMore = false, want true when the probe row was present")
	}
}

func TestServiceListEventsNoMoreWhenUnderLimit(t *testing.T) {
	store := &fakeEventStore{result: domain.ListEventsResult{Events: make([]domain.Event, 5)}}
	svc := NewService(store)

	result, err := svc.ListEvents(context.Background(), ListEventsInput{})
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if result.HasMore {
		t.Fatal("HasMore = true, want false when fewer than a full page was returned")
	}
}

func TestServiceListEventsForwardsFilters(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewService(store)
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	if _, err := svc.ListEvents(context.Background(), ListEventsInput{
		EventType:     "  projects.project.created  ",
		CreatedAfter:  &after,
		CreatedBefore: &before,
	}); err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if store.input.EventType == nil || *store.input.EventType != "projects.project.created" {
		t.Fatalf("store event_type = %v, want trimmed 'projects.project.created'", store.input.EventType)
	}
	if store.input.CreatedAfter == nil || store.input.CreatedBefore == nil {
		t.Fatal("store did not receive the created_at range filters")
	}
}

func TestServiceListEventsRejectsInvertedWindow(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewService(store)
	after := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := svc.ListEvents(context.Background(), ListEventsInput{CreatedAfter: &after, CreatedBefore: &before}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ListEvents error = %v, want ErrInvalidInput for before <= after", err)
	}
	if store.called {
		t.Fatal("store was called for an inverted time window")
	}
}

func TestServiceListEventsRejectsInvalidLimit(t *testing.T) {
	for _, limit := range []int{-1, 0, 101} {
		t.Run("limit", func(t *testing.T) {
			store := &fakeEventStore{}
			svc := NewService(store)

			_, err := svc.ListEvents(context.Background(), ListEventsInput{Limit: limit, LimitProvided: true})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ListEvents error = %v, want ErrInvalidInput", err)
			}
			if store.called {
				t.Fatal("store was called for invalid limit")
			}
		})
	}
}

type fakeEventStore struct {
	called        bool
	input         domain.ListEventsInput
	activityInput domain.ListUserActivityInput
	result        domain.ListEventsResult
	err           error
}

func (f *fakeEventStore) ListEvents(ctx context.Context, input domain.ListEventsInput) (domain.ListEventsResult, error) {
	f.called = true
	f.input = input
	if f.err != nil {
		return domain.ListEventsResult{}, f.err
	}
	if f.result.Limit == 0 {
		f.result.Limit = input.Limit
	}
	return f.result, nil
}

func (f *fakeEventStore) ListUserActivity(ctx context.Context, input domain.ListUserActivityInput) (domain.ListEventsResult, error) {
	f.called = true
	f.activityInput = input
	if f.err != nil {
		return domain.ListEventsResult{}, f.err
	}
	if f.result.Limit == 0 {
		f.result.Limit = input.Limit
	}
	return f.result, nil
}
