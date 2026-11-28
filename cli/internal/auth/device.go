package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// ClientName is the client identifier sent to the Kibotos Console Device Authorization endpoint.
	ClientName = "kibo"

	defaultPollInterval = 5 * time.Second
	defaultExpiresIn    = 600 * time.Second
)

// DeviceStartResponse is returned by POST /api/auth/device/start.
type DeviceStartResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_url"`
	VerificationURLComplete string `json:"verification_url_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// OrganizationSummary describes an organization returned by Console API or device login.
type OrganizationSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Region      string `json:"region"`
	Role        string `json:"role"`
	Status      string `json:"status"`
}

// DeviceTokenResponse is returned by POST /api/auth/device/token on approval.
// Modern servers issue a user-level CLI session token (access_token = kbs_...);
// legacy api_key responses remain supported for backwards compatibility.
type DeviceTokenResponse struct {
	Status           string                `json:"status"`
	TokenType        string                `json:"token_type,omitempty"`
	AccessToken      string                `json:"access_token,omitempty"`
	SessionID        string                `json:"session_id,omitempty"`
	UserID           string                `json:"user_id,omitempty"`
	UserName         string                `json:"user_name,omitempty"`
	UserEmail        string                `json:"user_email,omitempty"`
	APIKey           string                `json:"api_key,omitempty"`
	KeyID            string                `json:"key_id,omitempty"`
	OrganizationID   string                `json:"organization_id,omitempty"`
	OrganizationName string                `json:"organization_name,omitempty"`
	Organizations    []OrganizationSummary `json:"organizations,omitempty"`
	APIURL           string                `json:"api_url,omitempty"`
}

// EffectiveCredential returns the issued bearer credential and its type ("user_session" or "api_key").
func (r *DeviceTokenResponse) EffectiveCredential() (token string, authType string) {
	if r == nil {
		return "", ""
	}
	if strings.TrimSpace(r.AccessToken) != "" {
		tt := strings.TrimSpace(r.TokenType)
		if tt == "" {
			tt = "user_session"
		}
		return strings.TrimSpace(r.AccessToken), tt
	}
	if strings.TrimSpace(r.APIKey) != "" {
		return strings.TrimSpace(r.APIKey), "api_key"
	}
	return "", ""
}

// DeviceAuthError represents an error returned by the device authorization endpoints.
type DeviceAuthError struct {
	Code    string
	Message string
	Status  int
}

func (e *DeviceAuthError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("device authorization failed (%s): %s", e.Code, e.Message)
	}
	if e.Code != "" {
		return fmt.Sprintf("device authorization failed: %s", e.Code)
	}
	return fmt.Sprintf("device authorization failed with HTTP %d", e.Status)
}

// DeviceClient drives the Kibotos CLI Device Authorization Flow against the Console API.
type DeviceClient struct {
	ConsoleURL           string
	HTTPClient           *http.Client
	UserAgent            string
	PollIntervalOverride time.Duration
}

// NewDeviceClient constructs a DeviceClient normalized for consoleURL.
func NewDeviceClient(consoleURL string, hc *http.Client, userAgent string) (*DeviceClient, error) {
	normalized, err := NormalizeConsoleURL(consoleURL)
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &DeviceClient{
		ConsoleURL: normalized,
		HTTPClient: hc,
		UserAgent:  userAgent,
	}, nil
}

// NormalizeConsoleURL trims trailing slashes and any trailing "/api" suffix.
func NormalizeConsoleURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("console URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid console URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("console URL %q must use http or https scheme", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("console URL %q must include a host", raw)
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.Path = strings.TrimSuffix(u.Path, "/api")
	return u.String(), nil
}

// Start initiates a new CLI device authorization session.
func (c *DeviceClient) Start(ctx context.Context, deviceName string) (*DeviceStartResponse, error) {
	payload := map[string]string{
		"client_name": ClientName,
	}
	if strings.TrimSpace(deviceName) != "" {
		payload["device_name"] = strings.TrimSpace(deviceName)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal device start request: %w", err)
	}

	endpoint := c.ConsoleURL + "/api/auth/device/start"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build device start request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request device start: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device start response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseDeviceError(resp.StatusCode, respBody)
	}

	var start DeviceStartResponse
	if err := json.Unmarshal(respBody, &start); err != nil {
		return nil, fmt.Errorf("decode device start response: %w", err)
	}
	if start.DeviceCode == "" || start.UserCode == "" {
		return nil, errors.New("invalid device start response: missing device_code or user_code")
	}
	if start.VerificationURLComplete == "" {
		start.VerificationURLComplete = start.VerificationURL
	}
	return &start, nil
}

// Poll polls POST /api/auth/device/token until the session is approved, denied, expired, or canceled.
func (c *DeviceClient) Poll(ctx context.Context, start *DeviceStartResponse) (*DeviceTokenResponse, error) {
	if start == nil || start.DeviceCode == "" {
		return nil, errors.New("device_code is required to poll")
	}

	interval := defaultPollInterval
	if start.Interval > 0 {
		interval = time.Duration(start.Interval) * time.Second
	}
	if c.PollIntervalOverride > 0 {
		interval = c.PollIntervalOverride
	}

	expiresIn := defaultExpiresIn
	if start.ExpiresIn > 0 {
		expiresIn = time.Duration(start.ExpiresIn) * time.Second
	}

	pollCtx, cancel := context.WithTimeout(ctx, expiresIn)
	defer cancel()

	reqBody, err := json.Marshal(map[string]string{
		"device_code": start.DeviceCode,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal device token request: %w", err)
	}

	endpoint := c.ConsoleURL + "/api/auth/device/token"

	for {
		if err := sleepWithContext(pollCtx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, &DeviceAuthError{
					Code:    "expired_token",
					Message: "device login code expired before authorization completed",
					Status:  http.StatusGone,
				}
			}
			return nil, err
		}

		req, err := http.NewRequestWithContext(pollCtx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("build device token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return nil, &DeviceAuthError{
					Code:    "expired_token",
					Message: "device login code expired before authorization completed",
					Status:  http.StatusGone,
				}
			}
			return nil, fmt.Errorf("poll device token: %w", err)
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read device token response: %w", readErr)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			var tok DeviceTokenResponse
			if err := json.Unmarshal(respBody, &tok); err != nil {
				return nil, fmt.Errorf("decode device token response: %w", err)
			}
			if tok.Status == "authorization_pending" {
				continue
			}
			if cred, _ := tok.EffectiveCredential(); cred == "" {
				return nil, errors.New("device authorization approved without a credential token")
			}
			return &tok, nil

		case http.StatusAccepted:
			// Still awaiting user approval in the browser.
			continue

		case http.StatusTooManyRequests:
			if c.PollIntervalOverride <= 0 {
				if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
					if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
						interval = time.Duration(secs) * time.Second
					}
				} else {
					interval += 2 * time.Second
				}
			}
			continue

		case http.StatusForbidden:
			devErr := parseDeviceError(resp.StatusCode, respBody)
			if devErr.Code == "http_error" {
				devErr.Code = "access_denied"
			}
			return nil, devErr

		case http.StatusGone:
			devErr := parseDeviceError(resp.StatusCode, respBody)
			if devErr.Code == "http_error" {
				devErr.Code = "expired_token"
			}
			return nil, devErr

		default:
			return nil, parseDeviceError(resp.StatusCode, respBody)
		}
	}
}

type apiErrorEnvelope struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseDeviceError(status int, body []byte) *DeviceAuthError {
	var env apiErrorEnvelope
	if len(body) > 0 && json.Unmarshal(body, &env) == nil {
		if env.Error != nil && (env.Error.Code != "" || env.Error.Message != "") {
			code := env.Error.Code
			if code == "" {
				code = "http_error"
			}
			return &DeviceAuthError{
				Code:    code,
				Message: env.Error.Message,
				Status:  status,
			}
		}
		if env.Code != "" || env.Message != "" {
			code := env.Code
			if code == "" {
				code = "http_error"
			}
			return &DeviceAuthError{
				Code:    code,
				Message: env.Message,
				Status:  status,
			}
		}
	}

	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &DeviceAuthError{
		Code:    "http_error",
		Message: msg,
		Status:  status,
	}
}

// ListOrganizations fetches organizations accessible to the signed-in user session (Bearer kbs_...).
func (c *DeviceClient) ListOrganizations(ctx context.Context, accessToken string) ([]OrganizationSummary, error) {
	endpoint := c.ConsoleURL + "/api/organizations"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build list organizations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read organizations response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseDeviceError(resp.StatusCode, respBody)
	}
	var orgs []OrganizationSummary
	if err := json.Unmarshal(respBody, &orgs); err != nil {
		return nil, fmt.Errorf("decode organizations response: %w", err)
	}
	return orgs, nil
}

// LogoutSession revokes a CLI user session on the Console API (POST /api/auth/logout).
func (c *DeviceClient) LogoutSession(ctx context.Context, accessToken string) error {
	endpoint := c.ConsoleURL + "/api/auth/logout"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build logout request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("logout session: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return &DeviceAuthError{Code: "logout_failed", Message: fmt.Sprintf("server returned HTTP %d", resp.StatusCode), Status: resp.StatusCode}
	}
	return nil
}

// OpenBrowser opens targetURL in the user's default browser on a best-effort basis.
func OpenBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
