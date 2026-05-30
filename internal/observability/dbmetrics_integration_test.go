//go:build integration

package observability_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/aklmans/wow-dashboard-api/internal/observability"
	"github.com/aklmans/wow-dashboard-api/internal/store/storetest"
)

// TestDBMetricsCollectorsIntegration verifies the pgxpool collector exports pool
// gauges against a live pool, and that the River collector degrades gracefully
// (emits nothing) when the river_job table is absent — it is created by River's
// own migration tool, not the goose migrations the test container runs.
func TestDBMetricsCollectorsIntegration(t *testing.T) {
	ctx := context.Background()
	pool := storetest.NewPostgresPool(t, ctx, "test_dbmetrics_db", "../../migrations")

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		observability.NewPgxPoolCollector(pool),
		observability.NewRiverQueueCollector(pool, nil),
	)

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	present := map[string]bool{}
	for _, f := range families {
		present[f.GetName()] = true
	}

	for _, want := range []string{"db_pool_connections", "db_pool_acquire_total", "db_pool_empty_acquire_total"} {
		if !present[want] {
			t.Errorf("metric %q was not exported", want)
		}
	}
	if present["river_jobs"] {
		t.Error("river_jobs should be absent when the river_job table does not exist")
	}
}
