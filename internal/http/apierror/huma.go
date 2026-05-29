package apierror

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// WriteResponse writes the apierror as a JSON response with the project's
// stable error envelope. It sets the Content-Type, writes the status code,
// and encodes the Body(). Use this in chi/net-http handlers or middleware
// that operate outside Huma's handler pipeline.
func WriteResponse(w http.ResponseWriter, e *Error) {
	if e == nil {
		e = InternalError(nil)
	}
	status := e.Status
	if status == 0 {
		// A zero status would make net/http panic on WriteHeader; fall back
		// to 500 rather than emitting a malformed response.
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e.Body())
}

// GetStatus satisfies huma.StatusError.
func (e *Error) GetStatus() int {
	return e.Status
}

// MarshalJSON satisfies json.Marshaler to ensure our Error struct
// is always serialized as the stable client response envelope.
func (e *Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Body())
}

// ContentType keeps apierror responses on the project's JSON media type when
// Huma serializes them.
func (e *Error) ContentType(string) string {
	return "application/json"
}

// HumaError converts an apierror.Error into a Huma-compatible StatusError.
// Huma handlers can return this directly:
//
//	return nil, apierror.NotFound("user not found").WithRequestID(reqID).HumaError()
func (e *Error) HumaError() huma.StatusError {
	return e
}

// ForContext is a convenience that sets the request ID from the context
// and returns a Huma-compatible error. Typical usage in a Huma handler:
//
//	return nil, apierror.NotFound("item not found").ForContext(ctx)
func (e *Error) ForContext(ctx context.Context) huma.StatusError {
	return e.WithRequestID(RequestIDFromContext(ctx))
}

// HumaErrorTransformer converts Huma-generated parse and validation errors
// into the project's stable error envelope before serialization.
func HumaErrorTransformer(ctx huma.Context, status string, value any) (any, error) {
	model, ok := value.(*huma.ErrorModel)
	if !ok {
		return value, nil
	}

	ctx.SetHeader("Content-Type", "application/json")
	statusCode := model.Status
	if statusCode == 0 {
		parsedStatus, err := strconv.Atoi(status)
		if err == nil {
			statusCode = parsedStatus
		}
	}
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	return errorFromHumaModel(ctx.Context(), statusCode, model), nil
}

func errorFromHumaModel(ctx context.Context, statusCode int, model *huma.ErrorModel) *Error {
	requestID := RequestIDFromContext(ctx)

	// Server errors never echo Huma's message or field-level details to the
	// client; collapse every 5xx to a generic internal error envelope.
	if statusCode >= http.StatusInternalServerError {
		err := InternalError(nil)
		err.Status = statusCode
		return err.WithRequestID(requestID)
	}

	details := safeHumaDetails(model.Errors)
	message := safeHumaMessage(statusCode)

	var err *Error
	switch statusCode {
	case http.StatusUnauthorized:
		err = Unauthorized(message)
	case http.StatusForbidden:
		err = Forbidden(message)
	case http.StatusNotFound:
		err = NotFound(message)
	case http.StatusConflict:
		err = Conflict(message)
	case http.StatusUnprocessableEntity:
		err = ValidationFailed(message, details...)
	default:
		err = BadRequest(message)
		err.Status = statusCode
	}

	// ValidationFailed already carries details; attach them for any other 4xx
	// status that produced field-level errors.
	if statusCode != http.StatusUnprocessableEntity && len(details) > 0 {
		err = err.WithDetails(details...)
	}
	return err.WithRequestID(requestID)
}

func safeHumaMessage(statusCode int) string {
	if statusCode >= http.StatusInternalServerError {
		return "An internal error occurred. Please try again later."
	}

	switch statusCode {
	case http.StatusBadRequest,
		http.StatusRequestTimeout,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return "Invalid request."
	}

	if text := http.StatusText(statusCode); text != "" {
		return text
	}
	return "Invalid request."
}

func safeHumaDetails(humaDetails []*huma.ErrorDetail) []ErrorDetail {
	if len(humaDetails) == 0 {
		return nil
	}

	details := make([]ErrorDetail, 0, len(humaDetails))
	for _, detail := range humaDetails {
		if detail == nil || detail.Message == "" {
			continue
		}
		details = append(details, ErrorDetail{
			Field:   fieldFromHumaLocation(detail.Location),
			Message: detail.Message,
		})
	}
	return details
}

func fieldFromHumaLocation(location string) string {
	location = strings.TrimSpace(location)
	location = strings.TrimPrefix(location, "body.")
	return location
}

// LoggingTransformer returns a Huma transformer that records server-side
// diagnostics for error responses. It never mutates the value it receives — it
// only emits a structured log line so the internal cause of a 5xx (which is
// deliberately stripped from the client envelope) is preserved for operators.
//
// Register it BEFORE [HumaErrorTransformer] so it observes the original error
// value (a handler-returned *Error still carrying its cause, or Huma's own
// *huma.ErrorModel) rather than the already-collapsed client envelope.
//
// If logger is nil the process default (slog.Default()) is resolved lazily at
// call time, matching the rest of the HTTP stack (see middleware.RequestLogger).
func LoggingTransformer(logger *slog.Logger) huma.Transformer {
	return func(ctx huma.Context, status string, value any) (any, error) {
		switch v := value.(type) {
		case *Error:
			logAppError(ctx, logger, v)
		case *huma.ErrorModel:
			logHumaModel(ctx, logger, status, v)
		}
		return value, nil
	}
}

func resolveLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

// logAppError logs handler-returned apierror values. Pure 4xx client errors are
// expected and already captured by the request log, so only server faults
// (status >= 500) and any error carrying an internal cause are recorded here.
func logAppError(ctx huma.Context, logger *slog.Logger, e *Error) {
	if e == nil {
		return
	}
	if e.Status < http.StatusInternalServerError && e.cause == nil {
		return
	}

	requestID := e.RequestID
	if requestID == "" {
		requestID = RequestIDFromContext(ctx.Context())
	}

	attrs := []any{
		"code", e.Code,
		"status", e.Status,
		"request_id", requestID,
	}
	if e.cause != nil {
		attrs = append(attrs, "error", e.cause)
	} else {
		attrs = append(attrs, "error", e.Message)
	}

	log := resolveLogger(logger)
	if e.Status >= http.StatusInternalServerError {
		log.ErrorContext(ctx.Context(), "request_error", attrs...)
		return
	}
	log.WarnContext(ctx.Context(), "request_error", attrs...)
}

// logHumaModel logs Huma-generated errors (parse/validation failures, or a
// plain error wrapped by huma.NewError). Only 5xx are recorded; 4xx detail is
// safely returned to the client already. Huma collapses the real cause of a
// non-StatusError into the model's Detail/Errors, so capture it here before
// HumaErrorTransformer strips it from the client envelope.
func logHumaModel(ctx huma.Context, logger *slog.Logger, status string, model *huma.ErrorModel) {
	if model == nil {
		return
	}
	statusCode := model.Status
	if statusCode == 0 {
		if parsed, err := strconv.Atoi(status); err == nil {
			statusCode = parsed
		}
	}
	if statusCode < http.StatusInternalServerError {
		return
	}

	attrs := []any{
		"status", statusCode,
		"request_id", RequestIDFromContext(ctx.Context()),
	}
	if detail := humaModelDetail(model); detail != "" {
		attrs = append(attrs, "error", detail)
	}
	resolveLogger(logger).ErrorContext(ctx.Context(), "request_error", attrs...)
}

// humaModelDetail builds a single diagnostic string from a Huma error model,
// combining its top-level Detail with any field-level messages.
func humaModelDetail(model *huma.ErrorModel) string {
	parts := make([]string, 0, len(model.Errors)+1)
	if model.Detail != "" {
		parts = append(parts, model.Detail)
	}
	for _, d := range model.Errors {
		if d == nil || d.Message == "" {
			continue
		}
		if d.Location != "" {
			parts = append(parts, d.Location+": "+d.Message)
			continue
		}
		parts = append(parts, d.Message)
	}
	return strings.Join(parts, "; ")
}
