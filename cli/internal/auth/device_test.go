package auth

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/felinics/kibotos-sdk/cli/internal/testhttp"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeConsoleURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://console.kibo.to", "https://console.kibo.to", false},
		{"https://console.kibo.to/", "https://console.kibo.to", false},
		{"https://console.kibo.to/api", "https://console.kibo.to", false},
		{"http://localhost:5173/api/", "http://localhost:5173", false},
		{"ftp://console.kibo.to", "", true},
		{"", "", true},
	}

	for _, tc := range cases {
		got, err := NormalizeConsoleURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeConsoleURL(%q) expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeConsoleURL(%q) error = %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeConsoleURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDeviceClientStartAndPollApproved(t *testing.T) {
	t.Parallel()

	var polls atomic.Int32
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/device/start":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode start body: %v", err)
			}
			if body["client_name"] != "kibo" {
				t.Fatalf("client_name = %q, want kibo", body["client_name"])
			}
			if body["device_name"] != "Test Laptop" {
				t.Fatalf("device_name = %q, want Test Laptop", body["device_name"])
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(DeviceStartResponse{
				DeviceCode:              "dev_secret_123",
				UserCode:                "ABCD-EFGH",
				VerificationURL:         "https://console.kibo.to/device",
				VerificationURLComplete: "https://console.kibo.to/device?user_code=ABCD-EFGH",
				ExpiresIn:               600,
				Interval:                5,
			})

		case "/api/auth/device/token":
			n := polls.Add(1)
			if n < 2 {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"status":"authorization_pending"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(DeviceTokenResponse{
				Status:           "approved",
				APIKey:           "kb_approved_token_xyz9",
				KeyID:            "key_001",
				OrganizationID:   "org_001",
				OrganizationName: "Acme",
				UserEmail:        "alice@acme.com",
				APIURL:           "https://api.kibo.to/v1",
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewDeviceClient(srv.URL, srv.Client(), "kibo-cli/test")
	if err != nil {
		t.Fatalf("NewDeviceClient() error = %v", err)
	}
	client.PollIntervalOverride = 5 * time.Millisecond

	start, err := client.Start(context.Background(), "Test Laptop")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if start.UserCode != "ABCD-EFGH" {
		t.Fatalf("UserCode = %q, want ABCD-EFGH", start.UserCode)
	}

	tok, err := client.Poll(context.Background(), start)
	if err != nil {
		t.Fatalf("Poll() error = %v", err)
	}
	if tok.APIKey != "kb_approved_token_xyz9" || tok.OrganizationName != "Acme" {
		t.Fatalf("unexpected token response: %+v", tok)
	}
	if polls.Load() != 2 {
		t.Fatalf("expected 2 poll attempts, got %d", polls.Load())
	}
}

func TestDeviceClientPollDeniedAndExpired(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		status   int
		body     string
		wantCode string
	}{
		{
			name:     "denied",
			status:   http.StatusForbidden,
			body:     `{"error":{"code":"access_denied","message":"User denied login"}}`,
			wantCode: "access_denied",
		},
		{
			name:     "expired",
			status:   http.StatusGone,
			body:     `{"error":{"code":"expired_token","message":"Device code expired"}}`,
			wantCode: "expired_token",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client, err := NewDeviceClient(srv.URL, srv.Client(), "kibo-cli/test")
			if err != nil {
				t.Fatalf("NewDeviceClient() error = %v", err)
			}
			client.PollIntervalOverride = 5 * time.Millisecond

			_, err = client.Poll(context.Background(), &DeviceStartResponse{
				DeviceCode: "dev_123",
				ExpiresIn:  60,
				Interval:   1,
			})
			var devErr *DeviceAuthError
			if !errors.As(err, &devErr) {
				t.Fatalf("expected DeviceAuthError, got %v", err)
			}
			if devErr.Code != tc.wantCode {
				t.Fatalf("Code = %q, want %q", devErr.Code, tc.wantCode)
			}
		})
	}
}
