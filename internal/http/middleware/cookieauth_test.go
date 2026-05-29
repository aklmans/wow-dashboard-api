package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const testAccessCookie = "wow_dashboard_access_token"

func TestAccessCookieBridge(t *testing.T) {
	newHandler := func() (http.Handler, *string) {
		var seen string
		h := AccessCookieBridge(testAccessCookie)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		return h, &seen
	}

	t.Run("cookie is bridged into Authorization", func(t *testing.T) {
		h, seen := newHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: testAccessCookie, Value: "jwt-123"})
		h.ServeHTTP(httptest.NewRecorder(), req)
		if *seen != "Bearer jwt-123" {
			t.Fatalf("Authorization = %q, want %q", *seen, "Bearer jwt-123")
		}
	})

	t.Run("existing Authorization header wins", func(t *testing.T) {
		h, seen := newHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer real-bearer")
		req.AddCookie(&http.Cookie{Name: testAccessCookie, Value: "jwt-123"})
		h.ServeHTTP(httptest.NewRecorder(), req)
		if *seen != "Bearer real-bearer" {
			t.Fatalf("Authorization = %q, want the original bearer to win", *seen)
		}
	})

	t.Run("no cookie leaves Authorization empty", func(t *testing.T) {
		h, seen := newHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if *seen != "" {
			t.Fatalf("Authorization = %q, want empty", *seen)
		}
	})
}

func TestCSRFGuard(t *testing.T) {
	allowed := []string{"https://app.example.com"}

	newHandler := func() (http.Handler, *bool) {
		called := false
		h := CSRFGuard(allowed, testAccessCookie)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		return h, &called
	}

	withCookie := func(req *http.Request) *http.Request {
		req.AddCookie(&http.Cookie{Name: testAccessCookie, Value: "jwt-123"})
		return req
	}

	t.Run("safe method passes even cross-site", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/api/users", nil))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("GET should always pass the CSRF guard")
		}
	})

	t.Run("unsafe without access cookie passes (bearer client)", func(t *testing.T) {
		h, called := newHandler()
		req := httptest.NewRequest(http.MethodPost, "/api/projects", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("POST without the access cookie should pass (no ambient credential)")
		}
	})

	t.Run("unsafe cookie cross-site is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil))
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if *called {
			t.Fatal("cross-site cookie POST should be blocked")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("unsafe cookie same-site passes via Sec-Fetch-Site", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil))
		req.Header.Set("Sec-Fetch-Site", "same-site")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("same-site cookie POST should pass")
		}
	})

	t.Run("unsafe cookie allowed Origin passes without fetch metadata", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil))
		req.Header.Set("Origin", "https://app.example.com")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("allowed-Origin cookie POST should pass")
		}
	})

	t.Run("unsafe cookie disallowed Origin is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil))
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if *called {
			t.Fatal("disallowed-Origin cookie POST should be blocked")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("unsafe cookie with no origin signal is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if *called {
			t.Fatal("cookie POST with neither Origin nor Sec-Fetch-Site should be blocked")
		}
	})
}
