package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/aklmans/wow-dashboard-api/internal/auth/authctx"
	httpmiddleware "github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

type clientInfoProbeResponse struct {
	Body struct {
		UserAgent string `json:"userAgent"`
		IPAddress string `json:"ipAddress"`
	}
}

func TestClientInfoCapturesUserAgentAndIP(t *testing.T) {
	router := chi.NewRouter()
	// RealIP lets the test inject the client IP via X-Real-IP, mirroring how a
	// reverse proxy would set RemoteAddr.
	router.Use(chimiddleware.RealIP)
	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))
	api.UseMiddleware(httpmiddleware.ClientInfo())

	huma.Register(api, huma.Operation{
		OperationID: "get-clientinfo-probe",
		Method:      http.MethodGet,
		Path:        "/probe",
	}, func(ctx context.Context, _ *struct{}) (*clientInfoProbeResponse, error) {
		info := authctx.ClientInfoFrom(ctx)
		resp := &clientInfoProbeResponse{}
		resp.Body.UserAgent = info.UserAgent
		resp.Body.IPAddress = info.IPAddress
		return resp, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (TestBrowser)")
	req.Header.Set("X-Real-IP", "203.0.113.42")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if want := `"userAgent":"Mozilla/5.0 (TestBrowser)"`; !strings.Contains(body, want) {
		t.Errorf("body %s missing %s", body, want)
	}
	if want := `"ipAddress":"203.0.113.42"`; !strings.Contains(body, want) {
		t.Errorf("body %s missing %s", body, want)
	}
}
