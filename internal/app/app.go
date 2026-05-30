package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/email"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
	"github.com/aklmans/wow-dashboard-api/internal/jobs"
	"github.com/aklmans/wow-dashboard-api/internal/logging"
	notificationsservice "github.com/aklmans/wow-dashboard-api/internal/notifications/service"
	"github.com/aklmans/wow-dashboard-api/internal/observability"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	rolesservice "github.com/aklmans/wow-dashboard-api/internal/roles/service"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/notificationsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/projectsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/rolesrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/systemeventsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/usersrepo"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// Dependencies contains route-level use-case dependencies.
type Dependencies struct {
	AuthService             handlers.AuthService
	UsersService            handlers.UsersService
	RolesService            handlers.RolesService
	ProjectsService         handlers.ProjectsService
	SystemEventsService     handlers.SystemEventsService
	NotificationsService    handlers.NotificationsService
	RefreshCookie           handlers.RefreshCookieConfig
	AccessCookie            handlers.AccessCookieConfig
	AuthRateLimitMiddleware func(huma.Context, func(huma.Context))
	ReadyChecker            handlers.ReadyChecker
}

// RegisterRoutes registers all route handlers into the provided Huma API.
func RegisterRoutes(api huma.API, deps Dependencies) {
	handlers.RegisterHealth(api)
	handlers.RegisterReady(api, deps.ReadyChecker)
	if deps.AuthService != nil {
		var authMiddlewares []func(huma.Context, func(huma.Context))
		if deps.AuthRateLimitMiddleware != nil {
			authMiddlewares = append(authMiddlewares, deps.AuthRateLimitMiddleware)
		}
		handlers.RegisterAuthWithCookies(api, deps.AuthService, deps.RefreshCookie, deps.AccessCookie, authMiddlewares...)
	}
	if deps.AuthService != nil && deps.UsersService != nil {
		handlers.RegisterUsers(api, deps.AuthService, deps.UsersService)
	}
	if deps.AuthService != nil && deps.RolesService != nil {
		handlers.RegisterRoles(api, deps.AuthService, deps.RolesService)
	}
	if deps.AuthService != nil && deps.ProjectsService != nil {
		handlers.RegisterProjects(api, deps.AuthService, deps.ProjectsService)
	}
	if deps.AuthService != nil && deps.SystemEventsService != nil {
		handlers.RegisterSystemEvents(api, deps.AuthService, deps.SystemEventsService)
	}
	if deps.AuthService != nil && deps.NotificationsService != nil {
		handlers.RegisterNotifications(api, deps.AuthService, deps.NotificationsService)
	}
}

const (
	openAPITitle   = "WOW Dashboard API"
	openAPIVersion = "1.0.0"
)

// NewAPI creates and configures a Huma API instance on top of a Chi router.
// docsEnabled toggles the interactive Swagger UI at /docs; the OpenAPI JSON at
// /openapi is always served. The title/version/servers populate the generated
// contract.
func NewAPI(router chi.Router, docsEnabled bool) huma.API {
	humaCfg := huma.DefaultConfig(openAPITitle, openAPIVersion)
	humaCfg.Info.Description = "Production-grade admin dashboard API: authentication, users, roles, projects, and audit events."
	humaCfg.OpenAPIPath = "/openapi"
	// An empty DocsPath disables the Swagger UI; production hides the full API
	// surface by default (see config.EnableDocs).
	if docsEnabled {
		humaCfg.DocsPath = "/docs"
	} else {
		humaCfg.DocsPath = ""
	}
	// Intentionally no Servers entry: setting an absolute server URL makes Huma
	// rewrite every response `$schema` reference against it, baking a host into
	// the committed contract. The runtime /openapi already reflects the real
	// request host, so the source spec stays host-agnostic.
	// LoggingTransformer runs first so it observes the original error value
	// (with its internal cause) before HumaErrorTransformer collapses 5xx into
	// the generic client envelope. It passes nil to resolve slog.Default(),
	// which Run() has already configured via slog.SetDefault.
	humaCfg.Transformers = append(humaCfg.Transformers, apierror.LoggingTransformer(nil), apierror.HumaErrorTransformer)
	return humachi.New(router, humaCfg)
}

// Run sets up and executes the main HTTP server process with graceful shutdown orchestration.
func Run(ctx context.Context, cfg *config.Config) error {
	logger := logging.NewLogger(cfg, os.Stdout)
	slog.SetDefault(logger)

	// Distributed tracing. No-op unless OTEL_EXPORTER_OTLP_ENDPOINT is set.
	traceShutdown, err := observability.SetupTracing(ctx, cfg.AppName, cfg.OTelExporterEndpoint)
	if err != nil {
		logger.Warn("tracing setup failed; continuing without tracing", "error", err)
	} else if cfg.OTelExporterEndpoint != "" {
		logger.Info("Distributed tracing enabled", "endpoint", cfg.OTelExporterEndpoint)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := traceShutdown(flushCtx); err != nil {
			logger.Error("tracing shutdown failed", "error", err)
		}
	}()

	router := chi.NewRouter()

	// Base Standard Middlewares
	router.Use(middleware.RequestID)

	// Prometheus HTTP instrumentation, early so it times the whole request.
	metrics := httpmiddleware.NewMetrics()
	router.Use(metrics.Middleware())

	// Name the tracing span by the matched route once Chi has routed.
	router.Use(httpmiddleware.TraceRoute())

	router.Use(httpmiddleware.RequestLogger(logger))
	router.Use(middleware.Recoverer)

	// Apply our Custom CORS Middleware
	router.Use(httpmiddleware.CORS(cfg.CORS))

	// Reject oversized request bodies at the edge (after CORS so the 413 still
	// carries CORS headers the browser can read). A coarse backstop in front of
	// Huma's per-operation body cap.
	router.Use(httpmiddleware.RequestBodyLimit(cfg.RequestBodyMaxBytes))

	// Baseline security response headers; HSTS only in production (HTTPS).
	router.Use(httpmiddleware.SecurityHeaders(cfg.Env == "production"))

	// The access token rides as an ambient HttpOnly cookie, so block cross-site
	// state-changing requests (CSRF) before bridging that cookie into the
	// Authorization header every handler already reads.
	router.Use(httpmiddleware.CSRFGuard(cfg.CORS, cfg.AccessTokenCookieName, cfg.RefreshTokenCookieName))
	router.Use(httpmiddleware.AccessCookieBridge(cfg.AccessTokenCookieName))

	// Prometheus scrape endpoint, served outside the Huma JSON API. By default it
	// rides on the public router; when METRICS_ADDR is set it is served on a
	// separate internal-only listener instead (started below), keeping metrics
	// off the internet-facing port.
	if cfg.MetricsAddr == "" {
		router.Handle("/metrics", metrics.Handler())
	}

	// Configure and initialize Huma API dynamically using the shared constructor.
	// The Swagger UI at /docs is gated by config (off by default in production).
	api := NewAPI(router, cfg.EnableDocs)

	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize database pool: %w", err)
	}
	defer func() {
		pool.Close()
		logger.Info("Database pool closed")
	}()

	queries := query.New(pool)

	// Export database-pool and background-queue gauges on the metrics registry
	// now that the pool exists, so pool saturation and job backlog are scrapable.
	metrics.Register(
		observability.NewPgxPoolCollector(pool),
		observability.NewRiverQueueCollector(pool, logger),
	)

	tokenManager, err := token.NewManager(cfg.JWTAccessSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTAccessTokenTTL())
	if err != nil {
		return fmt.Errorf("initialize JWT token manager: %w", err)
	}

	// Email is queued through River so the API never blocks on SMTP. The
	// worker process picks up SendEmailArgs jobs and dispatches them via the
	// real transport (or LogSender when EMAIL_SMTP_HOST is unset).
	riverInsertClient, err := jobs.NewInsertOnlyClient(pool)
	if err != nil {
		return fmt.Errorf("initialize river insert-only client: %w", err)
	}
	// Drain the insert client before the pool closes. Defers run LIFO, so
	// registering this after the pool.Close defer above means it runs first.
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if stopErr := jobs.Stop(stopCtx, riverInsertClient); stopErr != nil && !errors.Is(stopErr, context.Canceled) {
			logger.Error("River insert client stop failed", "error", stopErr)
		}
	}()
	var emailSender email.Sender = jobs.NewAsyncEmailSender(riverInsertClient)
	if cfg.EmailSMTPHost == "" {
		logger.Warn("EMAIL_SMTP_HOST is not set; the worker will log emails to stdout instead of sending them")
	} else {
		logger.Info("API email transport: queued via River → SMTP",
			"host", cfg.EmailSMTPHost,
			"port", cfg.EmailSMTPPort,
			"tls", cfg.EmailSMTPTLSMode,
		)
	}

	authStore := authrepo.NewUserStore(queries)
	refreshTokenStore := authrepo.NewRefreshTokenStore(queries)
	authTokenStore := authrepo.NewAuthTokenStore(queries)
	unitOfWork := authrepo.NewUnitOfWork(pool)
	auditRecorder := authrepo.NewSystemEventRecorder(queries)
	authSvc := authservice.NewService(authStore, tokenManager,
		authservice.WithRefreshTokenStore(refreshTokenStore, cfg.RefreshTokenTTL()),
		authservice.WithUnitOfWork(unitOfWork),
		authservice.WithAuditRecorder(auditRecorder),
		authservice.WithAuthTokenStore(authTokenStore),
		authservice.WithEmailSender(emailSender),
		authservice.WithAppBaseURL(cfg.AppBaseURL))
	usersSvc := userservice.NewService(usersrepo.NewUserStore(pool),
		userservice.WithAuditRecorder(usersrepo.NewSystemEventRecorder(queries)),
		userservice.WithUnitOfWork(usersrepo.NewUnitOfWork(pool)))
	rolesSvc := rolesservice.NewService(rolesrepo.NewRoleStore(pool),
		rolesservice.WithAuditRecorder(rolesrepo.NewSystemEventRecorder(queries)),
		rolesservice.WithUnitOfWork(rolesrepo.NewUnitOfWork(pool)))
	projectsSvc := projectservice.NewService(projectsrepo.NewProjectStore(queries),
		projectservice.WithAuditRecorder(projectsrepo.NewSystemEventRecorder(queries)),
		projectservice.WithUnitOfWork(projectsrepo.NewUnitOfWork(pool)))
	systemEventsSvc := systemeventsservice.NewService(systemeventsrepo.NewEventStore(queries))
	notificationsSvc := notificationsservice.NewService(notificationsrepo.NewNotificationStore(queries))
	rateLimitConfig := httpmiddleware.RateLimitConfig{
		Enabled:  cfg.AuthRateLimitEnabled,
		Requests: cfg.AuthRateLimitRequests,
		Window:   cfg.AuthRateLimitWindow(),
		Burst:    cfg.AuthRateLimitBurst,
	}
	var authRateLimiter httpmiddleware.RateLimiter
	closeAuthRateLimiter := func() {}
	authRateLimiter, closeAuthRateLimiter, err = newAuthRateLimiter(ctx, cfg.RedisURL, rateLimitConfig, logger)
	if err != nil {
		return err
	}
	defer closeAuthRateLimiter()

	// Wire application routes
	RegisterRoutes(api, Dependencies{
		AuthService:             authSvc,
		UsersService:            usersSvc,
		RolesService:            rolesSvc,
		ProjectsService:         projectsSvc,
		SystemEventsService:     systemEventsSvc,
		NotificationsService:    notificationsSvc,
		RefreshCookie:           refreshCookieConfig(cfg),
		AccessCookie:            accessCookieConfig(cfg),
		AuthRateLimitMiddleware: httpmiddleware.AuthRateLimit(authRateLimiter, metrics.RecordAuthRateLimitRejection),
		ReadyChecker:            handlers.NewDatabaseReadyChecker(pool, cfg.DBHealthTimeout()),
	})

	// errChan is buffered for both listeners so a bind failure from either is
	// reported without the goroutine blocking. A configured metrics listener is
	// an operational requirement, so its failure fails startup just like the API
	// listener's — it must not silently start without metrics.
	errChan := make(chan error, 2)

	// Optional internal-only metrics listener. Started before the API server so
	// the scrape target is up early; it is drained alongside the API on shutdown.
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metrics.Handler())
		metricsServer = &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           mux,
			ReadHeaderTimeout: cfg.ReadTimeout(),
		}
		go func() {
			logger.Info("Starting metrics server", "addr", cfg.MetricsAddr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errChan <- fmt.Errorf("metrics server (%s): %w", cfg.MetricsAddr, err)
			}
		}()
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      otelhttp.NewHandler(router, "http.server"),
		ReadTimeout:  cfg.ReadTimeout(),
		WriteTimeout: cfg.WriteTimeout(),
		IdleTimeout:  cfg.IdleTimeout(),
	}

	go func() {
		logger.Info("Starting API server",
			"app_name", cfg.AppName,
			"port", cfg.Port,
			"env", cfg.Env,
			"log_format", cfg.LogFormat,
			"log_level", cfg.LogLevel,
			"shutdown_timeout_seconds", int(cfg.ShutdownTimeout().Seconds()),
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return fmt.Errorf("server startup failed: %w", err)
	case <-ctx.Done():
		logger.Info("Graceful shutdown initiated", "reason", ctx.Err().Error())
	}

	// Timeout buffer for socket drain and active requests handling
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()

	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics server shutdown failed", "error", err)
		}
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = forceCloseAfterShutdownTimeout(logger, server, err)
		} else {
			logger.Error("Graceful shutdown failed", "error", err)
		}
		return fmt.Errorf("server shutdown encountered error: %w", err)
	}

	logger.Info("Graceful shutdown completed")
	return nil
}

type serverCloser interface {
	Close() error
}

func forceCloseAfterShutdownTimeout(logger *slog.Logger, server serverCloser, shutdownErr error) error {
	logger.Warn("shutdown timeout reached; forced close starting", "error", shutdownErr)
	if closeErr := server.Close(); closeErr != nil {
		logger.Error("forced close failed", "error", closeErr)
		return errors.Join(shutdownErr, closeErr)
	}
	logger.Info("forced close completed")
	return shutdownErr
}

func refreshCookieConfig(cfg *config.Config) handlers.RefreshCookieConfig {
	return handlers.RefreshCookieConfig{
		Name:     cfg.RefreshTokenCookieName,
		Path:     "/api/auth",
		Secure:   cfg.RefreshTokenCookieSecure,
		SameSite: refreshCookieSameSite(cfg.RefreshTokenCookieSameSite),
		TTL:      cfg.RefreshTokenTTL(),
	}
}

func accessCookieConfig(cfg *config.Config) handlers.AccessCookieConfig {
	return handlers.AccessCookieConfig{
		Name:     cfg.AccessTokenCookieName,
		Path:     "/",
		Domain:   cfg.AccessTokenCookieDomain,
		Secure:   cfg.AccessTokenCookieSecure,
		SameSite: refreshCookieSameSite(cfg.AccessTokenCookieSameSite),
		// MaxAge tracks the refresh-session lifetime, not the short JWT TTL. The
		// JWT inside still expires per JWTAccessTokenTTL (enforced server-side),
		// but the cookie must outlive it: a reload after the access token expires
		// then still presents the cookie, the API returns 401, the client
		// silently refreshes, and an edge guard keying off cookie presence does
		// not bounce a still-refreshable session.
		TTL: cfg.RefreshTokenTTL(),
	}
}

func newAuthRateLimiter(ctx context.Context, redisURL string, cfg httpmiddleware.RateLimitConfig, logger *slog.Logger) (httpmiddleware.RateLimiter, func(), error) {
	if redisURL == "" {
		return httpmiddleware.NewIPRateLimiter(cfg), func() {}, nil
	}

	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, func() {}, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	redisClient := redis.NewClient(redisOpts)

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	pingErr := redisClient.Ping(pingCtx).Err()
	cancelPing()
	if pingErr != nil {
		if logger != nil {
			logger.Warn("Redis ping failed; auth rate limiting falling back to local memory", "error", pingErr)
		}
		_ = redisClient.Close()
		return httpmiddleware.NewIPRateLimiter(cfg), func() {}, nil
	}

	cleanup := func() {
		if err := redisClient.Close(); err != nil && logger != nil {
			logger.Error("Redis client close failed", "error", err)
		}
	}
	if logger != nil {
		logger.Info("Auth rate limiting backed by Redis")
	}
	return httpmiddleware.NewRedisRateLimiter(redisClient, cfg), cleanup, nil
}

func refreshCookieSameSite(value string) http.SameSite {
	switch value {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
