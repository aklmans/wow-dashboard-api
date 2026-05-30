package middleware

import (
	"net/http"

	"github.com/aklmans/wow-dashboard-api/internal/http/apierror"
)

// RequestBodyLimit returns middleware that bounds the request body at the
// transport edge, before routing or handler execution, for every route.
//
// A request that declares a Content-Length over maxBytes is rejected
// immediately with 413 using the project's stable error envelope. A request
// without a declared length (chunked transfer encoding) has its body wrapped
// with http.MaxBytesReader so a read past the limit fails rather than streaming
// unbounded into a handler.
//
// This is a coarse global backstop that also covers non-Huma routes. Huma
// additionally caps each operation's parsed body (1 MiB by default) and returns
// its own 413, so for the default configuration (maxBytes == 1 MiB) the two
// layers agree. Lowering maxBytes below the Huma per-operation cap tightens the
// ceiling for Content-Length requests; chunked requests in the gap remain bound
// by Huma's cap.
//
// A maxBytes <= 0 disables the edge check (Huma's per-operation cap still
// applies).
func RequestBodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if maxBytes <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				writeRequestEntityTooLarge(w, r)
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeRequestEntityTooLarge emits the same envelope Huma produces for an
// oversized body: a bad_request code carrying the 413 status and a generic,
// non-leaky message, so clients see one consistent shape regardless of which
// layer rejected the request.
func writeRequestEntityTooLarge(w http.ResponseWriter, r *http.Request) {
	err := apierror.BadRequest("Invalid request.")
	err.Status = http.StatusRequestEntityTooLarge
	apierror.WriteResponse(w, err.WithRequestID(apierror.RequestIDFromRequest(r)))
}
