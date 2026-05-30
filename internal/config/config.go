package config

import (
	"fmt"
	"log/slog"
	"net/mail"
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

	// OTelExporterEndpoint is the OTLP/HTTP collector URL for distributed
	// tracing; empty leaves tracing as a no-op.
	OTelExporterEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:""`

	// SystemEventsRetentionDays bounds how long audit events are kept before the
	// background retention job purges them. Refresh/auth tokens are purged on
	// their own expiry, independent of this window.
	SystemEventsRetentionDays int `env:"SYSTEM_EVENTS_RETENTION_DAYS" envDefault:"90"`

	// JWT authentication configuration.
	JWTAccessSecret          string `env:"JWT_ACCESS_SECRET" envDefault:"dev-only-change-me-min-32-characters"`
	JWTIssuer                string `env:"JWT_ISSUER" envDefault:"wow-dashboard-api"`
	JWTAudience              string `env:"JWT_AUDIENCE" envDefault:"wow-dashboard"`
	JWTAccessTokenTTLSeconds int    `env:"JWT_ACCESS_TOKEN_TTL_SECONDS" envDefault:"900"`

	// Refresh token cookie configuration.
	// Default TTL is 90 days. Rotation acts as a sliding window — every
	// /api/auth/refresh call issues a new token expiring 90 days out, so the
	// user stays signed in as long as they open the app once per quarter.
	RefreshTokenTTLSeconds     int    `env:"REFRESH_TOKEN_TTL_SECONDS" envDefault:"7776000"`
	RefreshTokenCookieName     string `env:"REFRESH_TOKEN_COOKIE_NAME" envDefault:"wow_dashboard_refresh_token"`
	RefreshTokenCookieSecure   bool   `env:"REFRESH_TOKEN_COOKIE_SECURE" envDefault:"false"`
	RefreshTokenCookieSameSite string `env:"REFRESH_TOKEN_COOKIE_SAMESITE" envDefault:"lax"`

	// Access token cookie configuration. The access token also rides as an
	// HttpOnly cookie (Path=/) so the browser never exposes it to JS and a
	// same-site edge middleware can gate routes on its presence. The cookie's
	// MaxAge tracks the refresh-session lifetime (RefreshTokenTTL) so it survives
	// access-token expiry; the JWT inside still expires per JWTAccessTokenTTL and
	// is re-minted on refresh. Set Domain to a shared parent (e.g. ".example.com")
	// when the app and API live on different subdomains.
	AccessTokenCookieName     string `env:"ACCESS_TOKEN_COOKIE_NAME" envDefault:"wow_dashboard_access_token"`
	AccessTokenCookieSecure   bool   `env:"ACCESS_TOKEN_COOKIE_SECURE" envDefault:"false"`
	AccessTokenCookieSameSite string `env:"ACCESS_TOKEN_COOKIE_SAMESITE" envDefault:"lax"`
	AccessTokenCookieDomain   string `env:"ACCESS_TOKEN_COOKIE_DOMAIN" envDefault:""`

	// Email transport configuration. Empty EmailSMTPHost falls back to the
	// LogSender (stdout) so dev environments without a relay still work.
	EmailSMTPHost     string `env:"EMAIL_SMTP_HOST" envDefault:""`
	EmailSMTPPort     int    `env:"EMAIL_SMTP_PORT" envDefault:"0"`
	EmailSMTPUsername string `env:"EMAIL_SMTP_USERNAME" envDefault:""`
	EmailSMTPPassword string `env:"EMAIL_SMTP_PASSWORD" envDefault:""`
	EmailSMTPTLSMode  string `env:"EMAIL_SMTP_TLS" envDefault:"starttls"`
	EmailFromAddress  string `env:"EMAIL_FROM_ADDRESS" envDefault:"noreply@wow-dashboard.test"`
	EmailFromName     string `env:"EMAIL_FROM_NAME" envDefault:"WOW Dashboard"`

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

func normalizeCookieSameSite(value, envVar string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "lax", "strict", "none":
		return v, nil
	default:
		return "", fmt.Errorf("invalid %s %q: must be one of lax, strict, none", envVar, value)
	}
}

// validateCookieName enforces the HTTP cookie-name token rules: no control or
// non-ASCII characters and no separator characters.
func validateCookieName(name, envVar string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s must not be empty", envVar)
	}
	// separators: ()<>@,;:\"/[]?={} \t
	const separators = "()<>@,;:\"\\\\/[]?={} \t"
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c <= 31 || c >= 127 {
			return fmt.Errorf("%s %q contains control or non-ASCII character", envVar, name)
		}
		if strings.ContainsRune(separators, rune(c)) {
			return fmt.Errorf("%s %q contains invalid separator character %q", envVar, name, string(c))
		}
	}
	return nil
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

func validateAppBaseURL(cfg *Config) error {
	cfg.AppBaseURL = strings.TrimSpace(cfg.AppBaseURL)
	if cfg.Env != "production" {
		return nil
	}
	u, err := url.Parse(cfg.AppBaseURL)
	if err != nil {
		return fmt.Errorf("invalid APP_BASE_URL %q in production: %w", cfg.AppBaseURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("APP_BASE_URL must start with %q in production: %q", "https://", cfg.AppBaseURL)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("APP_BASE_URL must contain a valid host in production: %q", cfg.AppBaseURL)
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

// SystemEventsRetention returns the audit-event retention window as a Duration.
func (c *Config) SystemEventsRetention() time.Duration {
	return time.Duration(c.SystemEventsRetentionDays) * 24 * time.Hour
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

	sameSite, err := normalizeCookieSameSite(cfg.RefreshTokenCookieSameSite, "REFRESH_TOKEN_COOKIE_SAMESITE")
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
	if cfg.SystemEventsRetentionDays <= 0 {
		return nil, fmt.Errorf("SYSTEM_EVENTS_RETENTION_DAYS must be greater than 0")
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
	if err := validateCookieName(cfg.RefreshTokenCookieName, "REFRESH_TOKEN_COOKIE_NAME"); err != nil {
		return nil, err
	}
	if cfg.RefreshTokenCookieSameSite == "none" && !cfg.RefreshTokenCookieSecure {
		return nil, fmt.Errorf("REFRESH_TOKEN_COOKIE_SECURE must be true when SameSite is set to %q", "none")
	}

	// Access token cookie: mirror the refresh cookie's production hardening.
	accessCookieSecureConfigured := strings.TrimSpace(os.Getenv("ACCESS_TOKEN_COOKIE_SECURE")) != ""
	if cfg.Env == "production" && !accessCookieSecureConfigured {
		cfg.AccessTokenCookieSecure = true
	}
	if cfg.Env == "production" && accessCookieSecureConfigured && !cfg.AccessTokenCookieSecure {
		return nil, fmt.Errorf("ACCESS_TOKEN_COOKIE_SECURE must be true in production")
	}
	accessSameSite, err := normalizeCookieSameSite(cfg.AccessTokenCookieSameSite, "ACCESS_TOKEN_COOKIE_SAMESITE")
	if err != nil {
		return nil, err
	}
	cfg.AccessTokenCookieSameSite = accessSameSite
	if err := validateCookieName(cfg.AccessTokenCookieName, "ACCESS_TOKEN_COOKIE_NAME"); err != nil {
		return nil, err
	}
	if cfg.AccessTokenCookieSameSite == "none" && !cfg.AccessTokenCookieSecure {
		return nil, fmt.Errorf("ACCESS_TOKEN_COOKIE_SECURE must be true when SameSite is set to %q", "none")
	}
	// The access cookie (Path=/) and refresh cookie (Path=/api/auth) are both
	// sent on /api/auth/* requests; if they shared a name the longer-path refresh
	// cookie would win in the access-cookie bridge and promote the opaque refresh
	// token to a Bearer access token. Require distinct names.
	if cfg.AccessTokenCookieName == cfg.RefreshTokenCookieName {
		return nil, fmt.Errorf("ACCESS_TOKEN_COOKIE_NAME and REFRESH_TOKEN_COOKIE_NAME must be different")
	}

	// 8. CORS Validations
	if err := validateCORS(&cfg); err != nil {
		return nil, err
	}
	if err := validateAppBaseURL(&cfg); err != nil {
		return nil, err
	}

	// 9. Email Transport Validations
	cfg.EmailSMTPTLSMode = strings.ToLower(strings.TrimSpace(cfg.EmailSMTPTLSMode))
	switch cfg.EmailSMTPTLSMode {
	case "none", "starttls", "tls":
	default:
		return nil, fmt.Errorf("invalid EMAIL_SMTP_TLS %q: must be one of none, starttls, tls", cfg.EmailSMTPTLSMode)
	}
	if cfg.EmailSMTPPort < 0 || cfg.EmailSMTPPort > 65535 {
		return nil, fmt.Errorf("EMAIL_SMTP_PORT must be between 0 and 65535")
	}
	if cfg.Env == "production" && strings.TrimSpace(cfg.EmailSMTPHost) == "" {
		return nil, fmt.Errorf("EMAIL_SMTP_HOST must not be empty in production")
	}
	if strings.TrimSpace(cfg.EmailFromAddress) == "" {
		return nil, fmt.Errorf("EMAIL_FROM_ADDRESS must not be empty")
	}
	cfg.EmailFromAddress = strings.TrimSpace(cfg.EmailFromAddress)
	if _, err := mail.ParseAddress(cfg.EmailFromAddress); err != nil {
		return nil, fmt.Errorf("invalid EMAIL_FROM_ADDRESS %q: %w", cfg.EmailFromAddress, err)
	}

	return &cfg, nil
}
