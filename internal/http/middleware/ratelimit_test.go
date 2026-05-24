package middleware_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestAuthRateLimit_AllowsRequestsUnderLimit(t *testing.T) {
	handler := newRateLimitTestHandler(httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 2,
		Window:   time.Minute,
		Burst:    2,
	}))

	rec := postLimited(handler, "203.0.113.10")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAuthRateLimit_ReturnsAPIErrorWhenLimited(t *testing.T) {
	handler := newRateLimitTestHandler(httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	}))

	first := postLimited(handler, "203.0.113.11")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	second := postLimited(handler, "203.0.113.11")
	assertRateLimited(t, second)
}

func TestAuthRateLimit_KeysByClientIP(t *testing.T) {
	handler := newRateLimitTestHandler(httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	}))

	first := postLimited(handler, "203.0.113.12")
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	sameIP := postLimited(handler, "203.0.113.12")
	assertRateLimited(t, sameIP)

	differentIP := postLimited(handler, "203.0.113.13")
	if differentIP.Code != http.StatusOK {
		t.Fatalf("different IP status = %d, want %d; body=%s", differentIP.Code, http.StatusOK, differentIP.Body.String())
	}
}

func TestAuthRateLimit_DisabledAllowsRequests(t *testing.T) {
	handler := newRateLimitTestHandler(httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  false,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	}))

	for i := 0; i < 3; i++ {
		rec := postLimited(handler, "203.0.113.14")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d; body=%s", i+1, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestRedisRateLimiterFallsBackToLocalLimiterWhenRedisErrors(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		MaxRetries:  0,
		DialTimeout: 10 * time.Millisecond,
		ReadTimeout: 10 * time.Millisecond,
	})
	t.Cleanup(func() { _ = client.Close() })

	limiter := httpmiddleware.NewRedisRateLimiter(client, httpmiddleware.RateLimitConfig{
		Enabled:  true,
		Requests: 1,
		Window:   time.Minute,
		Burst:    1,
	})

	if !limiter.Allow("203.0.113.15:1234") {
		t.Fatal("first request was denied; local fallback should allow within limit")
	}
	if limiter.Allow("203.0.113.15:1234") {
		t.Fatal("second request was allowed; local fallback should enforce the limit after Redis errors")
	}
}

func newRateLimitTestHandler(limiter httpmiddleware.RateLimiter) http.Handler {
	router := chi.NewRouter()
	router.Use(chimiddleware.RequestID)
	router.Use(chimiddleware.RealIP)

	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	huma.Register(api, huma.Operation{
		OperationID: "post-limited",
		Method:      http.MethodPost,
		Path:        "/limited",
		Middlewares: huma.Middlewares{httpmiddleware.AuthRateLimit(limiter)},
	}, func(context.Context, *struct{}) (*rateLimitTestResponse, error) {
		resp := &rateLimitTestResponse{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	return router
}

type rateLimitTestResponse struct {
	Body struct {
		Status string `json:"status"`
	}
}

func postLimited(handler http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/limited", nil)
	req.Header.Set("X-Real-IP", ip)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertRateLimited(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header is empty")
	}

	var body apierror.ResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body %q: %v", rec.Body.String(), err)
	}
	if body.Code != apierror.CodeRateLimited {
		t.Errorf("code = %q, want %q", body.Code, apierror.CodeRateLimited)
	}
	if body.Message != "Too many authentication attempts. Please try again later." {
		t.Errorf("message = %q, want safe rate limit message", body.Message)
	}
	if body.RequestID == "" {
		t.Error("request_id is empty")
	}
}
