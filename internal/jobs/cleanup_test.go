package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type fakeRetentionStore struct {
	refreshN, authN, eventsN       int64
	refreshErr, authErr, eventsErr error
	cutoff                         time.Time
	calls                          []string
}

func (f *fakeRetentionStore) PurgeExpiredRefreshTokens(context.Context) (int64, error) {
	f.calls = append(f.calls, "refresh")
	return f.refreshN, f.refreshErr
}

func (f *fakeRetentionStore) PurgeConsumedOrExpiredAuthTokens(context.Context) (int64, error) {
	f.calls = append(f.calls, "auth")
	return f.authN, f.authErr
}

func (f *fakeRetentionStore) PurgeSystemEventsBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.calls = append(f.calls, "events")
	f.cutoff = cutoff
	return f.eventsN, f.eventsErr
}

func TestRetentionCleanupWorker_PurgesAllThreeWithRetentionCutoff(t *testing.T) {
	store := &fakeRetentionStore{refreshN: 3, authN: 2, eventsN: 7}
	ttl := 90 * 24 * time.Hour
	w := &RetentionCleanupWorker{store: store, systemEventsTTL: ttl}

	before := time.Now().Add(-ttl)
	if err := w.Work(context.Background(), &river.Job[RetentionCleanupArgs]{}); err != nil {
		t.Fatalf("Work() returned error: %v", err)
	}
	after := time.Now().Add(-ttl)

	if got := store.calls; len(got) != 3 || got[0] != "refresh" || got[1] != "auth" || got[2] != "events" {
		t.Fatalf("purge calls = %v, want [refresh auth events]", got)
	}
	if store.cutoff.Before(before) || store.cutoff.After(after) {
		t.Fatalf("system-events cutoff %v not within [%v, %v] (~ now - TTL)", store.cutoff, before, after)
	}
}

func TestRetentionCleanupWorker_PropagatesStoreError(t *testing.T) {
	store := &fakeRetentionStore{authErr: errors.New("db unavailable")}
	w := &RetentionCleanupWorker{store: store, systemEventsTTL: time.Hour}

	if err := w.Work(context.Background(), &river.Job[RetentionCleanupArgs]{}); err == nil {
		t.Fatal("Work() should surface the store error")
	}
}

func TestRetentionCleanupWorker_NilStoreErrors(t *testing.T) {
	w := &RetentionCleanupWorker{systemEventsTTL: time.Hour}

	if err := w.Work(context.Background(), &river.Job[RetentionCleanupArgs]{}); err == nil {
		t.Fatal("Work() should error when no retention store is configured")
	}
}
