package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestNewAPIDocsToggle(t *testing.T) {
	docsStatus := func(docsEnabled bool) int {
		router := chi.NewRouter()
		_ = NewAPI(router, docsEnabled)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
		return rec.Code
	}

	if code := docsStatus(true); code == http.StatusNotFound {
		t.Fatalf("/docs should be served when docs are enabled, got %d", code)
	}
	if code := docsStatus(false); code != http.StatusNotFound {
		t.Fatalf("/docs should 404 when docs are disabled, got %d", code)
	}
}

type closeFuncServer struct {
	called bool
	err    error
}

func (s *closeFuncServer) Close() error {
	s.called = true
	return s.err
}

func TestForceCloseAfterShutdownTimeoutClosesServerAndPreservesShutdownError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := &closeFuncServer{}

	err := forceCloseAfterShutdownTimeout(logger, server, context.DeadlineExceeded)

	if !server.called {
		t.Fatal("server.Close was not called")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "shutdown timeout reached; forced close starting") {
		t.Fatalf("logs = %s, want forced close starting message", logOutput)
	}
	if !strings.Contains(logOutput, "forced close completed") {
		t.Fatalf("logs = %s, want forced close completed message", logOutput)
	}
}

func TestForceCloseAfterShutdownTimeoutReturnsCloseErrorWithoutDroppingShutdownError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	closeErr := errors.New("close failed")
	server := &closeFuncServer{err: closeErr}

	err := forceCloseAfterShutdownTimeout(logger, server, context.DeadlineExceeded)

	if !server.called {
		t.Fatal("server.Close was not called")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want close error", err)
	}
	if logOutput := logs.String(); !strings.Contains(logOutput, "forced close failed") {
		t.Fatalf("logs = %s, want forced close failed message", logOutput)
	}
}

func TestNewAuthRateLimiterFallsBackToLocalLimiterWhenRedisPingFails(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	limiter, cleanup, err := newAuthRateLimiter(context.Background(), "redis://127.0.0.1:1/0", httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	}, logger)
	if err != nil {
		t.Fatalf("newAuthRateLimiter returned error: %v", err)
	}
	t.Cleanup(cleanup)

	if _, ok := limiter.(*httpmiddleware.IPRateLimiter); !ok {
		t.Fatalf("limiter type = %T, want *IPRateLimiter after Redis ping failure", limiter)
	}
	if !limiter.Allow("203.0.113.16:1234") {
		t.Fatal("first request was denied; local limiter should allow within limit")
	}
	if limiter.Allow("203.0.113.16:1234") {
		t.Fatal("second request was allowed; local limiter should enforce the fallback limit")
	}
	if !strings.Contains(logs.String(), "Redis ping failed; auth rate limiting falling back to local memory") {
		t.Fatalf("logs = %s, want Redis fallback warning", logs.String())
	}
}
