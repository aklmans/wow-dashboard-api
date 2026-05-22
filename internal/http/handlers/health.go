package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

type HealthResponse struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"The status of the application"`
	}
}

type ReadyResponse struct {
	Body struct {
		Status string `json:"status" example:"ready" doc:"The readiness status of the application"`
	}
}

// ReadyChecker checks whether the service can handle business traffic.
type ReadyChecker interface {
	Ready(ctx context.Context) error
}

// ReadyCheckerFunc adapts a function to ReadyChecker.
type ReadyCheckerFunc func(ctx context.Context) error

// Ready runs the function as a readiness check.
func (f ReadyCheckerFunc) Ready(ctx context.Context) error {
	return f(ctx)
}

// NoopReadyChecker is ready when no runtime dependency is configured.
type NoopReadyChecker struct{}

// Ready always succeeds.
func (NoopReadyChecker) Ready(context.Context) error {
	return nil
}

// DatabasePinger is the database ping surface used by readiness checks.
type DatabasePinger interface {
	Ping(ctx context.Context) error
}

type databaseReadyChecker struct {
	pinger  DatabasePinger
	timeout time.Duration
}

// NewDatabaseReadyChecker returns a readiness checker backed by a database ping.
func NewDatabaseReadyChecker(pinger DatabasePinger, timeout time.Duration) ReadyChecker {
	return databaseReadyChecker{
		pinger:  pinger,
		timeout: timeout,
	}
}

func (c databaseReadyChecker) Ready(ctx context.Context) error {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	return c.pinger.Ping(ctx)
}

func RegisterHealth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Liveness probe",
		Description: "Checks if the service is alive.",
		Tags:        []string{"System"},
	}, func(ctx context.Context, input *struct{}) (*HealthResponse, error) {
		resp := &HealthResponse{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}

func RegisterReady(api huma.API, checker ReadyChecker) {
	if checker == nil {
		checker = NoopReadyChecker{}
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-readyz",
		Method:      http.MethodGet,
		Path:        "/readyz",
		Summary:     "Readiness probe",
		Description: "Checks if the service is ready to handle traffic.",
		Tags:        []string{"System"},
		Responses: apiErrorResponses(api,
			http.StatusServiceUnavailable,
		),
	}, func(ctx context.Context, input *struct{}) (*ReadyResponse, error) {
		if err := checker.Ready(ctx); err != nil {
			return nil, apierror.ServiceUnavailable("Service is not ready.").WithCause(err).ForContext(ctx)
		}
		resp := &ReadyResponse{}
		resp.Body.Status = "ready"
		return resp, nil
	})
}
