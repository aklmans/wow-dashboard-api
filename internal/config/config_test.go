package config

import (
	"log/slog"
	"testing"
	"time"
)

// configEnvKeys lists every environment variable read by Config.
// Used by clearConfigEnv to isolate tests from host environment.
var configEnvKeys = []string{
	"APP_NAME",
	"APP_BASE_URL",
	"PORT",
	"ENV",
	"LOG_FORMAT",
	"LOG_LEVEL",
	"READ_TIMEOUT_SECONDS",
	"WRITE_TIMEOUT_SECONDS",
	"IDLE_TIMEOUT_SECONDS",
	"HTTP_SHUTDOWN_TIMEOUT_SECONDS",
	"SHUTDOWN_TIMEOUT_SECONDS",
	"CORS_ALLOWED_ORIGINS",
	"DATABASE_URL",
	"DB_MAX_CONNS",
	"DB_MIN_CONNS",
	"DB_MAX_CONN_LIFETIME_SECONDS",
	"DB_MAX_CONN_IDLE_TIME_SECONDS",
	"DB_HEALTH_TIMEOUT_SECONDS",
	"AUTH_RATE_LIMIT_ENABLED",
	"AUTH_RATE_LIMIT_REQUESTS",
	"AUTH_RATE_LIMIT_WINDOW_SECONDS",
	"AUTH_RATE_LIMIT_BURST",
	"JWT_ACCESS_SECRET",
	"JWT_ISSUER",
	"JWT_AUDIENCE",
	"JWT_ACCESS_TOKEN_TTL_SECONDS",
	"REFRESH_TOKEN_TTL_SECONDS",
	"REFRESH_TOKEN_COOKIE_NAME",
	"REFRESH_TOKEN_COOKIE_SECURE",
	"REFRESH_TOKEN_COOKIE_SAMESITE",
	"ACCESS_TOKEN_COOKIE_NAME",
	"ACCESS_TOKEN_COOKIE_SECURE",
	"ACCESS_TOKEN_COOKIE_SAMESITE",
	"ACCESS_TOKEN_COOKIE_DOMAIN",
	"SYSTEM_EVENTS_RETENTION_DAYS",
	"EMAIL_SMTP_HOST",
	"EMAIL_SMTP_PORT",
	"EMAIL_SMTP_USERNAME",
	"EMAIL_SMTP_PASSWORD",
	"EMAIL_SMTP_TLS",
	"EMAIL_FROM_ADDRESS",
	"EMAIL_FROM_NAME",
}

// clearConfigEnv unsets all config-related environment variables for the
// duration of the test, ensuring test isolation from host environment.
// Uses t.Setenv so values are automatically restored after each test.
func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

// setProductionMinima populates the env vars that Load() requires whenever
// ENV=production but that are unrelated to a given test's subject. Call after
// clearConfigEnv so the test only has to express its own production overrides.
func setProductionMinima(t *testing.T) {
	t.Helper()
	t.Setenv("APP_BASE_URL", "https://app.example.com")
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("EMAIL_SMTP_HOST", "smtp.example.test")
}

// --- Default values ---

func TestLoad_Defaults(t *testing.T) {
	clearConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AppName != "wow-dashboard-api" {
		t.Errorf("AppName = %q, want %q", cfg.AppName, "wow-dashboard-api")
	}
	if cfg.Port != 7272 {
		t.Errorf("Port = %d, want %d", cfg.Port, 7272)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, want %q", cfg.Env, "development")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
	if cfg.SlogLevel() != slog.LevelInfo {
		t.Errorf("SlogLevel() = %v, want %v", cfg.SlogLevel(), slog.LevelInfo)
	}
	if cfg.ReadTimeoutSeconds != 15 {
		t.Errorf("ReadTimeoutSeconds = %d, want %d", cfg.ReadTimeoutSeconds, 15)
	}
	if cfg.WriteTimeoutSeconds != 15 {
		t.Errorf("WriteTimeoutSeconds = %d, want %d", cfg.WriteTimeoutSeconds, 15)
	}
	if cfg.IdleTimeoutSeconds != 60 {
		t.Errorf("IdleTimeoutSeconds = %d, want %d", cfg.IdleTimeoutSeconds, 60)
	}
	if cfg.HTTPShutdownTimeoutSeconds != 10 {
		t.Errorf("HTTPShutdownTimeoutSeconds = %d, want %d", cfg.HTTPShutdownTimeoutSeconds, 10)
	}
	wantCORS := []string{
		"http://localhost:3000",
		"http://localhost:5173",
		"http://localhost:8082",
		"http://localhost:8083",
		"http://localhost:8084",
		"http://localhost:8085",
	}
	if len(cfg.CORS) != len(wantCORS) {
		t.Fatalf("CORS length = %d, want %d", len(cfg.CORS), len(wantCORS))
	}
	for i, want := range wantCORS {
		if cfg.CORS[i] != want {
			t.Errorf("CORS[%d] = %q, want %q", i, cfg.CORS[i], want)
		}
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty string", cfg.DatabaseURL)
	}
	if cfg.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want 10", cfg.DBMaxConns)
	}
	if cfg.DBMinConns != 1 {
		t.Errorf("DBMinConns = %d, want 1", cfg.DBMinConns)
	}
	if cfg.DBMaxConnLifetimeSeconds != 1800 {
		t.Errorf("DBMaxConnLifetimeSeconds = %d, want 1800", cfg.DBMaxConnLifetimeSeconds)
	}
	if cfg.DBMaxConnIdleTimeSeconds != 300 {
		t.Errorf("DBMaxConnIdleTimeSeconds = %d, want 300", cfg.DBMaxConnIdleTimeSeconds)
	}
	if cfg.DBHealthTimeoutSeconds != 3 {
		t.Errorf("DBHealthTimeoutSeconds = %d, want 3", cfg.DBHealthTimeoutSeconds)
	}
	if !cfg.AuthRateLimitEnabled {
		t.Error("AuthRateLimitEnabled = false, want true")
	}
	if cfg.AuthRateLimitRequests != 10 {
		t.Errorf("AuthRateLimitRequests = %d, want 10", cfg.AuthRateLimitRequests)
	}
	if cfg.AuthRateLimitWindowSeconds != 60 {
		t.Errorf("AuthRateLimitWindowSeconds = %d, want 60", cfg.AuthRateLimitWindowSeconds)
	}
	if cfg.AuthRateLimitBurst != 5 {
		t.Errorf("AuthRateLimitBurst = %d, want 5", cfg.AuthRateLimitBurst)
	}
	if cfg.JWTAccessSecret != "dev-only-change-me-min-32-characters" {
		t.Errorf("JWTAccessSecret = %q, want default dev secret", cfg.JWTAccessSecret)
	}
	if cfg.JWTIssuer != "wow-dashboard-api" {
		t.Errorf("JWTIssuer = %q, want %q", cfg.JWTIssuer, "wow-dashboard-api")
	}
	if cfg.JWTAudience != "wow-dashboard" {
		t.Errorf("JWTAudience = %q, want %q", cfg.JWTAudience, "wow-dashboard")
	}
	if cfg.JWTAccessTokenTTLSeconds != 900 {
		t.Errorf("JWTAccessTokenTTLSeconds = %d, want 900", cfg.JWTAccessTokenTTLSeconds)
	}
	if cfg.RefreshTokenTTLSeconds != 7776000 {
		t.Errorf("RefreshTokenTTLSeconds = %d, want 7776000", cfg.RefreshTokenTTLSeconds)
	}
	if cfg.RefreshTokenCookieName != "wow_dashboard_refresh_token" {
		t.Errorf("RefreshTokenCookieName = %q, want wow_dashboard_refresh_token", cfg.RefreshTokenCookieName)
	}
	if cfg.RefreshTokenCookieSecure {
		t.Error("RefreshTokenCookieSecure = true, want false in development")
	}
	if cfg.RefreshTokenCookieSameSite != "lax" {
		t.Errorf("RefreshTokenCookieSameSite = %q, want lax", cfg.RefreshTokenCookieSameSite)
	}
	if cfg.AccessTokenCookieName != "wow_dashboard_access_token" {
		t.Errorf("AccessTokenCookieName = %q, want wow_dashboard_access_token", cfg.AccessTokenCookieName)
	}
	if cfg.AccessTokenCookieSecure {
		t.Error("AccessTokenCookieSecure = true, want false in development")
	}
	if cfg.AccessTokenCookieSameSite != "lax" {
		t.Errorf("AccessTokenCookieSameSite = %q, want lax", cfg.AccessTokenCookieSameSite)
	}
	if cfg.SystemEventsRetentionDays != 90 {
		t.Errorf("SystemEventsRetentionDays = %d, want 90", cfg.SystemEventsRetentionDays)
	}
}

// --- Environment variable overrides ---

func TestLoad_EnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("APP_NAME", "test-api")
	t.Setenv("APP_BASE_URL", "https://frontend.example.test")
	t.Setenv("PORT", "9090")
	t.Setenv("ENV", "production")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("READ_TIMEOUT_SECONDS", "30")
	t.Setenv("WRITE_TIMEOUT_SECONDS", "45")
	t.Setenv("IDLE_TIMEOUT_SECONDS", "120")
	t.Setenv("HTTP_SHUTDOWN_TIMEOUT_SECONDS", "20")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://example.com")
	t.Setenv("DATABASE_URL", "postgres://override")
	t.Setenv("DB_MAX_CONNS", "20")
	t.Setenv("DB_MIN_CONNS", "5")
	t.Setenv("DB_MAX_CONN_LIFETIME_SECONDS", "3600")
	t.Setenv("DB_MAX_CONN_IDLE_TIME_SECONDS", "600")
	t.Setenv("DB_HEALTH_TIMEOUT_SECONDS", "10")
	t.Setenv("AUTH_RATE_LIMIT_ENABLED", "false")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "20")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW_SECONDS", "120")
	t.Setenv("AUTH_RATE_LIMIT_BURST", "8")
	t.Setenv("JWT_ACCESS_SECRET", "override-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("JWT_ISSUER", "custom-issuer")
	t.Setenv("JWT_AUDIENCE", "custom-audience")
	t.Setenv("JWT_ACCESS_TOKEN_TTL_SECONDS", "3600")
	t.Setenv("REFRESH_TOKEN_TTL_SECONDS", "604800")
	t.Setenv("REFRESH_TOKEN_COOKIE_NAME", "custom_refresh")
	t.Setenv("REFRESH_TOKEN_COOKIE_SECURE", "true")
	t.Setenv("REFRESH_TOKEN_COOKIE_SAMESITE", "strict")
	t.Setenv("EMAIL_SMTP_HOST", "smtp.override.test")
	t.Setenv("EMAIL_SMTP_PORT", "2525")
	t.Setenv("EMAIL_SMTP_USERNAME", "override-user")
	t.Setenv("EMAIL_SMTP_PASSWORD", "override-pass")
	t.Setenv("EMAIL_SMTP_TLS", "tls")
	t.Setenv("EMAIL_FROM_ADDRESS", "override@example.test")
	t.Setenv("EMAIL_FROM_NAME", "Override Sender")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AppName != "test-api" {
		t.Errorf("AppName = %q, want %q", cfg.AppName, "test-api")
	}
	if cfg.AppBaseURL != "https://frontend.example.test" {
		t.Errorf("AppBaseURL = %q, want %q", cfg.AppBaseURL, "https://frontend.example.test")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want %d", cfg.Port, 9090)
	}
	if cfg.Env != "production" {
		t.Errorf("Env = %q, want %q", cfg.Env, "production")
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
	if cfg.ReadTimeoutSeconds != 30 {
		t.Errorf("ReadTimeoutSeconds = %d, want %d", cfg.ReadTimeoutSeconds, 30)
	}
	if cfg.WriteTimeoutSeconds != 45 {
		t.Errorf("WriteTimeoutSeconds = %d, want %d", cfg.WriteTimeoutSeconds, 45)
	}
	if cfg.IdleTimeoutSeconds != 120 {
		t.Errorf("IdleTimeoutSeconds = %d, want %d", cfg.IdleTimeoutSeconds, 120)
	}
	if cfg.HTTPShutdownTimeoutSeconds != 20 {
		t.Errorf("HTTPShutdownTimeoutSeconds = %d, want %d", cfg.HTTPShutdownTimeoutSeconds, 20)
	}
	if cfg.DatabaseURL != "postgres://override" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://override")
	}
	if cfg.DBMaxConns != 20 {
		t.Errorf("DBMaxConns = %d, want %d", cfg.DBMaxConns, 20)
	}
	if cfg.DBMinConns != 5 {
		t.Errorf("DBMinConns = %d, want %d", cfg.DBMinConns, 5)
	}
	if cfg.DBMaxConnLifetimeSeconds != 3600 {
		t.Errorf("DBMaxConnLifetimeSeconds = %d, want %d", cfg.DBMaxConnLifetimeSeconds, 3600)
	}
	if cfg.DBMaxConnIdleTimeSeconds != 600 {
		t.Errorf("DBMaxConnIdleTimeSeconds = %d, want %d", cfg.DBMaxConnIdleTimeSeconds, 600)
	}
	if cfg.DBHealthTimeoutSeconds != 10 {
		t.Errorf("DBHealthTimeoutSeconds = %d, want %d", cfg.DBHealthTimeoutSeconds, 10)
	}
	if cfg.AuthRateLimitEnabled {
		t.Error("AuthRateLimitEnabled = true, want false")
	}
	if cfg.AuthRateLimitRequests != 20 {
		t.Errorf("AuthRateLimitRequests = %d, want 20", cfg.AuthRateLimitRequests)
	}
	if cfg.AuthRateLimitWindowSeconds != 120 {
		t.Errorf("AuthRateLimitWindowSeconds = %d, want 120", cfg.AuthRateLimitWindowSeconds)
	}
	if cfg.AuthRateLimitBurst != 8 {
		t.Errorf("AuthRateLimitBurst = %d, want 8", cfg.AuthRateLimitBurst)
	}
	if cfg.JWTAccessSecret != "override-token-signing-key-value-at-least-32-chars!!" {
		t.Errorf("JWTAccessSecret = %q, want override value", cfg.JWTAccessSecret)
	}
	if cfg.JWTIssuer != "custom-issuer" {
		t.Errorf("JWTIssuer = %q, want %q", cfg.JWTIssuer, "custom-issuer")
	}
	if cfg.JWTAudience != "custom-audience" {
		t.Errorf("JWTAudience = %q, want %q", cfg.JWTAudience, "custom-audience")
	}
	if cfg.JWTAccessTokenTTLSeconds != 3600 {
		t.Errorf("JWTAccessTokenTTLSeconds = %d, want 3600", cfg.JWTAccessTokenTTLSeconds)
	}
	if cfg.RefreshTokenTTLSeconds != 604800 {
		t.Errorf("RefreshTokenTTLSeconds = %d, want 604800", cfg.RefreshTokenTTLSeconds)
	}
	if cfg.RefreshTokenCookieName != "custom_refresh" {
		t.Errorf("RefreshTokenCookieName = %q, want custom_refresh", cfg.RefreshTokenCookieName)
	}
	if !cfg.RefreshTokenCookieSecure {
		t.Error("RefreshTokenCookieSecure = false, want explicit true override")
	}
	if cfg.RefreshTokenCookieSameSite != "strict" {
		t.Errorf("RefreshTokenCookieSameSite = %q, want strict", cfg.RefreshTokenCookieSameSite)
	}
	if cfg.EmailSMTPHost != "smtp.override.test" {
		t.Errorf("EmailSMTPHost = %q, want smtp.override.test", cfg.EmailSMTPHost)
	}
	if cfg.EmailSMTPPort != 2525 {
		t.Errorf("EmailSMTPPort = %d, want 2525", cfg.EmailSMTPPort)
	}
	if cfg.EmailSMTPUsername != "override-user" {
		t.Errorf("EmailSMTPUsername = %q, want override-user", cfg.EmailSMTPUsername)
	}
	if cfg.EmailSMTPPassword != "override-pass" {
		t.Errorf("EmailSMTPPassword = %q, want override-pass", cfg.EmailSMTPPassword)
	}
	if cfg.EmailSMTPTLSMode != "tls" {
		t.Errorf("EmailSMTPTLSMode = %q, want tls", cfg.EmailSMTPTLSMode)
	}
	if cfg.EmailFromAddress != "override@example.test" {
		t.Errorf("EmailFromAddress = %q, want override@example.test", cfg.EmailFromAddress)
	}
	if cfg.EmailFromName != "Override Sender" {
		t.Errorf("EmailFromName = %q, want Override Sender", cfg.EmailFromName)
	}
}

// --- CORS parsing ---

func TestLoad_CORSMultipleOrigins(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,https://app.example.com,https://*.vercel.app")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := []string{"http://localhost:3000", "https://app.example.com", "https://*.vercel.app"}
	if len(cfg.CORS) != len(want) {
		t.Fatalf("CORS length = %d, want %d", len(cfg.CORS), len(want))
	}
	for i, v := range want {
		if cfg.CORS[i] != v {
			t.Errorf("CORS[%d] = %q, want %q", i, cfg.CORS[i], v)
		}
	}
}

func TestLoad_CORSSingleOrigin(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://only.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if len(cfg.CORS) != 1 {
		t.Fatalf("CORS length = %d, want 1", len(cfg.CORS))
	}
	if cfg.CORS[0] != "https://only.example.com" {
		t.Errorf("CORS[0] = %q, want %q", cfg.CORS[0], "https://only.example.com")
	}
}

func TestLoad_CORSWildcardForbiddenInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://*.example.com")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for wildcard CORS origin in production, got nil")
	}
}

func TestLoad_CORSExactOriginAllowedInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.CORS) != 1 || cfg.CORS[0] != "https://app.example.com" {
		t.Fatalf("CORS = %#v, want single exact production origin", cfg.CORS)
	}
}

func TestLoad_AppBaseURLRequiresHTTPSInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("APP_BASE_URL", "http://app.example.com")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should reject http APP_BASE_URL in production, got nil")
	}
}

func TestLoad_ValidProductionConfigPasses(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("APP_BASE_URL", "https://app.example.com")
	t.Setenv("EMAIL_FROM_ADDRESS", "WOW Dashboard <noreply@example.com>")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Env != "production" || cfg.AppBaseURL != "https://app.example.com" {
		t.Fatalf("production config = env %q appBaseURL %q, want production https app URL", cfg.Env, cfg.AppBaseURL)
	}
}

func TestLoad_RefreshCookieSecureDefaultsTrueInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if !cfg.RefreshTokenCookieSecure {
		t.Fatal("RefreshTokenCookieSecure = false, want true by default in production")
	}
}

func TestLoad_RefreshCookieSecureFalseRejectedInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("REFRESH_TOKEN_COOKIE_SECURE", "false")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for REFRESH_TOKEN_COOKIE_SECURE=false in production, got nil")
	}
}

func TestLoad_RefreshCookieSecureTrueAllowedInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("REFRESH_TOKEN_COOKIE_SECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if !cfg.RefreshTokenCookieSecure {
		t.Fatal("RefreshTokenCookieSecure = false, want explicit true in production")
	}
}

func TestLoad_InvalidRefreshCookieSameSite(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("REFRESH_TOKEN_COOKIE_SAMESITE", "sometimes")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for invalid REFRESH_TOKEN_COOKIE_SAMESITE, got nil")
	}
}

func TestLoad_AccessCookieValidation(t *testing.T) {
	t.Run("invalid SameSite is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ACCESS_TOKEN_COOKIE_SAMESITE", "sometimes")
		if _, err := Load(); err == nil {
			t.Fatal("Load() should reject invalid ACCESS_TOKEN_COOKIE_SAMESITE, got nil")
		}
	})

	t.Run("invalid cookie name is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ACCESS_TOKEN_COOKIE_NAME", "bad name")
		if _, err := Load(); err == nil {
			t.Fatal("Load() should reject invalid ACCESS_TOKEN_COOKIE_NAME, got nil")
		}
	})

	t.Run("SameSite none requires Secure", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ACCESS_TOKEN_COOKIE_SAMESITE", "none")
		t.Setenv("ACCESS_TOKEN_COOKIE_SECURE", "false")
		if _, err := Load(); err == nil {
			t.Fatal("Load() should reject ACCESS SameSite=none with Secure=false, got nil")
		}
	})

	t.Run("secure defaults true in production", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "production")
		setProductionMinima(t)
		t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
		t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if !cfg.AccessTokenCookieSecure {
			t.Fatal("AccessTokenCookieSecure = false, want true by default in production")
		}
	})

	t.Run("secure false rejected in production", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "production")
		setProductionMinima(t)
		t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
		t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
		t.Setenv("ACCESS_TOKEN_COOKIE_SECURE", "false")

		if _, err := Load(); err == nil {
			t.Fatal("Load() should reject ACCESS_TOKEN_COOKIE_SECURE=false in production, got nil")
		}
	})

	t.Run("must differ from refresh cookie name", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ACCESS_TOKEN_COOKIE_NAME", "wow_dashboard_session")
		t.Setenv("REFRESH_TOKEN_COOKIE_NAME", "wow_dashboard_session")
		if _, err := Load(); err == nil {
			t.Fatal("Load() should reject identical access/refresh cookie names, got nil")
		}
	})
}

// --- Duration helpers ---

func TestConfig_DurationHelpers(t *testing.T) {
	cfg := &Config{
		ReadTimeoutSeconds:         5,
		WriteTimeoutSeconds:        10,
		IdleTimeoutSeconds:         30,
		HTTPShutdownTimeoutSeconds: 7,
		DBMaxConnLifetimeSeconds:   1800,
		DBMaxConnIdleTimeSeconds:   300,
		DBHealthTimeoutSeconds:     3,
		AuthRateLimitWindowSeconds: 60,
		JWTAccessTokenTTLSeconds:   900,
		RefreshTokenTTLSeconds:     1209600,
		SystemEventsRetentionDays:  30,
	}

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"ReadTimeout", cfg.ReadTimeout(), 5 * time.Second},
		{"WriteTimeout", cfg.WriteTimeout(), 10 * time.Second},
		{"IdleTimeout", cfg.IdleTimeout(), 30 * time.Second},
		{"ShutdownTimeout", cfg.ShutdownTimeout(), 7 * time.Second},
		{"DBMaxConnLifetime", cfg.DBMaxConnLifetime(), 1800 * time.Second},
		{"DBMaxConnIdleTime", cfg.DBMaxConnIdleTime(), 300 * time.Second},
		{"DBHealthTimeout", cfg.DBHealthTimeout(), 3 * time.Second},
		{"AuthRateLimitWindow", cfg.AuthRateLimitWindow(), time.Minute},
		{"JWTAccessTokenTTL", cfg.JWTAccessTokenTTL(), 900 * time.Second},
		{"RefreshTokenTTL", cfg.RefreshTokenTTL(), 1209600 * time.Second},
		{"SystemEventsRetention", cfg.SystemEventsRetention(), 30 * 24 * time.Hour},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// --- LOG_LEVEL parsing ---

func TestLoad_LogLevelValues(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"Error", slog.LevelError},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("LOG_LEVEL", tc.input)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error for LOG_LEVEL=%q: %v", tc.input, err)
			}
			if cfg.SlogLevel() != tc.want {
				t.Errorf("SlogLevel() = %v, want %v", cfg.SlogLevel(), tc.want)
			}
		})
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LOG_LEVEL", "bad")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for invalid LOG_LEVEL, got nil")
	}
}

func TestLoad_LogFormatDefaultsByEnv(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		jwtSecret string
		want      string
	}{
		{name: "development", env: "development", want: "text"},
		{
			name:      "production",
			env:       "production",
			jwtSecret: "production-token-signing-key-value-at-least-32-chars!!",
			want:      "json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", tc.env)
			if tc.jwtSecret != "" {
				t.Setenv("JWT_ACCESS_SECRET", tc.jwtSecret)
			}
			if tc.env == "production" {
				setProductionMinima(t)
				t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}
			if cfg.LogFormat != tc.want {
				t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, tc.want)
			}
		})
	}
}

func TestLoad_LogFormatExplicitValues(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"json", "json"},
		{"JSON", "json"},
		{"text", "text"},
		{" Text ", "text"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("LOG_FORMAT", tc.input)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error for LOG_FORMAT=%q: %v", tc.input, err)
			}
			if cfg.LogFormat != tc.want {
				t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, tc.want)
			}
		})
	}
}

func TestLoad_LogFormatExplicitTextAllowedInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("LOG_FORMAT", "text")
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want explicit text", cfg.LogFormat)
	}
}

func TestLoad_InvalidLogFormat(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("LOG_FORMAT", "pretty")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for invalid LOG_FORMAT, got nil")
	}
}

func TestLoad_EnvValues(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		jwtSecret string
	}{
		{name: "default development", want: "development"},
		{name: "staging", input: "staging", want: "staging"},
		{
			name:      "production",
			input:     "production",
			want:      "production",
			jwtSecret: "production-token-signing-key-value-at-least-32-chars!!",
		},
		{
			name:      "production normalized",
			input:     " Production ",
			want:      "production",
			jwtSecret: "production-token-signing-key-value-at-least-32-chars!!",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			if tc.input != "" {
				t.Setenv("ENV", tc.input)
			}
			if tc.jwtSecret != "" {
				t.Setenv("JWT_ACCESS_SECRET", tc.jwtSecret)
			}
			if tc.want == "production" {
				setProductionMinima(t)
				t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error for ENV=%q: %v", tc.input, err)
			}
			if cfg.Env != tc.want {
				t.Errorf("Env = %q, want %q", cfg.Env, tc.want)
			}
		})
	}
}

func TestLoad_InvalidEnvValues(t *testing.T) {
	for _, env := range []string{"prod", "live", "producton"} {
		t.Run("ENV="+env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", env)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should return error for ENV=%q, got nil", env)
			}
		})
	}
}

func TestLoad_InvalidAuthRateLimitValues(t *testing.T) {
	tests := []struct {
		name string
		key  string
		val  string
	}{
		{"requests zero", "AUTH_RATE_LIMIT_REQUESTS", "0"},
		{"window zero", "AUTH_RATE_LIMIT_WINDOW_SECONDS", "0"},
		{"burst zero", "AUTH_RATE_LIMIT_BURST", "0"},
		{"requests negative", "AUTH_RATE_LIMIT_REQUESTS", "-1"},
		{"window negative", "AUTH_RATE_LIMIT_WINDOW_SECONDS", "-1"},
		{"burst negative", "AUTH_RATE_LIMIT_BURST", "-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tc.key, tc.val)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should return error for %s=%s, got nil", tc.key, tc.val)
			}
		})
	}
}

// --- Environment pollution isolation ---

func TestLoad_CORSMultipleOrigins_WithPollutedPort(t *testing.T) {
	clearConfigEnv(t)
	// Simulate host pollution: PORT is not a number, but the CORS-focused
	// test should still exercise CORS parsing without a PORT parse error
	// because clearConfigEnv wipes PORT (empty string triggers envDefault).
	// We explicitly set a valid PORT to prove isolation works.
	t.Setenv("PORT", "8080")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	want := []string{"http://localhost:3000", "https://app.example.com"}
	if len(cfg.CORS) != len(want) {
		t.Fatalf("CORS length = %d, want %d", len(cfg.CORS), len(want))
	}
	for i, v := range want {
		if cfg.CORS[i] != v {
			t.Errorf("CORS[%d] = %q, want %q", i, cfg.CORS[i], v)
		}
	}
}

// --- JWT validation ---

func TestLoad_JWTSecretTooShort(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("JWT_ACCESS_SECRET", "short")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should return error for JWT_ACCESS_SECRET shorter than 32 chars, got nil")
	}
}

func TestLoad_JWTDefaultSecretInProduction(t *testing.T) {
	tests := []string{"production", "Production", "PRODUCTION", " production ", "PRODUCTION "}
	for _, env := range tests {
		t.Run("ENV="+env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", env)
			// Default secret is set by envDefault — clearConfigEnv sets it to ""
			// which is too short, so we explicitly set the default dev secret.
			t.Setenv("JWT_ACCESS_SECRET", "dev-only-change-me-min-32-characters")

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error when using default dev JWT secret with ENV=%q, got nil", env)
			}
		})
	}
}

func TestLoad_JWTCustomSecretInProduction(t *testing.T) {
	tests := []string{"production", "Production", "PRODUCTION", " production ", "PRODUCTION "}
	for _, env := range tests {
		t.Run("ENV="+env, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", env)
			setProductionMinima(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned unexpected error for ENV=%q: %v", env, err)
			}
			if cfg.Env != "production" {
				t.Errorf("Env = %q, want production", cfg.Env)
			}
			if cfg.JWTAccessSecret != "production-token-signing-key-value-at-least-32-chars!!" {
				t.Errorf("JWTAccessSecret = %q, want production secret", cfg.JWTAccessSecret)
			}
		})
	}
}

func TestLoad_JWTTTLDurationHelper(t *testing.T) {
	cfg := &Config{
		JWTAccessTokenTTLSeconds: 3600,
	}
	want := time.Hour
	if cfg.JWTAccessTokenTTL() != want {
		t.Errorf("JWTAccessTokenTTL() = %v, want %v", cfg.JWTAccessTokenTTL(), want)
	}
}

func TestLoad_ProductionPlaceholderJWTSecrets(t *testing.T) {
	placeholders := []string{
		"this-is-a-change-me-secret-with-32-chars",
		"this-is-a-changeme-secret-with-32-chars",
		"this-is-a-dev-only-secret-with-32-chars",
		"this-is-an-example-secret-with-32-chars",
		"this-is-my-secret-key-value-with-32-chars",
		"THIS-IS-A-CHANGE-ME-SECRET-WITH-32-CHARS",
	}

	for _, secret := range placeholders {
		t.Run(secret, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", "production")
			setProductionMinima(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv("JWT_ACCESS_SECRET", secret)

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error for placeholder secret %q in production, got nil", secret)
			}
		})
	}

	// A valid non-placeholder secret should pass
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")

	_, err := Load()
	if err != nil {
		t.Errorf("Load() returned unexpected error for valid secret: %v", err)
	}
}

func TestLoad_ProductionJWTAccessTokenTTL(t *testing.T) {
	invalidTTLTests := []string{"0", "-5", "59", "3601"}
	for _, ttl := range invalidTTLTests {
		t.Run("invalid TTL in production: "+ttl, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", "production")
			setProductionMinima(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
			t.Setenv("JWT_ACCESS_TOKEN_TTL_SECONDS", ttl)

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error for TTL %s in production, got nil", ttl)
			}
		})
	}

	validTTLTests := []string{"60", "900", "3600"}
	for _, ttl := range validTTLTests {
		t.Run("valid TTL in production: "+ttl, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", "production")
			setProductionMinima(t)
			t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
			t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
			t.Setenv("JWT_ACCESS_TOKEN_TTL_SECONDS", ttl)

			_, err := Load()
			if err != nil {
				t.Errorf("Load() returned unexpected error for valid TTL %s: %v", ttl, err)
			}
		})
	}

	// In non-production, TTL should simply be > 0
	t.Run("non-production TTL <= 0", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "development")
		t.Setenv("JWT_ACCESS_TOKEN_TTL_SECONDS", "0")

		_, err := Load()
		if err == nil {
			t.Error("Load() should return error for TTL <= 0 in development, got nil")
		}
	})

	t.Run("non-production TTL > 0", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "development")
		t.Setenv("JWT_ACCESS_TOKEN_TTL_SECONDS", "10") // very low is fine in dev

		_, err := Load()
		if err != nil {
			t.Errorf("Load() returned unexpected error for TTL > 0 in development: %v", err)
		}
	})
}

func TestLoad_ProductionCORSOrigins(t *testing.T) {
	invalidCORSOrigins := []string{
		"*",
		"http://localhost:3000",
		"http://127.0.0.1:8080",
		"https://localhost:3000",
		"https://127.0.0.1",
		"https://0.0.0.0",
		"https://[::1]",
		"https://::1",
		"http://example.com",
		"https://app.example.com,http://localhost:3000",
		"https://app.example.com,,https://another.example.com",
		"https://",
		"https:// app.example.com",
		"https://app.example.com/path",
		"https://app.example.com?x=1",
		"https://app.example.com#frag",
		"https://app.example.com/",
		"https://user@app.example.com",
	}

	for _, origin := range invalidCORSOrigins {
		t.Run("invalid CORS: "+origin, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ENV", "production")
			setProductionMinima(t)
			t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
			t.Setenv("CORS_ALLOWED_ORIGINS", origin)

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error for CORS origin %q in production, got nil", origin)
			}
		})
	}

	// Valid exact production origins
	clearConfigEnv(t)
	t.Setenv("ENV", "production")
	setProductionMinima(t)
	t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://example.com,https://app.example.com,https://app.example.com:443")

	_, err := Load()
	if err != nil {
		t.Errorf("Load() returned unexpected error for valid CORS origins: %v", err)
	}
}

func TestLoad_CookieNameValidation(t *testing.T) {
	invalidCookieNames := []string{
		" ",
		"cookie name",
		"cookie;name",
		"cookie,name",
		"cookie=name",
		"cookie\nname",
		"cookieñname", // non-ASCII
		"bad/name",
		"bad:name",
		"bad[name]",
		"bad?name",
		"bad(name)",
		"bad@name",
		"bad\tname", // tab character
		"bad\\name",
		"bad\"name",
	}

	for _, name := range invalidCookieNames {
		t.Run("invalid cookie name: "+name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("REFRESH_TOKEN_COOKIE_NAME", name)

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error for cookie name %q, got nil", name)
			}
		})
	}

	// Valid cookie name should pass
	clearConfigEnv(t)
	t.Setenv("REFRESH_TOKEN_COOKIE_NAME", "valid_cookie-name.123")

	_, err := Load()
	if err != nil {
		t.Errorf("Load() returned unexpected error for valid cookie name: %v", err)
	}
}

func TestLoad_SameSiteNoneRequiresSecure(t *testing.T) {
	// SameSite=none, Secure=false should fail across all environments
	t.Run("SameSite none with Secure false fails", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "development")
		t.Setenv("REFRESH_TOKEN_COOKIE_SAMESITE", "none")
		t.Setenv("REFRESH_TOKEN_COOKIE_SECURE", "false")

		_, err := Load()
		if err == nil {
			t.Error("Load() should return error for SameSite=none with Secure=false, got nil")
		}
	})

	// SameSite=none, Secure=true should pass
	t.Run("SameSite none with Secure true passes", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "development")
		t.Setenv("REFRESH_TOKEN_COOKIE_SAMESITE", "none")
		t.Setenv("REFRESH_TOKEN_COOKIE_SECURE", "true")

		_, err := Load()
		if err != nil {
			t.Errorf("Load() returned unexpected error: %v", err)
		}
	})
}

func TestLoad_DBPoolValidation(t *testing.T) {
	tests := []struct {
		name     string
		maxConns string
		minConns string
		wantErr  bool
	}{
		{"max conns zero", "0", "1", true},
		{"max conns negative", "-1", "1", true},
		{"min conns negative", "10", "-1", true},
		{"min conns greater than max", "5", "10", true},
		{"valid equal", "10", "10", false},
		{"valid distinct", "10", "1", false},
		{"valid zero min conns", "5", "0", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("DB_MAX_CONNS", tc.maxConns)
			t.Setenv("DB_MIN_CONNS", tc.minConns)

			_, err := Load()
			if tc.wantErr && err == nil {
				t.Errorf("Load() should return error for max=%s, min=%s, got nil", tc.maxConns, tc.minConns)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Load() returned unexpected error for max=%s, min=%s: %v", tc.maxConns, tc.minConns, err)
			}
		})
	}
}

func TestLoad_TimeoutValidation(t *testing.T) {
	timeoutKeys := []string{
		"READ_TIMEOUT_SECONDS",
		"WRITE_TIMEOUT_SECONDS",
		"IDLE_TIMEOUT_SECONDS",
		"HTTP_SHUTDOWN_TIMEOUT_SECONDS",
		"DB_MAX_CONN_LIFETIME_SECONDS",
		"DB_MAX_CONN_IDLE_TIME_SECONDS",
		"DB_HEALTH_TIMEOUT_SECONDS",
		"AUTH_RATE_LIMIT_WINDOW_SECONDS",
		"REFRESH_TOKEN_TTL_SECONDS",
		"SYSTEM_EVENTS_RETENTION_DAYS",
	}

	for _, key := range timeoutKeys {
		t.Run(key+" zero", func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(key, "0")

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error when %s is 0, got nil", key)
			}
		})

		t.Run(key+" negative", func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(key, "-5")

			_, err := Load()
			if err == nil {
				t.Errorf("Load() should return error when %s is negative, got nil", key)
			}
		})
	}
}

func TestLoad_ProductionDATABASE_URL(t *testing.T) {
	t.Run("empty DATABASE_URL fails in production", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "production")
		t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
		t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")
		t.Setenv("DATABASE_URL", "")

		_, err := Load()
		if err == nil {
			t.Error("Load() should return error for empty DATABASE_URL in production, got nil")
		}
	})

	t.Run("empty DATABASE_URL passes in development", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "development")
		t.Setenv("DATABASE_URL", "")

		_, err := Load()
		if err != nil {
			t.Errorf("Load() returned unexpected error in development: %v", err)
		}
	})
}

func TestLoad_EmailTransport(t *testing.T) {
	t.Run("default TLS mode is starttls", func(t *testing.T) {
		clearConfigEnv(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if cfg.EmailSMTPTLSMode != "starttls" {
			t.Errorf("EmailSMTPTLSMode = %q, want starttls", cfg.EmailSMTPTLSMode)
		}
	})

	t.Run("invalid TLS mode is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("EMAIL_SMTP_TLS", "garbage")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() should reject invalid EMAIL_SMTP_TLS, got nil")
		}
	})

	t.Run("port outside 0..65535 is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("EMAIL_SMTP_PORT", "70000")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() should reject out-of-range EMAIL_SMTP_PORT, got nil")
		}
	})

	t.Run("empty EMAIL_FROM_ADDRESS is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("EMAIL_FROM_ADDRESS", "  ")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() should reject empty EMAIL_FROM_ADDRESS, got nil")
		}
	})

	t.Run("invalid EMAIL_FROM_ADDRESS is rejected", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("EMAIL_FROM_ADDRESS", "not an address")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() should reject invalid EMAIL_FROM_ADDRESS, got nil")
		}
	})

	t.Run("empty EMAIL_SMTP_HOST fails in production", func(t *testing.T) {
		clearConfigEnv(t)
		t.Setenv("ENV", "production")
		t.Setenv("DATABASE_URL", "postgres://test")
		t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
		t.Setenv("JWT_ACCESS_SECRET", "production-token-signing-key-value-at-least-32-chars!!")

		_, err := Load()
		if err == nil {
			t.Fatal("Load() should reject empty EMAIL_SMTP_HOST in production, got nil")
		}
	})

	t.Run("empty EMAIL_SMTP_HOST passes in development (LogSender fallback)", func(t *testing.T) {
		clearConfigEnv(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned error: %v", err)
		}
		if cfg.EmailSMTPHost != "" {
			t.Errorf("EmailSMTPHost = %q, want empty (LogSender fallback)", cfg.EmailSMTPHost)
		}
	})
}
