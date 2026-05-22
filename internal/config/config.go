package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds typed, validated configuration loaded from environment variables.
// Every field must have a sensible default so the service can start without a .env file.
type Config struct {
	// Application identity and environment stage.
	AppName string `env:"APP_NAME" envDefault:"wow-dashboard-api"`
	Port    int    `env:"PORT" envDefault:"7272"`
	Env     string `env:"ENV" envDefault:"development"`

	// AppBaseURL is the frontend base URL used to build links in transactional
	// emails (password reset, email verification).
	AppBaseURL string `env:"APP_BASE_URL" envDefault:"http://localhost:3000"`

	// Structured logging configuration.
	LogFormat string `env:"LOG_FORMAT" envDefault:""`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`

	// HTTP server timeouts in seconds.
	ReadTimeoutSeconds         int `env:"READ_TIMEOUT_SECONDS" envDefault:"15"`
	WriteTimeoutSeconds        int `env:"WRITE_TIMEOUT_SECONDS" envDefault:"15"`
	IdleTimeoutSeconds         int `env:"IDLE_TIMEOUT_SECONDS" envDefault:"60"`
	HTTPShutdownTimeoutSeconds int `env:"HTTP_SHUTDOWN_TIMEOUT_SECONDS" envDefault:"10"`

	// CORS allowed origins, comma-separated.
	CORS []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000,http://localhost:5173,http://localhost:8082,http://localhost:8083,http://localhost:8084,http://localhost:8085"`

	// Database connection configuration.
	DatabaseURL              string `env:"DATABASE_URL" envDefault:""`
	DBMaxConns               int    `env:"DB_MAX_CONNS" envDefault:"10"`
	DBMinConns               int    `env:"DB_MIN_CONNS" envDefault:"1"`
	DBMaxConnLifetimeSeconds int    `env:"DB_MAX_CONN_LIFETIME_SECONDS" envDefault:"1800"`
	DBMaxConnIdleTimeSeconds int    `env:"DB_MAX_CONN_IDLE_TIME_SECONDS" envDefault:"300"`
	DBHealthTimeoutSeconds   int    `env:"DB_HEALTH_TIMEOUT_SECONDS" envDefault:"3"`

	// Auth rate limiting configuration.
	AuthRateLimitEnabled       bool `env:"AUTH_RATE_LIMIT_ENABLED" envDefault:"true"`
	AuthRateLimitRequests      int  `env:"AUTH_RATE_LIMIT_REQUESTS" envDefault:"10"`
	AuthRateLimitWindowSeconds int  `env:"AUTH_RATE_LIMIT_WINDOW_SECONDS" envDefault:"60"`
	AuthRateLimitBurst         int  `env:"AUTH_RATE_LIMIT_BURST" envDefault:"5"`

	// RedisURL, when set, makes auth rate limiting shared across instances via
	// Redis; empty keeps the per-instance in-memory limiter.
	RedisURL string `env:"REDIS_URL" envDefault:""`

	// JWT authentication configuration.
	JWTAccessSecret          string `env:"JWT_ACCESS_SECRET" envDefault:"dev-only-change-me-min-32-characters"`
	JWTIssuer                string `env:"JWT_ISSUER" envDefault:"wow-dashboard-api"`
	JWTAudience              string `env:"JWT_AUDIENCE" envDefault:"wow-dashboard"`
	JWTAccessTokenTTLSeconds int    `env:"JWT_ACCESS_TOKEN_TTL_SECONDS" envDefault:"900"`

	// Refresh token cookie configuration.
	RefreshTokenTTLSeconds     int    `env:"REFRESH_TOKEN_TTL_SECONDS" envDefault:"1209600"`
	RefreshTokenCookieName     string `env:"REFRESH_TOKEN_COOKIE_NAME" envDefault:"wow_dashboard_refresh_token"`
	RefreshTokenCookieSecure   bool   `env:"REFRESH_TOKEN_COOKIE_SECURE" envDefault:"false"`
	RefreshTokenCookieSameSite string `env:"REFRESH_TOKEN_COOKIE_SAMESITE" envDefault:"lax"`

	// slogLevel is the parsed slog.Level, populated by Load.
	slogLevel slog.Level
}

// SlogLevel returns the parsed slog.Level for use with slog.HandlerOptions.
func (c *Config) SlogLevel() slog.Level {
	return c.slogLevel
}

// parseLogLevel converts a log level string to slog.Level.
// Accepted values (case-insensitive): debug, info, warn, error.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid LOG_LEVEL %q: must be one of debug, info, warn, error", s)
	}
}

func normalizeLogFormat(format, envName string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(format))
	if value == "" {
		if envName == "production" {
			return "json", nil
		}
		return "text", nil
	}
	switch value {
	case "json", "text":
		return value, nil
	default:
		return "", fmt.Errorf("invalid LOG_FORMAT %q: must be one of json, text", format)
	}
}

func normalizeEnv(s string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(s))
	switch value {
	case "development", "staging", "production":
		return value, nil
	default:
		return "", fmt.Errorf("invalid ENV %q: must be one of development, staging, production", s)
	}
}

func normalizeRefreshCookieSameSite(s string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(s))
	switch value {
	case "lax", "strict", "none":
		return value, nil
	default:
		return "", fmt.Errorf("invalid REFRESH_TOKEN_COOKIE_SAMESITE %q: must be one of lax, strict, none", s)
	}
}

func validateCORS(cfg *Config) error {
	if cfg.Env != "production" {
		return nil
	}
	for _, origin := range cfg.CORS {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain empty origins in production")
		}
		if strings.Contains(trimmed, "*") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain wildcard origins in production")
		}
		if strings.Contains(trimmed, " ") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain spaces in production: %q", origin)
		}

		u, err := url.Parse(trimmed)
		if err != nil {
			return fmt.Errorf("invalid CORS origin %q in production: %w", origin, err)
		}

		if u.Scheme != "https" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must start with %q in production: %q", "https://", origin)
		}

		if u.Hostname() == "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must contain a valid host in production: %q", origin)
		}

		if u.User != nil {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain userinfo in production: %q", origin)
		}

		if u.Path != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain a trailing slash or path in production: %q", origin)
		}

		if u.RawQuery != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain query parameters in production: %q", origin)
		}

		if u.Fragment != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain a fragment in production: %q", origin)
		}

		h := strings.ToLower(u.Hostname())
		if h == "localhost" || h == "127.0.0.1" || h == "0.0.0.0" || h == "::1" || h == "[::1]" || strings.HasPrefix(h, "127.") {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain localhost or loopback IP in production: %q", origin)
		}
	}
	return nil
}

// ReadTimeout returns the read timeout as a time.Duration.
func (c *Config) ReadTimeout() time.Duration {
	return time.Duration(c.ReadTimeoutSeconds) * time.Second
}

// WriteTimeout returns the write timeout as a time.Duration.
func (c *Config) WriteTimeout() time.Duration {
	return time.Duration(c.WriteTimeoutSeconds) * time.Second
}

// IdleTimeout returns the idle timeout as a time.Duration.
func (c *Config) IdleTimeout() time.Duration {
	return time.Duration(c.IdleTimeoutSeconds) * time.Second
}

// ShutdownTimeout returns the graceful shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.HTTPShutdownTimeoutSeconds) * time.Second
}

// DBMaxConnLifetime returns the DB connection max lifetime as a time.Duration.
func (c *Config) DBMaxConnLifetime() time.Duration {
	return time.Duration(c.DBMaxConnLifetimeSeconds) * time.Second
}

// DBMaxConnIdleTime returns the DB connection max idle time as a time.Duration.
func (c *Config) DBMaxConnIdleTime() time.Duration {
	return time.Duration(c.DBMaxConnIdleTimeSeconds) * time.Second
}

// DBHealthTimeout returns the DB health check/ping timeout as a time.Duration.
func (c *Config) DBHealthTimeout() time.Duration {
	return time.Duration(c.DBHealthTimeoutSeconds) * time.Second
}

// AuthRateLimitWindow returns the auth rate limit window as a time.Duration.
func (c *Config) AuthRateLimitWindow() time.Duration {
	return time.Duration(c.AuthRateLimitWindowSeconds) * time.Second
}

// JWTAccessTokenTTL returns the access token time-to-live as a time.Duration.
func (c *Config) JWTAccessTokenTTL() time.Duration {
	return time.Duration(c.JWTAccessTokenTTLSeconds) * time.Second
}

// RefreshTokenTTL returns the refresh token time-to-live as a time.Duration.
func (c *Config) RefreshTokenTTL() time.Duration {
	return time.Duration(c.RefreshTokenTTLSeconds) * time.Second
}

// defaultJWTSecret is the dev-only placeholder. It must never be used in production.
const defaultJWTSecret = "dev-only-change-me-min-32-characters"

// minJWTSecretLength is the minimum acceptable length for JWT signing secrets.
const minJWTSecretLength = 32

// Load parses environment variables into a typed Config and validates
// constrained fields. Returns an error for invalid values (e.g. unknown log level,
// insecure JWT secret in production).
func Load() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	envName, err := normalizeEnv(cfg.Env)
	if err != nil {
		return nil, err
	}
	cfg.Env = envName

	if strings.TrimSpace(os.Getenv("HTTP_SHUTDOWN_TIMEOUT_SECONDS")) == "" {
		if legacyRaw := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT_SECONDS")); legacyRaw != "" {
			legacy, err := strconv.Atoi(legacyRaw)
			if err != nil {
				return nil, fmt.Errorf("invalid SHUTDOWN_TIMEOUT_SECONDS %q: %w", legacyRaw, err)
			}
			cfg.HTTPShutdownTimeoutSeconds = legacy
		}
	}

	logFormat, err := normalizeLogFormat(cfg.LogFormat, cfg.Env)
	if err != nil {
		return nil, err
	}
	cfg.LogFormat = logFormat

	refreshCookieSecureConfigured := strings.TrimSpace(os.Getenv("REFRESH_TOKEN_COOKIE_SECURE")) != ""
	if cfg.Env == "production" && !refreshCookieSecureConfigured {
		cfg.RefreshTokenCookieSecure = true
	}
	if cfg.Env == "production" && refreshCookieSecureConfigured && !cfg.RefreshTokenCookieSecure {
		return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_SECURE must be true in production")
	}

	sameSite, err := normalizeRefreshCookieSameSite(cfg.RefreshTokenCookieSameSite)
	if err != nil {
		return nil, err
	}
	cfg.RefreshTokenCookieSameSite = sameSite

	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	cfg.slogLevel = level

	// 1. Database URL Production Requirement
	if cfg.Env == "production" && strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL must not be empty in production")
	}

	// 2. Timeout and Duration Validations (all must be > 0)
	if cfg.ReadTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("READ_TIMEOUT_SECONDS must be greater than 0")
	}
	if cfg.WriteTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("WRITE_TIMEOUT_SECONDS must be greater than 0")
	}
	if cfg.IdleTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("IDLE_TIMEOUT_SECONDS must be greater than 0")
	}
	if cfg.HTTPShutdownTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("HTTP_SHUTDOWN_TIMEOUT_SECONDS must be greater than 0")
	}
	if cfg.DBMaxConnLifetimeSeconds <= 0 {
		return nil, fmt.Errorf("DB_MAX_CONN_LIFETIME_SECONDS must be greater than 0")
	}
	if cfg.DBMaxConnIdleTimeSeconds <= 0 {
		return nil, fmt.Errorf("DB_MAX_CONN_IDLE_TIME_SECONDS must be greater than 0")
	}
	if cfg.DBHealthTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("DB_HEALTH_TIMEOUT_SECONDS must be greater than 0")
	}
	if cfg.AuthRateLimitWindowSeconds <= 0 {
		return nil, fmt.Errorf("AUTH_RATE_LIMIT_WINDOW_SECONDS must be greater than 0")
	}
	if cfg.RefreshTokenTTLSeconds <= 0 {
		return nil, fmt.Errorf("REFRESH_TOKEN_TTL_SECONDS must be greater than 0")
	}

	// 3. DB Pool Validations
	if cfg.DBMaxConns <= 0 {
		return nil, fmt.Errorf("DB_MAX_CONNS must be greater than 0")
	}
	if cfg.DBMinConns < 0 {
		return nil, fmt.Errorf("DB_MIN_CONNS must be greater than or equal to 0")
	}
	if cfg.DBMinConns > cfg.DBMaxConns {
		return nil, fmt.Errorf("DB_MIN_CONNS must be less than or equal to DB_MAX_CONNS")
	}

	// 4. Rate Limiter Constraints
	if cfg.AuthRateLimitRequests <= 0 {
		return nil, fmt.Errorf("AUTH_RATE_LIMIT_REQUESTS must be greater than 0")
	}
	if cfg.AuthRateLimitBurst <= 0 {
		return nil, fmt.Errorf("AUTH_RATE_LIMIT_BURST must be greater than 0")
	}

	// 5. JWT Secret Constraints
	if len(cfg.JWTAccessSecret) < minJWTSecretLength {
		return nil, fmt.Errorf("JWT_ACCESS_SECRET must be at least %d characters", minJWTSecretLength)
	}
	if cfg.Env == "production" {
		if cfg.JWTAccessSecret == defaultJWTSecret {
			return nil, fmt.Errorf("JWT_ACCESS_SECRET must not use the default dev secret in production")
		}
		secretLower := strings.ToLower(cfg.JWTAccessSecret)
		placeholders := []string{"change-me", "changeme", "dev-only", "example", "secret"}
		for _, p := range placeholders {
			if strings.Contains(secretLower, p) {
				return nil, fmt.Errorf("JWT_ACCESS_SECRET must not contain common placeholder %q in production", p)
			}
		}
	}

	// 6. JWT Access Token TTL Constraints
	if cfg.Env == "production" {
		if cfg.JWTAccessTokenTTLSeconds < 60 || cfg.JWTAccessTokenTTLSeconds > 3600 {
			return nil, fmt.Errorf("JWT_ACCESS_TOKEN_TTL_SECONDS must be between 60 and 3600 seconds in production")
		}
	} else {
		if cfg.JWTAccessTokenTTLSeconds <= 0 {
			return nil, fmt.Errorf("JWT_ACCESS_TOKEN_TTL_SECONDS must be greater than 0")
		}
	}

	// 7. Cookie Validations
	if strings.TrimSpace(cfg.RefreshTokenCookieName) == "" {
		return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_NAME must not be empty")
	}
	// Validate cookie name based on HTTP cookie token rules
	// separators: ()<>@,;:\"/[]?={} \t
	separators := "()<>@,;:\"\\\\/[]?={} \t"
	for i := 0; i < len(cfg.RefreshTokenCookieName); i++ {
		c := cfg.RefreshTokenCookieName[i]
		if c <= 31 || c >= 127 {
			return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_NAME %q contains control or non-ASCII character", cfg.RefreshTokenCookieName)
		}
		if strings.ContainsRune(separators, rune(c)) {
			return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_NAME %q contains invalid separator character %q", cfg.RefreshTokenCookieName, string(c))
		}
	}

	if cfg.RefreshTokenCookieSameSite == "none" && !cfg.RefreshTokenCookieSecure {
		return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_SECURE must be true when SameSite is set to %q", "none")
	}

	// 8. CORS Validations
	if err := validateCORS(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
