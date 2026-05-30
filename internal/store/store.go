package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SanitizeDatabaseURL masks the credential password from a PostgreSQL connection string.
// If the string cannot be parsed as a URL, it checks if it is in key-value DSN format and masks the password.
func SanitizeDatabaseURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		// Fallback: search and mask password in case it's in a DSN format e.g. "host=... password=..."
		return maskDSN(rawURL)
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxxx")
		}
	}
	return u.String()
}

func maskDSN(dsn string) string {
	parts := strings.Split(dsn, " ")
	for i, part := range parts {
		if strings.HasPrefix(part, "password=") {
			parts[i] = "password=xxxxxx"
		}
	}
	return strings.Join(parts, " ")
}

// NewPool initializes a pgxpool.Pool configured based on runtime configuration.
// It parses the config.DatabaseURL, overrides pool configurations, and pings the database to ensure connectivity.
func NewPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("database connection URL (DATABASE_URL) is empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		sanitized := SanitizeDatabaseURL(cfg.DatabaseURL)
		return nil, fmt.Errorf("failed to parse database URL %q: %w", sanitized, err)
	}

	// Apply configuration options
	poolCfg.MaxConns = int32(cfg.DBMaxConns)
	poolCfg.MinConns = int32(cfg.DBMinConns)
	poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime()
	poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime()

	// How often the pool probes idle connections (prune dead ones, top up to
	// MinConns). Zero leaves the pgxpool default (1 minute) in place, which keeps
	// existing callers that build a Config literal without this field working.
	if period := cfg.DBHealthCheckPeriod(); period > 0 {
		poolCfg.HealthCheckPeriod = period
	}

	// Bound every statement server-side so a stuck query cannot pin a pooled
	// connection forever. Sent as a startup runtime parameter so every connection
	// in the pool inherits it. PostgreSQL reads a bare integer as milliseconds;
	// zero leaves the server default (no timeout).
	if timeout := cfg.DBStatementTimeout(); timeout > 0 {
		if poolCfg.ConnConfig.RuntimeParams == nil {
			poolCfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(timeout.Milliseconds(), 10)
	}

	// Trace database queries; the tracer is a no-op until tracing is enabled.
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		sanitized := SanitizeDatabaseURL(cfg.DatabaseURL)
		return nil, fmt.Errorf("failed to create connection pool to %q: %w", sanitized, err)
	}

	// Perform Ping check with configuration timeout
	pingCtx, cancel := context.WithTimeout(ctx, cfg.DBHealthTimeout())
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		sanitized := SanitizeDatabaseURL(cfg.DatabaseURL)
		return nil, fmt.Errorf("failed to ping database at %q: %w", sanitized, err)
	}

	return pool, nil
}
