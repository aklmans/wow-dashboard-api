package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSmokeAuthSuccess(t *testing.T) {
	const (
		initialAccessToken   = "token-123"
		refreshedAccessToken = "token-456"
		initialRefreshToken  = "refresh-old"
		rotatedRefreshToken  = "refresh-new"
		testRefreshCookie    = "wow_dashboard_refresh_token"
	)

	var sawBearers []string
	var sawOldRefreshReplay bool
	var sawSignOut bool
	var sawRefreshAfterSignOut bool
	rotated := false
	signedOut := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/sign-in":
			var body signInRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode sign-in request: %v", err)
			}
			if body.Email != defaultSmokeEmail || body.Password != defaultSmokePassword {
				t.Fatalf("sign-in request = %#v, want demo credentials", body)
			}
			http.SetCookie(w, &http.Cookie{
				Name:     testRefreshCookie,
				Value:    initialRefreshToken,
				Path:     "/api/auth",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.SetCookie(w, &http.Cookie{
				Name:     accessCookieName,
				Value:    initialAccessToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/refresh":
			cookie, err := r.Cookie(testRefreshCookie)
			if err != nil {
				if signedOut {
					sawRefreshAfterSignOut = true
				}
				http.Error(w, "missing refresh cookie", http.StatusUnauthorized)
				return
			}
			switch cookie.Value {
			case initialRefreshToken:
				if rotated {
					sawOldRefreshReplay = true
					http.Error(w, "old refresh token rejected", http.StatusUnauthorized)
					return
				}
				rotated = true
				http.SetCookie(w, &http.Cookie{
					Name:     testRefreshCookie,
					Value:    rotatedRefreshToken,
					Path:     "/api/auth",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				http.SetCookie(w, &http.Cookie{
					Name:     accessCookieName,
					Value:    refreshedAccessToken,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"}}`))
			default:
				http.Error(w, "refresh token rejected", http.StatusUnauthorized)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/sign-out":
			cookie, err := r.Cookie(testRefreshCookie)
			if err != nil {
				t.Fatalf("sign-out missing refresh cookie: %v", err)
			}
			if cookie.Value != rotatedRefreshToken {
				t.Fatalf("sign-out refresh cookie value = %q, want rotated token", cookie.Value)
			}
			sawSignOut = true
			signedOut = true
			http.SetCookie(w, &http.Cookie{
				Name:     testRefreshCookie,
				Value:    "",
				Path:     "/api/auth",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.SetCookie(w, &http.Cookie{
				Name:     accessCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/me":
			sawBearers = append(sawBearers, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test","roles":["admin"],"permissions":["*"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	err := run(context.Background(), smokeConfig{
		BaseURL:  server.URL,
		Email:    defaultSmokeEmail,
		Password: defaultSmokePassword,
		Client:   server.Client(),
		Stdout:   &out,
	})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if len(sawBearers) != 2 {
		t.Fatalf("saw %d /api/auth/me calls, want 2", len(sawBearers))
	}
	if sawBearers[0] != "Bearer "+initialAccessToken {
		t.Fatalf("first Authorization = %q, want initial access token", sawBearers[0])
	}
	if sawBearers[1] != "Bearer "+refreshedAccessToken {
		t.Fatalf("second Authorization = %q, want refreshed access token", sawBearers[1])
	}
	if !sawOldRefreshReplay {
		t.Fatal("old refresh token replay was not attempted and rejected")
	}
	if !sawSignOut {
		t.Fatal("sign-out was not called")
	}
	if !sawRefreshAfterSignOut {
		t.Fatal("refresh after sign-out was not attempted and rejected")
	}
	output := out.String()
	if output == "" {
		t.Fatal("expected smoke output")
	}
	for _, secret := range []string{initialAccessToken, refreshedAccessToken, initialRefreshToken, rotatedRefreshToken} {
		if strings.Contains(output, secret) {
			t.Fatalf("smoke output leaked token value %q: %s", secret, output)
		}
	}
}

func TestRunSmokeAuthFailsWhenSignInOmitsAccessCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/auth/sign-in":
			// Sets the refresh cookie but not the access cookie, so the run must
			// fail specifically on the missing access cookie.
			http.SetCookie(w, &http.Cookie{
				Name:     refreshCookieName,
				Value:    "refresh-token",
				Path:     "/api/auth",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := run(context.Background(), smokeConfig{
		BaseURL:  server.URL,
		Email:    defaultSmokeEmail,
		Password: defaultSmokePassword,
		Client:   server.Client(),
		Stdout:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("run returned nil error, want missing access cookie failure")
	}
}

func TestRunSmokeAuthFailsWhenSignInOmitsRefreshCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/auth/sign-in":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"},"accessToken":"token-123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := run(context.Background(), smokeConfig{
		BaseURL:  server.URL,
		Email:    defaultSmokeEmail,
		Password: defaultSmokePassword,
		Client:   server.Client(),
		Stdout:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("run returned nil error, want missing refresh cookie failure")
	}
}

func TestRunSmokeAuthRedactsUnexpectedRefreshReplayResponse(t *testing.T) {
	const (
		initialAccessToken   = "token-123"
		refreshedAccessToken = "token-456"
		initialRefreshToken  = "refresh-old"
		rotatedRefreshToken  = "refresh-new"
		leakedReplayToken    = "leaked-replay-access-token"
		testRefreshCookie    = "wow_dashboard_refresh_token"
	)

	rotated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/readyz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/sign-in":
			http.SetCookie(w, &http.Cookie{
				Name:     testRefreshCookie,
				Value:    initialRefreshToken,
				Path:     "/api/auth",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.SetCookie(w, &http.Cookie{
				Name:     accessCookieName,
				Value:    initialAccessToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/refresh":
			cookie, err := r.Cookie(testRefreshCookie)
			if err != nil {
				http.Error(w, "missing refresh cookie", http.StatusUnauthorized)
				return
			}
			if cookie.Value == initialRefreshToken && rotated {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"},"accessToken":"` + leakedReplayToken + `"}`))
				return
			}
			if cookie.Value != initialRefreshToken {
				http.Error(w, "unexpected refresh cookie", http.StatusUnauthorized)
				return
			}
			rotated = true
			http.SetCookie(w, &http.Cookie{
				Name:     testRefreshCookie,
				Value:    rotatedRefreshToken,
				Path:     "/api/auth",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			http.SetCookie(w, &http.Cookie{
				Name:     accessCookieName,
				Value:    refreshedAccessToken,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/auth/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"user":{"email":"demo@wow-dashboard.test","roles":["admin"],"permissions":["*"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := run(context.Background(), smokeConfig{
		BaseURL:  server.URL,
		Email:    defaultSmokeEmail,
		Password: defaultSmokePassword,
		Client:   server.Client(),
		Stdout:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("run returned nil error, want replay status failure")
	}
	if strings.Contains(err.Error(), leakedReplayToken) {
		t.Fatalf("run error leaked access token %q: %v", leakedReplayToken, err)
	}
}

func TestSafeResponseBodySnippetRedactsSensitiveJSONFields(t *testing.T) {
	body := `{
		"accessToken":"access-secret",
		"refreshToken":"refresh-secret",
		"token":"generic-secret",
		"password":"password-secret",
		"cookie":"cookie-secret",
		"setCookie":"set-cookie-secret",
		"user":{"email":"demo@wow-dashboard.test","token":"nested-secret"}
	}`

	snippet := safeResponseBodySnippet(strings.NewReader(body))
	for _, secret := range []string{
		"access-secret",
		"refresh-secret",
		"generic-secret",
		"password-secret",
		"cookie-secret",
		"set-cookie-secret",
		"nested-secret",
	} {
		if strings.Contains(snippet, secret) {
			t.Fatalf("safe response snippet leaked %q: %s", secret, snippet)
		}
	}
	if !strings.Contains(snippet, "demo@wow-dashboard.test") {
		t.Fatalf("safe response snippet removed non-sensitive response detail: %s", snippet)
	}
}

func TestSmokeBaseURLFromEnv(t *testing.T) {
	t.Run("prefers BASE_URL", func(t *testing.T) {
		t.Setenv("BASE_URL", "http://base-url.example")
		t.Setenv("SMOKE_AUTH_BASE_URL", "http://legacy.example")

		if got := smokeBaseURLFromEnv(); got != "http://base-url.example" {
			t.Fatalf("smokeBaseURLFromEnv() = %q, want BASE_URL value", got)
		}
	})

	t.Run("falls back to legacy smoke env", func(t *testing.T) {
		t.Setenv("BASE_URL", "")
		t.Setenv("SMOKE_AUTH_BASE_URL", "http://legacy.example")

		if got := smokeBaseURLFromEnv(); got != "http://legacy.example" {
			t.Fatalf("smokeBaseURLFromEnv() = %q, want SMOKE_AUTH_BASE_URL value", got)
		}
	})
}
