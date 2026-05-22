package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func TestRequestLoggerRecordsStructuredFieldsAndRedactsSensitiveInput(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	handler := chimiddleware.RequestID(RequestLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "wow_dashboard_refresh_token", Value: "response-cookie-secret"})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})))

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/users?search=demo&accessToken=access-token-secret&password=password-secret&role=admin",
		nil,
	)
	req.RemoteAddr = "203.0.113.10:12345"
	req.Header.Set("Authorization", "Bearer authorization-secret")
	req.Header.Set("Cookie", "wow_dashboard_refresh_token=request-cookie-secret")
	req.Header.Set("User-Agent", "requestlog-test")

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	logLine := logs.String()
	if logLine == "" {
		t.Fatal("expected request log line, got empty output")
	}
	for _, secret := range []string{
		"access-token-secret",
		"password-secret",
		"authorization-secret",
		"request-cookie-secret",
		"response-cookie-secret",
	} {
		if strings.Contains(logLine, secret) {
			t.Fatalf("request log leaked secret %q in %s", secret, logLine)
		}
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(logLine), &entry); err != nil {
		t.Fatalf("request log is not JSON: %v; log=%s", err, logLine)
	}

	assertStringField(t, entry, "msg", "http_request")
	assertStringField(t, entry, "method", http.MethodGet)
	assertStringField(t, entry, "path", "/api/users")
	assertStringField(t, entry, "remote_addr", "203.0.113.10:12345")
	assertStringField(t, entry, "user_agent", "requestlog-test")
	if got := entry["request_id"]; got == "" || got == nil {
		t.Fatalf("request_id = %v, want non-empty request id", got)
	}
	if got := entry["status"]; got != float64(http.StatusCreated) {
		t.Fatalf("status = %#v, want %d", got, http.StatusCreated)
	}
	if got, ok := entry["duration_ms"].(float64); !ok || got < 0 {
		t.Fatalf("duration_ms = %#v, want non-negative number", entry["duration_ms"])
	}
	query, ok := entry["query"].(string)
	if !ok {
		t.Fatalf("query = %#v, want string", entry["query"])
	}
	if !strings.Contains(query, "search=demo") || !strings.Contains(query, "role=admin") {
		t.Fatalf("query = %q, want non-sensitive parameters preserved", query)
	}
	if !strings.Contains(query, "accessToken=%5BREDACTED%5D") || !strings.Contains(query, "password=%5BREDACTED%5D") {
		t.Fatalf("query = %q, want sensitive parameters redacted", query)
	}
}

func assertStringField(t *testing.T, entry map[string]any, key, want string) {
	t.Helper()
	if got := entry[key]; got != want {
		t.Fatalf("%s = %#v, want %q", key, got, want)
	}
}
