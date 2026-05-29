package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testAccessCookie  = "wow_dashboard_access_token"
	testRefreshCookie = "wow_dashboard_refresh_token"
)

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
		h := CSRFGuard(allowed, testAccessCookie, testRefreshCookie)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}))
		return h, &called
	}

	withCookie := func(req *http.Request, name string) *http.Request {
		req.AddCookie(&http.Cookie{Name: name, Value: "v"})
		return req
	}

	assertBlocked := func(t *testing.T, called bool, code int) {
		t.Helper()
		if called {
			t.Fatal("request should have been blocked")
		}
		if code != http.StatusForbidden {
			t.Fatalf("code = %d, want %d", code, http.StatusForbidden)
		}
	}

	t.Run("safe method passes even cross-site", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodGet, "/api/users", nil), testAccessCookie)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("GET should always pass the CSRF guard")
		}
	})

	t.Run("unsafe without any auth cookie passes", func(t *testing.T) {
		h, called := newHandler()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("POST without an auth cookie should pass (no ambient credential)")
		}
	})

	t.Run("same-origin passes", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("same-origin cookie POST should pass")
		}
	})

	t.Run("same-site without allowlisted Origin is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Sec-Fetch-Site", "same-site") // e.g. an untrusted sibling subdomain
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertBlocked(t, *called, rec.Code)
	})

	t.Run("same-site with allowlisted Origin passes", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Origin", "https://app.example.com")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("same-site with an allowlisted Origin should pass")
		}
	})

	t.Run("cross-site is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertBlocked(t, *called, rec.Code)
	})

	t.Run("allowlisted Origin without fetch metadata passes", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Origin", "https://app.example.com")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("allowlisted-Origin cookie POST should pass")
		}
	})

	t.Run("disallowed Origin is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertBlocked(t, *called, rec.Code)
	})

	t.Run("no origin signal is blocked", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/projects", nil), testAccessCookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if *called {
			t.Fatal("cookie POST with neither Origin nor Sec-Fetch-Site should be blocked")
		}
	})

	t.Run("refresh cookie alone is guarded cross-site", func(t *testing.T) {
		// Access cookie expired; only the refresh cookie remains (POST /refresh).
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil), testRefreshCookie)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assertBlocked(t, *called, rec.Code)
	})

	t.Run("refresh cookie same-origin passes", func(t *testing.T) {
		h, called := newHandler()
		req := withCookie(httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil), testRefreshCookie)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !*called {
			t.Fatal("same-origin refresh-cookie POST should pass")
		}
	})
}
