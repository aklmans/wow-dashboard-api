package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	authservice "github.com/aklmans/wow-dashboard-api/internal/auth/service"
	"github.com/aklmans/wow-dashboard-api/internal/auth/token"
	"github.com/aklmans/wow-dashboard-api/internal/config"
	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
	"github.com/aklmans/wow-dashboard-api/internal/http/handlers"
	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
	"github.com/aklmans/wow-dashboard-api/internal/logging"
	projectservice "github.com/aklmans/wow-dashboard-api/internal/projects/service"
	"github.com/aklmans/wow-dashboard-api/internal/store"
	"github.com/aklmans/wow-dashboard-api/internal/store/authrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/projectsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/query"
	"github.com/aklmans/wow-dashboard-api/internal/store/systemeventsrepo"
	"github.com/aklmans/wow-dashboard-api/internal/store/usersrepo"
	systemeventsservice "github.com/aklmans/wow-dashboard-api/internal/systemevents/service"
	userservice "github.com/aklmans/wow-dashboard-api/internal/users/service"
)

// Dependencies contains route-level use-case dependencies.
type Dependencies struct {
	AuthService             handlers.AuthService
	UsersService            handlers.UsersService
	ProjectsService         handlers.ProjectsService
	SystemEventsService     handlers.SystemEventsService
	RefreshCookie           handlers.RefreshCookieConfig
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
		handlers.RegisterAuthWithCookies(api, deps.AuthService, deps.RefreshCookie, authMiddlewares...)
	}
	if deps.AuthService != nil && deps.UsersService != nil {
		handlers.RegisterUsers(api, deps.AuthService, deps.UsersService)
	}
	if deps.AuthService != nil && deps.ProjectsService != nil {
		handlers.RegisterProjects(api, deps.AuthService, deps.ProjectsService)
	}
	if deps.AuthService != nil && deps.SystemEventsService != nil {
		handlers.RegisterSystemEvents(api, deps.AuthService, deps.SystemEventsService)
	}
}

// NewAPI creates and configures a Huma API instance on top of a Chi router.
func NewAPI(router chi.Router) huma.API {
	humaCfg := huma.DefaultConfig("Spec D-D API", "1.0.0")
	humaCfg.OpenAPIPath = "/openapi"
	humaCfg.DocsPath = "/docs"
	humaCfg.Transformers = append(humaCfg.Transformers, apierror.HumaErrorTransformer)
	return humachi.New(router, humaCfg)
}

// Run sets up and executes the main HTTP server process with graceful shutdown orchestration.
func Run(ctx context.Context, cfg *config.Config) error {
	logger := logging.NewLogger(cfg, os.Stdout)
	slog.SetDefault(logger)

	router := chi.NewRouter()

	// Base Standard Middlewares
	router.Use(middleware.RequestID)
	router.Use(httpmiddleware.RequestLogger(logger))
	router.Use(middleware.Recoverer)

	// Apply our Custom CORS Middleware
	router.Use(httpmiddleware.CORS(cfg.CORS))

	// Configure and initialize Huma API dynamically using the shared constructor
	api := NewAPI(router)

	pool, err := store.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialize database pool: %w", err)
	}
	defer func() {
		pool.Close()
		logger.Info("Database pool closed")
	}()

	queries := query.New(pool)

	tokenManager, err := token.NewManager(cfg.JWTAccessSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTAccessTokenTTL())
	if err != nil {
		return fmt.Errorf("initialize JWT token manager: %w", err)
	}

	authStore := authrepo.NewUserStore(queries)
	refreshTokenStore := authrepo.NewRefreshTokenStore(queries)
	unitOfWork := authrepo.NewUnitOfWork(pool)
	auditRecorder := authrepo.NewSystemEventRecorder(queries)
	authSvc := authservice.NewService(authStore, tokenManager,
		authservice.WithRefreshTokenStore(refreshTokenStore, cfg.RefreshTokenTTL()),
		authservice.WithUnitOfWork(unitOfWork),
		authservice.WithAuditRecorder(auditRecorder))
	usersSvc := userservice.NewService(usersrepo.NewUserStore(queries))
	projectsSvc := projectservice.NewService(projectsrepo.NewProjectStore(queries),
		projectservice.WithAuditRecorder(projectsrepo.NewSystemEventRecorder(queries)))
	systemEventsSvc := systemeventsservice.NewService(systemeventsrepo.NewEventStore(queries))
	authRateLimiter := httpmiddleware.NewIPRateLimiter(httpmiddleware.RateLimitConfig{
		Enabled:  cfg.AuthRateLimitEnabled,
		Requests: cfg.AuthRateLimitRequests,
		Window:   cfg.AuthRateLimitWindow(),
		Burst:    cfg.AuthRateLimitBurst,
	})

	// Wire application routes
	RegisterRoutes(api, Dependencies{
		AuthService:             authSvc,
		UsersService:            usersSvc,
		ProjectsService:         projectsSvc,
		SystemEventsService:     systemEventsSvc,
		RefreshCookie:           refreshCookieConfig(cfg),
		AuthRateLimitMiddleware: httpmiddleware.AuthRateLimit(authRateLimiter),
		ReadyChecker:            handlers.NewDatabaseReadyChecker(pool, cfg.DBHealthTimeout()),
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout(),
		WriteTimeout: cfg.WriteTimeout(),
		IdleTimeout:  cfg.IdleTimeout(),
	}

	errChan := make(chan error, 1)

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
