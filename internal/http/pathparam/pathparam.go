// Package pathparam provides tiny helpers for parsing values out of HTTP
// path parameters. Each helper trims whitespace, distinguishes "missing"
// from "malformed", and returns a sentinel error callers can map to a
// validation envelope at their layer.
package pathparam

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidUUID is returned when a path parameter is empty after trimming
// or cannot be parsed as a UUID. Callers should wrap or translate this
// into their own validation envelope.
var ErrInvalidUUID = errors.New("pathparam: invalid uuid")

// Detail returns the human-readable detail of an ErrInvalidUUID error with the
// package sentinel prefix stripped, so callers can embed it in their own
// envelope without hardcoding this package's message text. The prefix is
// derived from the sentinel itself. For any other error it returns err.Error().
func Detail(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(err.Error(), ErrInvalidUUID.Error()+": ")
}

// ParseUUID trims whitespace and parses value as a UUID. The field name is
// included in the returned error so callers can echo a precise message to
// the client (e.g. "id must be a valid UUID").
func ParseUUID(value string, field string) (uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if field == "" {
		field = "value"
	}
	if value == "" {
		return uuid.Nil, fmt.Errorf("%w: %s is required", ErrInvalidUUID, field)
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %s must be a valid UUID", ErrInvalidUUID, field)
	}
	return id, nil
}
