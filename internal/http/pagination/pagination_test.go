package pagination_test

import (
	"errors"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/http/pagination"
)

func TestNormalizeAppliesDefaults(t *testing.T) {
	got, err := pagination.Normalize(pagination.Params{})
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if got.Page != pagination.DefaultPage {
		t.Errorf("Page = %d, want %d", got.Page, pagination.DefaultPage)
	}
	if got.PageSize != pagination.DefaultPageSize {
		t.Errorf("PageSize = %d, want %d", got.PageSize, pagination.DefaultPageSize)
	}
	if got.Offset != 0 {
		t.Errorf("Offset = %d, want 0", got.Offset)
	}
	if got.Search != "" {
		t.Errorf("Search = %q, want empty", got.Search)
	}
}

func TestNormalizeDefaultsPageSizeWhenPageProvided(t *testing.T) {
	got, err := pagination.Normalize(pagination.Params{Page: 3})
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if got.Page != 3 || got.PageSize != pagination.DefaultPageSize {
		t.Errorf("Page/PageSize = %d/%d, want 3/%d", got.Page, got.PageSize, pagination.DefaultPageSize)
	}
}

func TestNormalizeOffsetCalculation(t *testing.T) {
	cases := []struct {
		page, size, want int
	}{
		{1, 20, 0},
		{2, 20, 20},
		{3, 50, 100},
		{1, 100, 0},
	}
	for _, tc := range cases {
		got, err := pagination.Normalize(pagination.Params{Page: tc.page, PageSize: tc.size})
		if err != nil {
			t.Fatalf("Normalize(%d,%d) error: %v", tc.page, tc.size, err)
		}
		if got.Offset != tc.want {
			t.Errorf("Normalize(%d,%d).Offset = %d, want %d", tc.page, tc.size, got.Offset, tc.want)
		}
	}
}

func TestNormalizeAcceptsMaxPageSize(t *testing.T) {
	got, err := pagination.Normalize(pagination.Params{Page: 1, PageSize: pagination.MaxPageSize})
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if got.PageSize != pagination.MaxPageSize {
		t.Errorf("PageSize = %d, want %d", got.PageSize, pagination.MaxPageSize)
	}
}

func TestNormalizeRejectsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		params pagination.Params
	}{
		{"negative page", pagination.Params{Page: -1}},
		{"negative pageSize", pagination.Params{Page: 1, PageSize: -1}},
		{"pageSize above max", pagination.Params{Page: 1, PageSize: pagination.MaxPageSize + 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pagination.Normalize(tc.params)
			if !errors.Is(err, pagination.ErrInvalidPagination) {
				t.Fatalf("err = %v, want ErrInvalidPagination", err)
			}
		})
	}
}

func TestNormalizeTrimsSearch(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  demo ", "demo"},
		{"\t\nfoo\t", "foo"},
		{"   ", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got, err := pagination.Normalize(pagination.Params{Search: tc.in})
		if err != nil {
			t.Fatalf("Normalize(%q) error: %v", tc.in, err)
		}
		if got.Search != tc.want {
			t.Errorf("Search(%q) = %q, want %q", tc.in, got.Search, tc.want)
		}
	}
}
