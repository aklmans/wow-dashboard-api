package middleware

import (
	"encoding/json"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/time/rate"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

const authRateLimitMessage = "Too many authentication attempts. Please try again later."

// RateLimitConfig configures a single-instance token bucket rate limiter.
type RateLimitConfig struct {
	Enabled  bool
	Requests int
	Window   time.Duration
	Burst    int
}

type rateLimitClient struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter limits requests independently per client IP address.
type IPRateLimiter struct {
	enabled    bool
	limit      rate.Limit
	burst      int
	retryAfter int
	ttl        time.Duration
	now        func() time.Time

	mu        sync.Mutex
	clients   map[string]*rateLimitClient
	lastPrune time.Time
}

// NewIPRateLimiter returns an in-memory token bucket limiter keyed by client IP.
func NewIPRateLimiter(cfg RateLimitConfig) *IPRateLimiter {
	if cfg.Requests <= 0 {
		cfg.Requests = 1
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.Burst <= 0 {
		cfg.Burst = cfg.Requests
	}

	return &IPRateLimiter{
		enabled:    cfg.Enabled,
		limit:      rate.Limit(float64(cfg.Requests) / cfg.Window.Seconds()),
		burst:      cfg.Burst,
		retryAfter: retryAfterSeconds(cfg.Requests, cfg.Window),
		ttl:        maxDuration(2*cfg.Window, time.Minute),
		now:        time.Now,
		clients:    make(map[string]*rateLimitClient),
	}
}

// Allow reports whether a request from remoteAddr may proceed.
func (l *IPRateLimiter) Allow(remoteAddr string) bool {
	if l == nil || !l.enabled {
		return true
	}

	key := clientIPKey(remoteAddr)
	now := l.now()

	l.mu.Lock()
	// Pruning scans the whole client map, so throttle it to at most once per
	// ttl instead of running it on every request. This keeps the hot path off
	// an O(n) sweep and removes an IP-rotation amplification vector.
	if now.Sub(l.lastPrune) >= l.ttl {
		l.pruneLocked(now)
		l.lastPrune = now
	}
	client := l.clients[key]
	if client == nil {
		client = &rateLimitClient{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.clients[key] = client
	}
	client.lastSeen = now
	limiter := client.limiter
	l.mu.Unlock()

	return limiter.Allow()
}

// RetryAfterSeconds returns a conservative Retry-After value for limited clients.
func (l *IPRateLimiter) RetryAfterSeconds() int {
	if l == nil || l.retryAfter <= 0 {
		return 1
	}
	return l.retryAfter
}

func (l *IPRateLimiter) pruneLocked(now time.Time) {
	for key, client := range l.clients {
		if now.Sub(client.lastSeen) > l.ttl {
			delete(l.clients, key)
		}
	}
}

// AuthRateLimit returns a Huma operation middleware for auth-sensitive routes.
func AuthRateLimit(limiter *IPRateLimiter) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if limiter == nil || limiter.Allow(ctx.RemoteAddr()) {
			next(ctx)
			return
		}

		err := apierror.RateLimited(authRateLimitMessage).
			WithRequestID(apierror.RequestIDFromContext(ctx.Context()))
		ctx.SetStatus(http.StatusTooManyRequests)
		ctx.SetHeader("Content-Type", "application/json")
		ctx.SetHeader("Retry-After", strconv.Itoa(limiter.RetryAfterSeconds()))
		_ = json.NewEncoder(ctx.BodyWriter()).Encode(err.Body())
	}
}

func clientIPKey(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		remoteAddr = host
	}
	if ip := net.ParseIP(remoteAddr); ip != nil {
		return ip.String()
	}
	return remoteAddr
}

func retryAfterSeconds(requests int, window time.Duration) int {
	seconds := int(math.Ceil(window.Seconds() / float64(requests)))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
