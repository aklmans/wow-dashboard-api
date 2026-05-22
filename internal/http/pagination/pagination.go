// Package pagination provides small reusable helpers for list-style API
// endpoints: page/pageSize normalization with sane defaults and an upper
// bound, offset computation, and search-term trimming.
//
// It is deliberately tiny: a struct of raw query inputs in, a normalized
// struct out. Callers (services or handlers) decide how to surface the
// resulting validation error to clients.
package pagination

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// DefaultPage is used when the caller passes 0 (the Go zero value).
	DefaultPage = 1
	// DefaultPageSize is used when the caller passes 0.
	DefaultPageSize = 20
	// MaxPageSize caps any client-supplied pageSize.
	MaxPageSize = 100
)

// ErrInvalidPagination is returned when page or pageSize falls outside the
// allowed range after defaulting. Callers should wrap or translate this
// into their own domain/error envelope.
var ErrInvalidPagination = errors.New("pagination: invalid input")

// Params is the raw, possibly-unset input from a query string.
type Params struct {
	Page     int
	PageSize int
	Search   string
}

// Normalized is the result of Normalize: page/pageSize are guaranteed to be
// within [1, MaxPageSize] for pageSize and >= 1 for page, Search is trimmed,
// and Offset is precomputed for store layers.
type Normalized struct {
	Page     int
	PageSize int
	Offset   int
	Search   string
}

// Normalize applies defaults, validates bounds, trims Search, and computes
// Offset. A zero Page or PageSize is treated as "unset" and replaced with
// the default. Negative values, or a PageSize above MaxPageSize, return
// ErrInvalidPagination.
func Normalize(p Params) (Normalized, error) {
	page := p.Page
	if page == 0 {
		page = DefaultPage
	}
	if page < 1 {
		return Normalized{}, fmt.Errorf("%w: page must be greater than or equal to 1", ErrInvalidPagination)
	}

	pageSize := p.PageSize
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return Normalized{}, fmt.Errorf("%w: pageSize must be between 1 and %d", ErrInvalidPagination, MaxPageSize)
	}

	return Normalized{
		Page:     page,
		PageSize: pageSize,
		Offset:   (page - 1) * pageSize,
		Search:   strings.TrimSpace(p.Search),
	}, nil
}
