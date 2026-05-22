package service

import (
	"context"
	"errors"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/systemevents/domain"
)

func TestServiceListEventsNormalizesLimit(t *testing.T) {
	t.Run("default limit is 20", func(t *testing.T) {
		store := &fakeEventStore{}
		svc := NewService(store)

		result, err := svc.ListEvents(context.Background(), ListEventsInput{})
		if err != nil {
			t.Fatalf("ListEvents returned error: %v", err)
		}

		if store.input.Limit != 20 {
			t.Fatalf("store limit = %d, want 20", store.input.Limit)
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

		if store.input.Limit != 100 {
			t.Fatalf("store limit = %d, want 100", store.input.Limit)
		}
		if result.Limit != 100 {
			t.Fatalf("result limit = %d, want 100", result.Limit)
		}
	})
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
	called bool
	input  domain.ListEventsInput
	result domain.ListEventsResult
	err    error
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
