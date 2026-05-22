package apierror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// --- Code to HTTP status mapping ---

func TestStatusForCode_BuiltinCodes(t *testing.T) {
	tests := []struct {
		code Code
		want int
	}{
		{CodeBadRequest, http.StatusBadRequest},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeConflict, http.StatusConflict},
		{CodeValidationFailed, http.StatusUnprocessableEntity},
		{CodeRateLimited, http.StatusTooManyRequests},
		{CodeServiceUnavailable, http.StatusServiceUnavailable},
		{CodeInternalError, http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			got := StatusForCode(tc.code)
			if got != tc.want {
				t.Errorf("StatusForCode(%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}

func TestStatusForCode_UnknownCode(t *testing.T) {
	got := StatusForCode(Code("unknown_code"))
	if got != http.StatusInternalServerError {
		t.Errorf("StatusForCode unknown = %d, want %d", got, http.StatusInternalServerError)
	}
}

// --- Constructor tests ---

func TestNew_SetsCodeMessageStatus(t *testing.T) {
	e := New(CodeNotFound, "resource not found")
	if e.Code != CodeNotFound {
		t.Errorf("Code = %q, want %q", e.Code, CodeNotFound)
	}
	if e.Message != "resource not found" {
		t.Errorf("Message = %q, want %q", e.Message, "resource not found")
	}
	if e.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", e.Status, http.StatusNotFound)
	}
}

func TestBadRequest(t *testing.T) {
	e := BadRequest("invalid input")
	if e.Code != CodeBadRequest || e.Status != http.StatusBadRequest {
		t.Errorf("BadRequest: code=%q status=%d", e.Code, e.Status)
	}
}

func TestUnauthorized(t *testing.T) {
	e := Unauthorized("not authenticated")
	if e.Code != CodeUnauthorized || e.Status != http.StatusUnauthorized {
		t.Errorf("Unauthorized: code=%q status=%d", e.Code, e.Status)
	}
}

func TestForbidden(t *testing.T) {
	e := Forbidden("access denied")
	if e.Code != CodeForbidden || e.Status != http.StatusForbidden {
		t.Errorf("Forbidden: code=%q status=%d", e.Code, e.Status)
	}
}

func TestNotFound(t *testing.T) {
	e := NotFound("not found")
	if e.Code != CodeNotFound || e.Status != http.StatusNotFound {
		t.Errorf("NotFound: code=%q status=%d", e.Code, e.Status)
	}
}

func TestConflict(t *testing.T) {
	e := Conflict("already exists")
	if e.Code != CodeConflict || e.Status != http.StatusConflict {
		t.Errorf("Conflict: code=%q status=%d", e.Code, e.Status)
	}
}

func TestValidationFailed(t *testing.T) {
	e := ValidationFailed("invalid fields",
		ErrorDetail{Field: "email", Message: "must be valid"},
		ErrorDetail{Field: "name", Message: "required"},
	)
	if e.Code != CodeValidationFailed || e.Status != http.StatusUnprocessableEntity {
		t.Errorf("ValidationFailed: code=%q status=%d", e.Code, e.Status)
	}
	if len(e.Details) != 2 {
		t.Fatalf("Details length = %d, want 2", len(e.Details))
	}
	if e.Details[0].Field != "email" {
		t.Errorf("Details[0].Field = %q, want %q", e.Details[0].Field, "email")
	}
}

func TestServiceUnavailable(t *testing.T) {
	e := ServiceUnavailable("Service is not ready.")
	if e.Code != CodeServiceUnavailable || e.Status != http.StatusServiceUnavailable {
		t.Errorf("ServiceUnavailable: code=%q status=%d", e.Code, e.Status)
	}
	if e.Message != "Service is not ready." {
		t.Errorf("Message = %q, want Service is not ready.", e.Message)
	}
}

func TestRateLimited(t *testing.T) {
	e := RateLimited("Too many authentication attempts. Please try again later.")
	if e.Code != CodeRateLimited || e.Status != http.StatusTooManyRequests {
		t.Errorf("RateLimited: code=%q status=%d", e.Code, e.Status)
	}
	if e.Message != "Too many authentication attempts. Please try again later." {
		t.Errorf("Message = %q, want rate limit message", e.Message)
	}
}

// --- InternalError: safe message, no cause leak ---

func TestInternalError_SafeMessage(t *testing.T) {
	cause := fmt.Errorf("pq: relation \"users\" does not exist")
	e := InternalError(cause)

	if e.Code != CodeInternalError {
		t.Errorf("Code = %q, want %q", e.Code, CodeInternalError)
	}
	if e.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", e.Status, http.StatusInternalServerError)
	}

	// The client-facing message must NOT contain the internal cause.
	body := e.Body()
	if body.Message == cause.Error() {
		t.Error("Body().Message leaks the internal error cause")
	}
	if body.Message != "An internal error occurred. Please try again later." {
		t.Errorf("Body().Message = %q, want safe generic message", body.Message)
	}

	// But the cause is available for logging.
	if e.Cause() == nil {
		t.Error("Cause() should not be nil")
	}
	if !errors.Is(e, cause) {
		t.Error("errors.Is should match the wrapped cause")
	}
}

// --- Error() string ---

func TestError_String(t *testing.T) {
	e := NotFound("item not found")
	want := "not_found: item not found"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestError_StringWithCause(t *testing.T) {
	cause := fmt.Errorf("db timeout")
	e := InternalError(cause)
	got := e.Error()
	// Must include both the code and the cause for logging.
	if got != "internal_error: An internal error occurred. Please try again later.: db timeout" {
		t.Errorf("Error() = %q, want to include code and cause", got)
	}
}

// --- WithRequestID ---

func TestWithRequestID(t *testing.T) {
	original := NotFound("gone")
	withID := original.WithRequestID("req-abc-123")

	if withID.RequestID != "req-abc-123" {
		t.Errorf("RequestID = %q, want %q", withID.RequestID, "req-abc-123")
	}
	// Original should be unmodified (shallow copy).
	if original.RequestID != "" {
		t.Error("original.RequestID was mutated")
	}
}

func TestBody_IncludesRequestID(t *testing.T) {
	e := BadRequest("bad").WithRequestID("req-xyz")
	body := e.Body()
	if body.RequestID != "req-xyz" {
		t.Errorf("Body().RequestID = %q, want %q", body.RequestID, "req-xyz")
	}
}

// --- WithDetails ---

func TestWithDetails(t *testing.T) {
	e := BadRequest("validation error").WithDetails(
		ErrorDetail{Field: "age", Message: "must be positive"},
	)
	if len(e.Details) != 1 {
		t.Fatalf("Details length = %d, want 1", len(e.Details))
	}
}

// --- Request ID from context ---

func TestRequestIDFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	id := RequestIDFromContext(ctx)
	if id != "" {
		t.Errorf("RequestIDFromContext(empty) = %q, want empty string", id)
	}
}

func TestRequestIDFromContext_WithValue(t *testing.T) {
	// Simulate chi's middleware setting the request ID in context.
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "test-req-id")
	id := RequestIDFromContext(ctx)
	if id != "test-req-id" {
		t.Errorf("RequestIDFromContext = %q, want %q", id, "test-req-id")
	}
}

func TestRequestIDFromRequest(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	ctx := context.WithValue(req.Context(), middleware.RequestIDKey, "from-request")
	req = req.WithContext(ctx)

	id := RequestIDFromRequest(req)
	if id != "from-request" {
		t.Errorf("RequestIDFromRequest = %q, want %q", id, "from-request")
	}
}

// --- Body does not leak cause ---

func TestBody_DoesNotLeakCause(t *testing.T) {
	cause := fmt.Errorf("SELECT * FROM secret_table WHERE token='abc'")
	e := InternalError(cause)
	body := e.Body()

	// The body should have no reference to the SQL error.
	if body.Message == cause.Error() {
		t.Error("Body leaks internal cause in Message")
	}
	// Code should be generic.
	if body.Code != CodeInternalError {
		t.Errorf("Body.Code = %q, want %q", body.Code, CodeInternalError)
	}
}
