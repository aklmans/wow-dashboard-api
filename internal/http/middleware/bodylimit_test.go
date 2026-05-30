package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestBodyLimit_UnderLimitPasses(t *testing.T) {
	called := false
	h := RequestBodyLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small body"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler was not called for an under-limit body")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequestBodyLimit_OverContentLengthRejected(t *testing.T) {
	h := RequestBodyLimit(8)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler must not run for an oversized body")
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is definitely longer than eight bytes"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"code":"bad_request"`) {
		t.Errorf("response body = %q, want the bad_request envelope", body)
	}
}

func TestRequestBodyLimit_DisabledWhenNonPositive(t *testing.T) {
	called := false
	h := RequestBodyLimit(0)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("anything goes"))
	req.ContentLength = 1 << 30 // even a huge declared length passes when disabled
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should run when the limit is disabled")
	}
}

func TestRequestBodyLimit_WrapsBodyForUndeclaredLength(t *testing.T) {
	var readErr error
	h := RequestBodyLimit(8)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	// ContentLength -1 (unknown/chunked) bypasses the fast path; the wrapped
	// reader must fail once the handler reads past the limit.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("way more than eight bytes streamed in"))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("expected a read error from the MaxBytesReader-wrapped body")
	}
}
