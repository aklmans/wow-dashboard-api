//go:build integration

package middleware_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestRedisRateLimiterIntegration(t *testing.T) {
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Errorf("failed to terminate redis container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: host + ":" + port.Port()})
	t.Cleanup(func() { _ = client.Close() })

	limiter := middleware.NewRedisRateLimiter(client, middleware.RateLimitConfig{
		Enabled:  true,
		Requests: 3,
		Window:   time.Minute,
	})

	const clientA = "10.0.0.1:5000"
	for i := 1; i <= 3; i++ {
		if !limiter.Allow(clientA) {
			t.Fatalf("request %d denied, want allowed within the limit", i)
		}
	}
	if limiter.Allow(clientA) {
		t.Fatal("the 4th request was allowed, want denied past the limit")
	}

	// A different client IP is counted independently.
	if !limiter.Allow("10.0.0.2:5000") {
		t.Fatal("a separate client IP was limited")
	}

	// A disabled limiter always allows.
	disabled := middleware.NewRedisRateLimiter(client, middleware.RateLimitConfig{
		Enabled:  false,
		Requests: 1,
		Window:   time.Minute,
	})
	if !disabled.Allow(clientA) {
		t.Fatal("a disabled limiter denied a request")
	}

	if limiter.RetryAfterSeconds() < 1 {
		t.Fatalf("RetryAfterSeconds = %d, want >= 1", limiter.RetryAfterSeconds())
	}
}
