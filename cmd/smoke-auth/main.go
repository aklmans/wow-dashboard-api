package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	defaultSmokeBaseURL  = "http://localhost:7272"
	defaultSmokeEmail    = "demo@wow-dashboard.test"
	defaultSmokePassword = "@2Minimal"
	refreshCookieName    = "wow_dashboard_refresh_token"
	redactedValue        = "[REDACTED]"
)

var sensitiveTextFieldPattern = regexp.MustCompile(`(?i)(("?(?:accessToken|refreshToken|setCookie|set-cookie|cookie|token|password)"?\s*[:=]\s*)("[^"]*"|[^\s,;]+))`)

type smokeConfig struct {
	BaseURL  string
	Email    string
	Password string
	Client   *http.Client
	Stdout   io.Writer
}

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signInResponse struct {
	User struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
	AccessToken string `json:"accessToken"`
}

type meResponse struct {
	User struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"user"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	jar, err := cookiejar.New(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke-auth failed: initialize cookie jar: %v\n", err)
		os.Exit(1)
	}

	cfg := smokeConfig{
		BaseURL:  smokeBaseURLFromEnv(),
		Email:    envOrDefault("SMOKE_AUTH_EMAIL", defaultSmokeEmail),
		Password: envOrDefault("SMOKE_AUTH_PASSWORD", defaultSmokePassword),
		Client:   &http.Client{Timeout: 5 * time.Second, Jar: jar},
		Stdout:   os.Stdout,
	}
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "smoke-auth failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg smokeConfig) error {
	var err error
	if cfg.Client == nil {
		cfg.Client, err = newSmokeHTTPClient()
		if err != nil {
			return err
		}
	} else if cfg.Client.Jar == nil {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return fmt.Errorf("initialize cookie jar: %w", err)
		}
		cfg.Client.Jar = jar
	}
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		return fmt.Errorf("base URL is empty")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return fmt.Errorf("parse base URL: %w", err)
	}
	if cfg.Email == "" || cfg.Password == "" {
		return fmt.Errorf("smoke credentials are empty")
	}

	if err := getOK(ctx, cfg.Client, cfg.BaseURL+"/healthz"); err != nil {
		return fmt.Errorf("GET /healthz: %w", err)
	}
	fmt.Fprintln(cfg.Stdout, "OK /healthz")

	if err := getOK(ctx, cfg.Client, cfg.BaseURL+"/readyz"); err != nil {
		return fmt.Errorf("GET /readyz: %w", err)
	}
	fmt.Fprintln(cfg.Stdout, "OK /readyz")

	signInBody, sawRefreshSetCookie, err := postSignIn(ctx, cfg)
	if err != nil {
		return err
	}
	if signInBody.User.Email != cfg.Email {
		return fmt.Errorf("sign-in user.email = %q, want %q", signInBody.User.Email, cfg.Email)
	}
	if signInBody.User.Role == "" {
		return fmt.Errorf("sign-in user.role is empty")
	}
	if signInBody.AccessToken == "" {
		return fmt.Errorf("sign-in accessToken is empty")
	}
	if !sawRefreshSetCookie {
		return fmt.Errorf("sign-in did not set refresh cookie")
	}
	initialRefreshCookie, ok := refreshCookieValue(cfg.Client, baseURL)
	if !ok {
		return fmt.Errorf("sign-in refresh cookie missing from cookie jar")
	}
	fmt.Fprintf(cfg.Stdout, "OK /api/auth/sign-in as %s role=%s\n", signInBody.User.Email, signInBody.User.Role)

	meBody, err := getMe(ctx, cfg.Client, cfg.BaseURL+"/api/auth/me", signInBody.AccessToken)
	if err != nil {
		return err
	}
	if meBody.User.Email != cfg.Email {
		return fmt.Errorf("me user.email = %q, want %q", meBody.User.Email, cfg.Email)
	}
	if meBody.User.Role != "admin" {
		return fmt.Errorf("me user.role = %q, want admin", meBody.User.Role)
	}
	fmt.Fprintf(cfg.Stdout, "OK /api/auth/me as %s role=%s\n", meBody.User.Email, meBody.User.Role)

	refreshBody, sawRotatedSetCookie, err := postRefresh(ctx, cfg)
	if err != nil {
		return err
	}
	if refreshBody.User.Email != cfg.Email {
		return fmt.Errorf("refresh user.email = %q, want %q", refreshBody.User.Email, cfg.Email)
	}
	if refreshBody.User.Role == "" {
		return fmt.Errorf("refresh user.role is empty")
	}
	if refreshBody.AccessToken == "" {
		return fmt.Errorf("refresh accessToken is empty")
	}
	if !sawRotatedSetCookie {
		return fmt.Errorf("refresh did not set rotated refresh cookie")
	}
	rotatedRefreshCookie, ok := refreshCookieValue(cfg.Client, baseURL)
	if !ok {
		return fmt.Errorf("rotated refresh cookie missing from cookie jar")
	}
	if rotatedRefreshCookie == initialRefreshCookie {
		return fmt.Errorf("refresh cookie was not rotated")
	}
	fmt.Fprintln(cfg.Stdout, "OK /api/auth/refresh rotated refresh cookie")

	refreshedMeBody, err := getMe(ctx, cfg.Client, cfg.BaseURL+"/api/auth/me", refreshBody.AccessToken)
	if err != nil {
		return err
	}
	if refreshedMeBody.User.Email != cfg.Email {
		return fmt.Errorf("refreshed me user.email = %q, want %q", refreshedMeBody.User.Email, cfg.Email)
	}
	if refreshedMeBody.User.Role != "admin" {
		return fmt.Errorf("refreshed me user.role = %q, want admin", refreshedMeBody.User.Role)
	}
	fmt.Fprintf(cfg.Stdout, "OK /api/auth/me with refreshed access token as %s role=%s\n", refreshedMeBody.User.Email, refreshedMeBody.User.Role)

	if err := postRefreshWithCookieExpectStatus(ctx, cfg, initialRefreshCookie, http.StatusUnauthorized); err != nil {
		return err
	}
	fmt.Fprintln(cfg.Stdout, "OK old refresh token rejected")

	sawClearCookie, err := postSignOut(ctx, cfg)
	if err != nil {
		return err
	}
	if !sawClearCookie {
		return fmt.Errorf("sign-out did not clear refresh cookie")
	}
	if _, ok := refreshCookieValue(cfg.Client, baseURL); ok {
		return fmt.Errorf("refresh cookie still present after sign-out")
	}
	fmt.Fprintln(cfg.Stdout, "OK /api/auth/sign-out cleared refresh cookie")

	if err := postRefreshExpectStatus(ctx, cfg, http.StatusUnauthorized); err != nil {
		return err
	}
	fmt.Fprintln(cfg.Stdout, "OK refresh after sign-out rejected")
	return nil
}

func newSmokeHTTPClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("initialize cookie jar: %w", err)
	}
	return &http.Client{Timeout: 5 * time.Second, Jar: jar}, nil
}

func getOK(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, safeResponseBodySnippet(resp.Body))
	}
	return nil
}

func postSignIn(ctx context.Context, cfg smokeConfig) (*signInResponse, bool, error) {
	payload, err := json.Marshal(signInRequest{
		Email:    cfg.Email,
		Password: cfg.Password,
	})
	if err != nil {
		return nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/auth/sign-in", bytes.NewReader(payload))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("POST /api/auth/sign-in: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("POST /api/auth/sign-in status %d: %s", resp.StatusCode, safeResponseBodySnippet(resp.Body))
	}

	var body signInResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, false, fmt.Errorf("decode sign-in response: %w", err)
	}
	return &body, hasRefreshSetCookie(resp.Cookies()), nil
}

func getMe(ctx context.Context, client *http.Client, url string, accessToken string) (*meResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/auth/me: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /api/auth/me status %d: %s", resp.StatusCode, safeResponseBodySnippet(resp.Body))
	}

	var body meResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode me response: %w", err)
	}
	return &body, nil
}

func postRefresh(ctx context.Context, cfg smokeConfig) (*signInResponse, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/auth/refresh", nil)
	if err != nil {
		return nil, false, err
	}

	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("POST /api/auth/refresh: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("POST /api/auth/refresh status %d: %s", resp.StatusCode, safeResponseBodySnippet(resp.Body))
	}

	var body signInResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, false, fmt.Errorf("decode refresh response: %w", err)
	}
	return &body, hasRefreshSetCookie(resp.Cookies()), nil
}

func postRefreshExpectStatus(ctx context.Context, cfg smokeConfig, wantStatus int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/auth/refresh", nil)
	if err != nil {
		return err
	}
	return doExpectStatus(ctx, cfg.Client, req, "POST /api/auth/refresh", wantStatus)
}

func postRefreshWithCookieExpectStatus(ctx context.Context, cfg smokeConfig, refreshCookie string, wantStatus int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/auth/refresh", nil)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: refreshCookie})
	client := *cfg.Client
	client.Jar = nil
	return doExpectStatus(ctx, &client, req, "POST /api/auth/refresh with previous refresh cookie", wantStatus)
}

func postSignOut(ctx context.Context, cfg smokeConfig) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/auth/sign-out", nil)
	if err != nil {
		return false, err
	}

	resp, err := cfg.Client.Do(req)
	if err != nil {
		return false, fmt.Errorf("POST /api/auth/sign-out: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("POST /api/auth/sign-out status %d: %s", resp.StatusCode, safeResponseBodySnippet(resp.Body))
	}
	return hasClearedRefreshCookie(resp.Cookies()), nil
}

func doExpectStatus(ctx context.Context, client *http.Client, req *http.Request, step string, wantStatus int) error {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("%s: %w", step, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("%s status %d, want %d: %s", step, resp.StatusCode, wantStatus, safeResponseBodySnippet(resp.Body))
	}
	return nil
}

func refreshCookieValue(client *http.Client, baseURL *url.URL) (string, bool) {
	if client == nil || client.Jar == nil || baseURL == nil {
		return "", false
	}
	cookieURL := *baseURL
	cookieURL.Path = "/api/auth/refresh"
	for _, cookie := range client.Jar.Cookies(&cookieURL) {
		if cookie.Name == refreshCookieName && cookie.Value != "" {
			return cookie.Value, true
		}
	}
	return "", false
}

func hasRefreshSetCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name == refreshCookieName && cookie.Value != "" && cookie.HttpOnly {
			return true
		}
	}
	return false
}

func hasClearedRefreshCookie(cookies []*http.Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name == refreshCookieName && cookie.Value == "" && cookie.MaxAge < 0 && cookie.HttpOnly {
			return true
		}
	}
	return false
}

func safeResponseBodySnippet(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 2048))
	snippet := strings.TrimSpace(string(data))
	if snippet == "" {
		return ""
	}

	var value any
	if err := json.Unmarshal([]byte(snippet), &value); err == nil {
		redactSensitiveJSONFields(value)
		redacted, err := json.Marshal(value)
		if err == nil {
			return string(redacted)
		}
	}
	return redactSensitiveTextFields(snippet)
}

func redactSensitiveJSONFields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitiveBodyField(key) {
				typed[key] = redactedValue
				continue
			}
			redactSensitiveJSONFields(child)
		}
	case []any:
		for _, child := range typed {
			redactSensitiveJSONFields(child)
		}
	}
}

func isSensitiveBodyField(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	switch normalized {
	case "accesstoken", "refreshtoken", "token", "password", "setcookie", "cookie":
		return true
	default:
		return false
	}
}

func redactSensitiveTextFields(snippet string) string {
	return sensitiveTextFieldPattern.ReplaceAllStringFunc(snippet, func(match string) string {
		separator := strings.LastIndexAny(match, ":=")
		if separator < 0 {
			return redactedValue
		}
		return match[:separator+1] + redactedValue
	})
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func smokeBaseURLFromEnv() string {
	return envOrDefault("BASE_URL", envOrDefault("SMOKE_AUTH_BASE_URL", defaultSmokeBaseURL))
}
