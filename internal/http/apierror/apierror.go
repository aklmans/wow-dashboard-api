// Package apierror provides a unified API error contract for the wow-dashboard-api service.
//
// Every error returned to clients goes through this package to ensure:
//   - Stable JSON fields: code, message, request_id, and optional details.
//   - Machine-readable error codes (e.g. "not_found", "validation_failed").
//   - No accidental leakage of internal errors, stack traces, or SQL messages.
//
// Errors are converted to Huma-compatible StatusError values via the
// [*Error.HumaError] method so they integrate with Huma's middleware and
// OpenAPI error documentation.
package apierror

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// Code is a machine-readable error identifier sent in every error response.
type Code string

const (
	CodeBadRequest         Code = "bad_request"
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeValidationFailed   Code = "validation_failed"
	CodeRateLimited        Code = "rate_limited"
	CodeServiceUnavailable Code = "service_unavailable"
	CodeInternalError      Code = "internal_error"
)

// codeStatus maps each Code to its default HTTP status.
var codeStatus = map[Code]int{
	CodeBadRequest:         http.StatusBadRequest,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeNotFound:           http.StatusNotFound,
	CodeConflict:           http.StatusConflict,
	CodeValidationFailed:   http.StatusUnprocessableEntity,
	CodeRateLimited:        http.StatusTooManyRequests,
	CodeServiceUnavailable: http.StatusServiceUnavailable,
	CodeInternalError:      http.StatusInternalServerError,
}

// StatusForCode returns the HTTP status code for the given error Code.
// Unknown codes default to 500.
func StatusForCode(c Code) int {
	if s, ok := codeStatus[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// ErrorDetail provides optional structured information about a specific
// validation or field-level error.
type ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// Error is the project's canonical API error type.
// It satisfies the error interface and carries all information needed
// to produce a stable JSON error response.
type Error struct {
	Code      Code          `json:"code"`
	Message   string        `json:"message"`
	Status    int           `json:"status"`
	Details   []ErrorDetail `json:"details,omitempty"`
	RequestID string        `json:"request_id,omitempty"`

	// cause is the underlying error, never exposed to clients.
	cause error
}

// Error satisfies the error interface.
func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap supports errors.Is / errors.As.
func (e *Error) Unwrap() error {
	return e.cause
}

// WithRequestID returns a shallow copy of the error with the request ID set.
func (e *Error) WithRequestID(id string) *Error {
	cp := *e
	cp.RequestID = id
	return &cp
}

// WithDetails returns a shallow copy of the error with the given details appended.
func (e *Error) WithDetails(details ...ErrorDetail) *Error {
	cp := *e
	cp.Details = append(cp.Details, details...)
	return &cp
}

// WithCause returns a shallow copy of the error with the given internal cause.
// The cause is used for logging but never exposed to clients.
func (e *Error) WithCause(err error) *Error {
	cp := *e
	cp.cause = err
	return &cp
}

// Cause returns the wrapped internal error, or nil.
func (e *Error) Cause() error {
	return e.cause
}

// ResponseBody returns the stable JSON-serializable error envelope.
// This is the shape sent to API clients.
type ResponseBody struct {
	Code      Code          `json:"code"`
	Message   string        `json:"message"`
	RequestID string        `json:"request_id"`
	Details   []ErrorDetail `json:"details,omitempty"`
}

// Body returns a ResponseBody suitable for JSON marshaling to the client.
func (e *Error) Body() ResponseBody {
	return ResponseBody{
		Code:      e.Code,
		Message:   e.Message,
		RequestID: e.RequestID,
		Details:   e.Details,
	}
}

// --- Constructors for common error codes ---

// New creates an Error with the given code and safe client-facing message.
func New(code Code, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Status:  StatusForCode(code),
	}
}

// BadRequest creates a 400 error.
func BadRequest(message string) *Error {
	return New(CodeBadRequest, message)
}

// Unauthorized creates a 401 error.
func Unauthorized(message string) *Error {
	return New(CodeUnauthorized, message)
}

// Forbidden creates a 403 error.
func Forbidden(message string) *Error {
	return New(CodeForbidden, message)
}

// NotFound creates a 404 error.
func NotFound(message string) *Error {
	return New(CodeNotFound, message)
}

// Conflict creates a 409 error.
func Conflict(message string) *Error {
	return New(CodeConflict, message)
}

// ValidationFailed creates a 422 error, typically with field-level details.
func ValidationFailed(message string, details ...ErrorDetail) *Error {
	return New(CodeValidationFailed, message).WithDetails(details...)
}

// RateLimited creates a 429 error.
func RateLimited(message string) *Error {
	return New(CodeRateLimited, message)
}

// ServiceUnavailable creates a 503 error.
func ServiceUnavailable(message string) *Error {
	return New(CodeServiceUnavailable, message)
}

// InternalError creates a 500 error with a generic safe message.
// The cause is stored for logging but never shown to clients.
func InternalError(cause error) *Error {
	return New(CodeInternalError, "An internal error occurred. Please try again later.").WithCause(cause)
}

// --- Request ID helpers ---

// RequestIDFromContext extracts the request ID set by chi's RequestID middleware.
// Returns an empty string if no request ID is present.
func RequestIDFromContext(ctx context.Context) string {
	return middleware.GetReqID(ctx)
}

// RequestIDFromRequest is a convenience wrapper around RequestIDFromContext.
func RequestIDFromRequest(r *http.Request) string {
	return RequestIDFromContext(r.Context())
}
