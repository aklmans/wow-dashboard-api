package middleware

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisRateLimitKeyPrefix = "ratelimit:auth:"
	// redisRateLimitTimeout bounds the Redis round trip so a slow or
	// unreachable Redis cannot stall an auth request.
	redisRateLimitTimeout = 200 * time.Millisecond
)

// redisRateLimitScript atomically increments a fixed-window counter and sets
// its expiry on the first hit. Running INCR and EXPIRE as one Lua script means
// the key can never be left without a TTL.
var redisRateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// RedisRateLimiter is a fixed-window, per-IP limiter backed by Redis, so the
// limit is shared across every API instance pointed at the same Redis.
type RedisRateLimiter struct {
	client     *redis.Client
	fallback   *IPRateLimiter
	enabled    bool
	limit      int64
	window     time.Duration
	retryAfter int
}

// NewRedisRateLimiter returns a Redis-backed limiter allowing cfg.Requests
// requests per cfg.Window per client IP.
func NewRedisRateLimiter(client *redis.Client, cfg RateLimitConfig) *RedisRateLimiter {
	if cfg.Requests <= 0 {
		cfg.Requests = 1
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	return &RedisRateLimiter{
		client:     client,
		fallback:   NewIPRateLimiter(cfg),
		enabled:    cfg.Enabled,
		limit:      int64(cfg.Requests),
		window:     cfg.Window,
		retryAfter: retryAfterSeconds(cfg.Requests, cfg.Window),
	}
}

// Allow reports whether a request from remoteAddr may proceed. If Redis is
// unreachable at runtime, the limiter falls back to the local per-IP limiter
// instead of failing open.
func (l *RedisRateLimiter) Allow(remoteAddr string) bool {
	if l == nil || !l.enabled {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisRateLimitTimeout)
	defer cancel()

	key := redisRateLimitKeyPrefix + clientIPKey(remoteAddr)
	count, err := redisRateLimitScript.Run(ctx, l.client, []string{key}, int(l.window.Seconds())).Int64()
	if err != nil {
		return l.fallback.Allow(remoteAddr)
	}
	return count <= l.limit
}

// RetryAfterSeconds returns a conservative Retry-After value for limited clients.
func (l *RedisRateLimiter) RetryAfterSeconds() int {
	if l == nil || l.retryAfter <= 0 {
		return 1
	}
	return l.retryAfter
}
