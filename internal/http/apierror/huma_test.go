package apierror

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func TestHumaError_GetStatus(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want int
	}{
		{"bad_request", BadRequest("bad"), http.StatusBadRequest},
		{"not_found", NotFound("missing"), http.StatusNotFound},
		{"rate_limited", RateLimited("slow down"), http.StatusTooManyRequests},
		{"service_unavailable", ServiceUnavailable("not ready"), http.StatusServiceUnavailable},
		{"internal", InternalError(nil), http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			he := tc.err.HumaError()
			if he.GetStatus() != tc.want {
				t.Errorf("GetStatus() = %d, want %d", he.GetStatus(), tc.want)
			}
		})
	}
}

func TestHumaError_Error(t *testing.T) {
	e := NotFound("gone")
	he := e.HumaError()
	if he.Error() != e.Error() {
		t.Errorf("Error() = %q, want %q", he.Error(), e.Error())
	}
}

func TestForContext_SetsRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "ctx-req-123")
	e := NotFound("item not found")
	he := e.ForContext(ctx)

	if he.GetStatus() != http.StatusNotFound {
		t.Errorf("GetStatus() = %d, want %d", he.GetStatus(), http.StatusNotFound)
	}
	// Verify that the original error was not mutated.
	if e.RequestID != "" {
		t.Error("original error was mutated by ForContext")
	}

	// Verify request ID is in the serialized body.
	data, err := json.Marshal(he)
	if err != nil {
		t.Fatalf("failed to marshal Huma error: %v", err)
	}

	var body ResponseBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if body.RequestID != "ctx-req-123" {
		t.Errorf("body.RequestID = %q, want %q", body.RequestID, "ctx-req-123")
	}
}

func TestWriteResponse(t *testing.T) {
	e := BadRequest("invalid field").WithRequestID("wr-001").WithDetails(
		ErrorDetail{Field: "email", Message: "required"},
	)

	rec := httptest.NewRecorder()
	WriteResponse(rec, e)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}

	var body ResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Code != CodeBadRequest {
		t.Errorf("body.Code = %q, want %q", body.Code, CodeBadRequest)
	}
	if body.Message != "invalid field" {
		t.Errorf("body.Message = %q, want %q", body.Message, "invalid field")
	}
	if body.RequestID != "wr-001" {
		t.Errorf("body.RequestID = %q, want %q", body.RequestID, "wr-001")
	}
	if len(body.Details) != 1 || body.Details[0].Field != "email" {
		t.Errorf("body.Details = %+v, want [{email required}]", body.Details)
	}
}

func TestWriteResponse_InternalError_NoLeak(t *testing.T) {
	cause := errorf("pq: password authentication failed for user postgres")
	e := InternalError(cause).WithRequestID("wr-002")

	rec := httptest.NewRecorder()
	WriteResponse(rec, e)

	var body ResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Message == cause.Error() {
		t.Error("WriteResponse leaked internal cause in message")
	}
	if body.Code != CodeInternalError {
		t.Errorf("body.Code = %q, want %q", body.Code, CodeInternalError)
	}
}

func TestMarshalJSON_InternalError_NoLeak(t *testing.T) {
	cause := errors.New("super secret database password leaked")
	err := InternalError(cause)

	data, errMarshal := json.Marshal(err)
	if errMarshal != nil {
		t.Fatalf("failed to marshal error: %v", errMarshal)
	}

	var body ResponseBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if body.Message != "An internal error occurred. Please try again later." {
		t.Errorf("body.Message = %q, want safe generic message", body.Message)
	}

	// Double check the raw JSON string does not contain the secret.
	jsonStr := string(data)
	if strings.Contains(jsonStr, "secret") {
		t.Errorf("marshaled JSON contains sensitive leak: %s", jsonStr)
	}
}

func TestHumaIntegration(t *testing.T) {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)

	api := humachi.New(router, huma.DefaultConfig("Test API", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "get-test-item",
		Method:      http.MethodGet,
		Path:        "/items/{id}",
	}, func(ctx context.Context, input *struct {
		ID string `path:"id"`
	}) (*struct{ Body string }, error) {
		return nil, NotFound("item not found").ForContext(ctx)
	})

	req := httptest.NewRequest(http.MethodGet, "/items/123", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	var body ResponseBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Code != CodeNotFound {
		t.Errorf("body.Code = %q, want %q", body.Code, CodeNotFound)
	}
	if body.Message != "item not found" {
		t.Errorf("body.Message = %q, want %q", body.Message, "item not found")
	}
	if body.RequestID == "" {
		t.Error("body.RequestID is empty, want a valid request ID")
	}
}

// errorf is a test helper that creates a plain error (avoids importing fmt for a test-only call).
func errorf(msg string) error {
	return &plainError{msg: msg}
}

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }
