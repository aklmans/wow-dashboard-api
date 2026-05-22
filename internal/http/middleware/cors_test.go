package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/http/middleware"
)

func TestCORSMiddleware(t *testing.T) {
	allowedOrigins := []string{
		"http://localhost:3000",
		"http://localhost:5173",
		"https://*-workspace.vercel.app",
	}

	cors := middleware.CORS(allowedOrigins)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	testHandler := cors(nextHandler)

	tests := []struct {
		name              string
		method            string
		origin            string
		expectedStatus    int
		expectedOrigin    string
		shouldSetCORS     bool
		expectCredentials bool
	}{
		{
			name:              "allow exact local origin 3000",
			method:            "GET",
			origin:            "http://localhost:3000",
			expectedStatus:    http.StatusOK,
			expectedOrigin:    "http://localhost:3000",
			shouldSetCORS:     true,
			expectCredentials: true,
		},
		{
			name:              "allow exact local origin 5173",
			method:            "GET",
			origin:            "http://localhost:5173",
			expectedStatus:    http.StatusOK,
			expectedOrigin:    "http://localhost:5173",
			shouldSetCORS:     true,
			expectCredentials: true,
		},
		{
			name:           "refuse unknown origin",
			method:         "GET",
			origin:         "http://evil-attacker.com",
			expectedStatus: http.StatusOK,
			expectedOrigin: "",
			shouldSetCORS:  false,
		},
		{
			// A wildcard-matched origin is allowed but never receives
			// credentialed CORS.
			name:              "allow wildcard preview origin without credentials",
			method:            "GET",
			origin:            "https://my-pr-12-workspace.vercel.app",
			expectedStatus:    http.StatusOK,
			expectedOrigin:    "https://my-pr-12-workspace.vercel.app",
			shouldSetCORS:     true,
			expectCredentials: false,
		},
		{
			name:           "refuse invalid wildcard format",
			method:         "GET",
			origin:         "https://my-pr-12-workspace.vercel.app.evil.com",
			expectedStatus: http.StatusOK,
			expectedOrigin: "",
			shouldSetCORS:  false,
		},
		{
			name:              "OPTIONS preflight request allowed",
			method:            "OPTIONS",
			origin:            "http://localhost:3000",
			expectedStatus:    http.StatusNoContent,
			expectedOrigin:    "http://localhost:3000",
			shouldSetCORS:     true,
			expectCredentials: true,
		},
		{
			name:           "OPTIONS preflight request refused",
			method:         "OPTIONS",
			origin:         "http://evil-attacker.com",
			expectedStatus: http.StatusNoContent,
			expectedOrigin: "",
			shouldSetCORS:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()

			testHandler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			originHeader := rec.Header().Get("Access-Control-Allow-Origin")
			credentialsHeader := rec.Header().Get("Access-Control-Allow-Credentials")
			if tt.shouldSetCORS {
				if originHeader != tt.expectedOrigin {
					t.Errorf("expected Access-Control-Allow-Origin %q, got %q", tt.expectedOrigin, originHeader)
				}
				if tt.expectCredentials && credentialsHeader != "true" {
					t.Error("expected Access-Control-Allow-Credentials to be true")
				}
				if !tt.expectCredentials && credentialsHeader != "" {
					t.Errorf("expected no Access-Control-Allow-Credentials for a wildcard origin, got %q", credentialsHeader)
				}
			} else {
				if originHeader != "" {
					t.Errorf("expected empty Access-Control-Allow-Origin, got %q", originHeader)
				}
			}
		})
	}
}
