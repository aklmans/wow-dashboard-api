package store

import (
	"context"
	"strings"
	"testing"

	"github.com/aklmans/wow-dashboard-api/internal/config"
)

func TestSanitizeDatabaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty URL",
			input:    "",
			expected: "",
		},
		{
			name:     "URL with password",
			input:    "postgres://user:supersecretpassword@localhost:5432/dbname?sslmode=disable",
			expected: "postgres://user:xxxxxx@localhost:5432/dbname?sslmode=disable",
		},
		{
			name:     "URL without password",
			input:    "postgres://user@localhost:5432/dbname",
			expected: "postgres://user@localhost:5432/dbname",
		},
		{
			name:     "DSN format with password",
			input:    "host=localhost port=5432 dbname=test user=postgres password=secret sslmode=disable",
			expected: "host=localhost port=5432 dbname=test user=postgres password=xxxxxx sslmode=disable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeDatabaseURL(tc.input)
			if got != tc.expected {
				t.Errorf("SanitizeDatabaseURL(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestNewPool_EmptyDatabaseURL(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL: "",
	}
	_, err := NewPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("NewPool should return error when DATABASE_URL is empty")
	}
	if err.Error() != "database connection URL (DATABASE_URL) is empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewPool_InvalidDatabaseURL_DoesNotLeak(t *testing.T) {
	cfg := &config.Config{
		DatabaseURL: "postgres://user:supersecretpassword@invalid_host:5432/dbname",
	}
	_, err := NewPool(context.Background(), cfg)
	if err == nil {
		t.Fatal("NewPool should return error for invalid URL/host")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "invalid_host") {
		t.Errorf("expected error message to contain host, got: %s", errStr)
	}
	if strings.Contains(errStr, "supersecretpassword") {
		t.Error("error message leaks sensitive password!")
	}
}
