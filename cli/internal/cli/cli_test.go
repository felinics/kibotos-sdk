package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
	"github.com/felinics/kibotos-sdk/cli/internal/config"
)

type testHarness struct {
	store          *config.Store
	env            map[string]string
	httpClient     *http.Client
	openedURLs     []string
	openBrowserErr error
}

func newTestHarness(t *testing.T, hc *http.Client) *testHarness {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	h := &testHarness{
		env:        make(map[string]string),
		httpClient: hc,
	}
	h.store = config.NewStoreAt(cfgPath, func(k string) string { return h.env[k] })
	return h
}

func (h *testHarness) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, Options{
		Stdout:               &stdout,
		Stderr:               &stderr,
		Store:                h.store,
		EnvLookup:            func(k string) string { return h.env[k] },
		HTTPClient:           h.httpClient,
		PollIntervalOverride: 5 * time.Millisecond,
		OpenBrowser: func(u string) error {
			h.openedURLs = append(h.openedURLs, u)
			return h.openBrowserErr
		},
		Build: BuildInfo{Version: "0.1.0-test", Commit: "abc1234"},
	})
	return code, stdout.String(), stderr.String()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Request-Id", "req_test_001")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Request-Id", "req_err_001")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    msg,
			"status":     status,
			"request_id": "req_err_001",
			"retryable":  retryable,
		},
	})
}

func TestLoginDeviceFlowAndWhoami(t *testing.T) {
	t.Parallel()

	var pollCount atomic.Int32
	var apiSrv *httptest.Server
	apiSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/zones":
			if got := r.Header.Get("Authorization"); got != "Bearer kb_device_secret_1234567890abcd" {
				writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "bad token", false)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"data": []kibotos.Zone{
					{ID: "global-1", DisplayName: "Global 1", Region: "global", Status: kibotos.ZoneStatusAvailable, Default: true},
				},
				"next_page_token": nil,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	consoleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/device/start":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["client_name"] != "kibo" {
				t.Errorf("client_name = %q, want kibo", body["client_name"])
			}
			writeJSON(w, http.StatusCreated, auth.DeviceStartResponse{
				DeviceCode:              "dev_code_1",
				UserCode:                "WXYZ-1234",
				VerificationURL:         "https://console.kibo.to/device",
				VerificationURLComplete: "https://console.kibo.to/device?user_code=WXYZ-1234",
				ExpiresIn:               300,
				Interval:                5,
			})
		case "/api/auth/device/token":
			if pollCount.Add(1) == 1 {
				writeJSON(w, http.StatusAccepted, map[string]string{"status": "authorization_pending"})
				return
			}
			writeJSON(w, http.StatusOK, auth.DeviceTokenResponse{
				Status:           "approved",
				APIKey:           "kb_device_secret_1234567890abcd",
				KeyID:            "key_dev_1",
				OrganizationID:   "org_acme",
				OrganizationName: "Acme Corp",
				UserEmail:        "dev@acme.com",
				APIURL:           apiSrv.URL + "/v1",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer consoleSrv.Close()

	h := newTestHarness(t, apiSrv.Client())

	code, stdout, stderr := h.run("login", "--console-url", consoleSrv.URL, "--device-name", "CI-Box", "--json")
	if code != ExitOK {
		t.Fatalf("login exit = %d, want 0; stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "WXYZ-1234") || !strings.Contains(stderr, "user_code=WXYZ-1234") {
		t.Fatalf("expected user_code and verification URL on stderr, got: %s", stderr)
	}
	if len(h.openedURLs) != 1 || h.openedURLs[0] != "https://console.kibo.to/device?user_code=WXYZ-1234" {
		t.Fatalf("expected browser to open verification URL once, got %v", h.openedURLs)
	}

	var loginOut loginResult
	if err := json.Unmarshal([]byte(stdout), &loginOut); err != nil {
		t.Fatalf("stdout must be valid JSON: %v\nstdout=%s", err, stdout)
	}
	if loginOut.OrganizationName != "Acme Corp" || loginOut.APIKeyMasked != "kb_...abcd" {
		t.Fatalf("unexpected login result: %+v", loginOut)
	}
	if strings.Contains(stdout, "kb_device_secret_1234567890abcd") {
		t.Fatalf("full API key must never be printed to stdout")
	}

	// Verify live whoami works with saved profile.
	code, stdout, stderr = h.run("whoami", "--json")
	if code != ExitOK {
		t.Fatalf("whoami exit = %d, stderr=%s", code, stderr)
	}
	var who whoamiResult
	if err := json.Unmarshal([]byte(stdout), &who); err != nil {
		t.Fatalf("whoami JSON decode error: %v", err)
	}
	if !who.Authenticated || who.AuthSource != "profile:default" || len(who.Zones) != 1 || who.Zones[0].ID != "global-1" {
		t.Fatalf("unexpected whoami output: %+v", who)
	}

	// Verify whoami --offline skips HTTP check.
	code, stdout, stderr = h.run("whoami", "--offline", "--json")
	if code != ExitOK {
		t.Fatalf("whoami --offline exit = %d, stderr=%s", code, stderr)
	}
	var whoOff whoamiResult
	if err := json.Unmarshal([]byte(stdout), &whoOff); err != nil {
		t.Fatalf("whoami --offline decode error: %v", err)
	}
	if !whoOff.Offline || len(whoOff.Zones) != 0 {
		t.Fatalf("expected offline=true and no zones, got %+v", whoOff)
	}
}

func TestLoginAPIKeyAndProfilesAndLogout(t *testing.T) {
	t.Parallel()

	var zoneHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/zones" {
			zoneHits.Add(1)
			if r.Header.Get("Authorization") == "Bearer kb_invalid_key_0000" {
				writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "invalid key", false)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"data":            []kibotos.Zone{{ID: "global-1", Status: kibotos.ZoneStatusAvailable, Default: true}},
				"next_page_token": nil,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())

	// Invalid API key returns exit 3 (unauthenticated).
	code, stdout, stderr := h.run("login", "--api-key", "kb_invalid_key_0000", "--api-url", srv.URL, "--json")
	if code != ExitUnauthenticated {
		t.Fatalf("invalid key login exit = %d, want %d; stderr=%s", code, ExitUnauthenticated, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout on error in --json mode, got %q", stdout)
	}

	// Valid API key login saves profile "prod" and verifies via GET /v1/zones.
	code, stdout, stderr = h.run("login", "--api-key", "kb_valid_prod_key_9999", "--api-url", srv.URL, "--profile", "prod", "--json")
	if code != ExitOK {
		t.Fatalf("valid key login exit = %d, stderr=%s", code, stderr)
	}
	var res loginResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("decode login json: %v", err)
	}
	if res.Profile != "prod" || !res.Verified || res.APIKeyMasked != "kb_...9999" {
		t.Fatalf("unexpected login result: %+v", res)
	}

	// Second profile with --skip-verify does not call /v1/zones.
	hitsBefore := zoneHits.Load()
	code, _, stderr = h.run("login", "--api-key", "kb_valid_dev_key_1111", "--api-url", srv.URL, "--profile", "dev", "--skip-verify")
	if code != ExitOK {
		t.Fatalf("skip-verify login exit = %d, stderr=%s", code, stderr)
	}
	if zoneHits.Load() != hitsBefore {
		t.Fatalf("expected --skip-verify to skip /v1/zones call")
	}

	// Profile list / use / delete.
	code, stdout, _ = h.run("profile", "list", "--json")
	if code != ExitOK {
		t.Fatalf("profile list exit = %d", code)
	}
	var plist profileListResult
	if err := json.Unmarshal([]byte(stdout), &plist); err != nil {
		t.Fatalf("decode profile list: %v", err)
	}
	if plist.CurrentProfile != "dev" || len(plist.Profiles) != 2 {
		t.Fatalf("unexpected profile list: %+v", plist)
	}

	code, _, stderr = h.run("profile", "use", "prod")
	if code != ExitOK {
		t.Fatalf("profile use prod exit = %d, stderr=%s", code, stderr)
	}

	code, _, stderr = h.run("profile", "delete", "dev")
	if code != ExitOK {
		t.Fatalf("profile delete dev exit = %d, stderr=%s", code, stderr)
	}

	// Logout active profile ("prod").
	code, stdout, stderr = h.run("logout", "--json")
	if code != ExitOK {
		t.Fatalf("logout exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"removed": true`) {
		t.Fatalf("unexpected logout output: %s", stdout)
	}

	// Whoami now fails with exit 3.
	code, stdout, stderr = h.run("whoami", "--json")
	if code != ExitUnauthenticated {
		t.Fatalf("whoami after logout exit = %d, want %d; stderr=%s", code, ExitUnauthenticated, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout on unauthenticated error, got %q", stdout)
	}
	if !strings.Contains(stderr, `"code": "unauthenticated"`) {
		t.Fatalf("expected JSON error envelope on stderr, got %s", stderr)
	}
}

func TestWorkspaceCommandsEndToEnd(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	var netPolls atomic.Int32
	var delPolls atomic.Int32

	makeWS := func(status kibotos.WorkspaceStatus, netMode kibotos.NetworkMode, netApplied bool) kibotos.Workspace {
		return kibotos.Workspace{
			ID:        "wks_123",
			Name:      "agent-runner",
			Zone:      "global-1",
			Template:  kibotos.WorkspaceTemplate{ID: "python-3.12", Revision: "r42"},
			Status:    status,
			Resources: kibotos.WorkspaceResources{VCPUs: 2, MemoryMiB: 4096, DiskMiB: 20480},
			Network:   kibotos.WorkspaceNetwork{Mode: netMode, Applied: netApplied},
			Volume:    &kibotos.WorkspaceVolume{ID: "vol_1", MountPath: "/data", DeleteWithWorkspace: false},
			Timeout:   &kibotos.WorkspaceTimeout{ExpiresAt: now.Add(time.Hour), Action: kibotos.TimeoutActionStop},
			Labels:    map[string]string{"env": "test"},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces":
			var body kibotos.CreateWorkspaceParams
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create workspace: %v", err)
			}
			if body.Template.ID != "python-3.12" || body.Template.Revision != "r42" {
				t.Errorf("unexpected template: %+v", body.Template)
			}
			if body.Resources == nil || body.Resources.VCPUs != 2 || body.Resources.MemoryMiB != 4096 || body.Resources.DiskMiB != 20480 {
				t.Errorf("unexpected resources: %+v", body.Resources)
			}
			if body.Network == nil || body.Network.Mode != kibotos.NetworkModePublicInternet {
				t.Errorf("unexpected network: %+v", body.Network)
			}
			if body.Volume == nil || body.Volume.ID != "vol_1" || body.Volume.MountPath != "/data" || !body.Volume.DeleteWithWorkspace {
				t.Errorf("unexpected volume: %+v", body.Volume)
			}
			if body.Timeout == nil || body.Timeout.Seconds != 1800 || body.Timeout.Action != kibotos.TimeoutActionStop {
				t.Errorf("unexpected timeout: %+v", body.Timeout)
			}
			if body.Labels["env"] != "test" {
				t.Errorf("unexpected labels: %+v", body.Labels)
			}
			writeJSON(w, http.StatusAccepted, makeWS(kibotos.WorkspaceStatusCreating, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_123/wait":
			statuses := r.URL.Query()["status"]
			target := kibotos.WorkspaceStatusAvailable
			if len(statuses) > 0 {
				target = kibotos.WorkspaceStatus(statuses[0])
			}
			writeJSON(w, http.StatusOK, makeWS(target, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces":
			if got := r.URL.Query().Get("label.env"); got != "test" {
				t.Errorf("expected label.env=test, got %q", got)
			}
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Workspace]{
				Data:             []kibotos.Workspace{makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModePublicInternet, true)},
				UnavailableZones: []string{},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_123":
			if delPolls.Load() > 0 {
				if delPolls.Add(1) >= 3 {
					writeAPIError(w, http.StatusNotFound, "workspace_not_found", "not found", false)
					return
				}
				writeJSON(w, http.StatusOK, makeWS(kibotos.WorkspaceStatusDeleting, kibotos.NetworkModePublicInternet, true))
				return
			}
			if netPolls.Load() > 0 {
				applied := netPolls.Add(1) >= 3
				writeJSON(w, http.StatusOK, makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModeDenyAll, applied))
				return
			}
			writeJSON(w, http.StatusOK, makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/workspaces/wks_123":
			var raw map[string]any
			_ = json.NewDecoder(r.Body).Decode(&raw)
			if _, hasName := raw["name"]; !hasName || raw["name"] != nil {
				t.Errorf("expected explicit name:null for --clear-name, got %+v", raw)
			}
			ws := makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModePublicInternet, true)
			ws.Name = ""
			writeJSON(w, http.StatusOK, ws)

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_123/stop":
			writeJSON(w, http.StatusAccepted, makeWS(kibotos.WorkspaceStatusStopping, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_123/start":
			writeJSON(w, http.StatusAccepted, makeWS(kibotos.WorkspaceStatusStarting, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_123/timeout":
			writeJSON(w, http.StatusOK, makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/wks_123/network":
			netPolls.Store(1)
			writeJSON(w, http.StatusAccepted, makeWS(kibotos.WorkspaceStatusAvailable, kibotos.NetworkModeDenyAll, false))

		case r.Method == http.MethodPut && r.URL.Path == "/v1/workspaces/wks_123/volume":
			writeJSON(w, http.StatusOK, makeWS(kibotos.WorkspaceStatusStopped, kibotos.NetworkModePublicInternet, true))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_123/volume":
			ws := makeWS(kibotos.WorkspaceStatusStopped, kibotos.NetworkModePublicInternet, true)
			ws.Volume = nil
			writeJSON(w, http.StatusOK, ws)

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_123":
			delPolls.Store(1)
			writeJSON(w, http.StatusAccepted, makeWS(kibotos.WorkspaceStatusDeleting, kibotos.NetworkModePublicInternet, true))

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_KEY"] = "kb_test_workspace_key_1234"
	h.env["KIBOTOS_API_URL"] = srv.URL

	// Create with --wait --json: stdout MUST be valid JSON only, progress on stderr.
	code, stdout, stderr := h.run(
		"workspace", "create",
		"--template", "python-3.12",
		"--revision", "r42",
		"--name", "agent-runner",
		"--zone", "global-1",
		"--vcpus", "2",
		"--memory", "4096",
		"--disk", "20480",
		"--network", "public_internet",
		"--volume", "vol_1",
		"--mount-path", "/data",
		"--delete-volume",
		"--timeout", "1800",
		"--timeout-action", "stop",
		"--label", "env=test",
		"--wait",
		"--wait-timeout", "10",
		"--json",
	)
	if code != ExitOK {
		t.Fatalf("workspace create exit = %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "Waiting for workspace wks_123") {
		t.Fatalf("expected wait progress on stderr, got: %q", stderr)
	}
	var created kibotos.Workspace
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("stdout must be pure valid JSON: %v\nstdout=%s", err, stdout)
	}
	if created.Status != kibotos.WorkspaceStatusAvailable {
		t.Fatalf("expected available after --wait, got %q", created.Status)
	}

	// List using alias `ws`.
	code, stdout, stderr = h.run("ws", "list", "--label", "env=test", "--json")
	if code != ExitOK {
		t.Fatalf("ws list exit = %d, stderr=%s", code, stderr)
	}
	var listPage kibotos.MultiZonePage[kibotos.Workspace]
	if err := json.Unmarshal([]byte(stdout), &listPage); err != nil || len(listPage.Data) != 1 {
		t.Fatalf("unexpected ws list json: err=%v stdout=%s", err, stdout)
	}

	// Get (table mode).
	code, stdout, stderr = h.run("ws", "get", "wks_123")
	if code != ExitOK || !strings.Contains(stdout, "wks_123") {
		t.Fatalf("ws get exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// Update --clear-name --label a=b --unset-label old.
	code, _, stderr = h.run("ws", "update", "wks_123", "--clear-name", "--label", "a=b", "--unset-label", "old", "--json")
	if code != ExitOK {
		t.Fatalf("ws update exit = %d, stderr=%s", code, stderr)
	}

	// Stop --force --wait.
	code, stdout, stderr = h.run("ws", "stop", "wks_123", "--force", "--wait", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"status": "stopped"`) {
		t.Fatalf("ws stop exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	// Start --timeout 3600 --timeout-action stop --wait.
	code, stdout, stderr = h.run("ws", "start", "wks_123", "--timeout", "3600", "--timeout-action", "stop", "--wait", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"status": "available"`) {
		t.Fatalf("ws start exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	// Timeout.
	code, _, stderr = h.run("ws", "timeout", "wks_123", "--seconds", "7200", "--action", "delete", "--json")
	if code != ExitOK {
		t.Fatalf("ws timeout exit=%d stderr=%s", code, stderr)
	}

	// Network --mode deny_all --wait.
	code, stdout, stderr = h.run("ws", "network", "wks_123", "--mode", "deny_all", "--wait", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"applied": true`) {
		t.Fatalf("ws network exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	netPolls.Store(0)

	// Volume attach / detach.
	code, _, stderr = h.run("ws", "volume", "attach", "wks_123", "--volume", "vol_1", "--mount-path", "/data", "--delete-with-workspace")
	if code != ExitOK {
		t.Fatalf("ws volume attach exit=%d stderr=%s", code, stderr)
	}
	code, _, stderr = h.run("ws", "volume", "detach", "wks_123")
	if code != ExitOK {
		t.Fatalf("ws volume detach exit=%d stderr=%s", code, stderr)
	}

	// Wait command with comma-separated statuses.
	code, stdout, stderr = h.run("ws", "wait", "wks_123", "--status", "available,failed", "--timeout", "10", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"status": "available"`) {
		t.Fatalf("ws wait exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}

	// Delete --wait.
	code, stdout, stderr = h.run("ws", "delete", "wks_123", "--wait", "--wait-timeout", "10", "--json")
	if code != ExitOK || !strings.Contains(stdout, `"status": "deleted"`) {
		t.Fatalf("ws delete --wait exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func TestVolumeExposureTemplateZoneAndVersionCommands(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	sizeMiB := int64(2048)
	var volDelPolls atomic.Int32

	makeVol := func(status kibotos.VolumeStatus, size int) kibotos.Volume {
		return kibotos.Volume{
			ID:        "vol_100",
			Name:      "datasets",
			Zone:      "global-1",
			SizeGiB:   size,
			Status:    status,
			Labels:    map[string]string{"tier": "fast"},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	makeExp := func(status kibotos.ExposureStatus) kibotos.Exposure {
		return kibotos.Exposure{
			ID:          "exp_100",
			WorkspaceID: "wks_123",
			Port:        8080,
			URL:         "https://abc123.kibo.run",
			Status:      status,
			ExpiresAt:   now.Add(time.Hour),
			CreatedAt:   now,
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/zones":
			writeJSON(w, http.StatusOK, kibotos.Page[kibotos.Zone]{
				Data: []kibotos.Zone{
					{ID: "global-1", DisplayName: "Global 1", Region: "global", Country: kibotos.Ptr("JP"), Status: kibotos.ZoneStatusAvailable, Default: true},
					{ID: "eu-1", DisplayName: "EU 1", Region: "eu", Country: nil, Status: kibotos.ZoneStatusDraining, Default: false},
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates":
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Template]{
				Data: []kibotos.Template{
					{ID: "python-3.12", Owner: kibotos.TemplateOwnerPlatform, DisplayName: "Python 3.12", DefaultRevision: "r42", Zones: []string{"global-1"}},
				},
				UnavailableZones: []string{},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates/python-3.12":
			writeJSON(w, http.StatusOK, kibotos.Template{
				ID: "python-3.12", Owner: kibotos.TemplateOwnerPlatform, DisplayName: "Python 3.12", DefaultRevision: "r42", Zones: []string{"global-1"},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates/python-3.12/revisions":
			writeJSON(w, http.StatusOK, kibotos.Page[kibotos.TemplateRevision]{
				Data: []kibotos.TemplateRevision{
					{ID: "r42", TemplateID: "python-3.12", Status: "ready", SizeMiB: &sizeMiB, Zones: []string{"global-1"}},
				},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates/python-3.12/revisions/r42":
			writeJSON(w, http.StatusOK, kibotos.TemplateRevision{
				ID: "r42", TemplateID: "python-3.12", Status: "ready", SizeMiB: &sizeMiB, Zones: []string{"global-1"},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/volumes":
			writeJSON(w, http.StatusAccepted, makeVol(kibotos.VolumeStatusCreating, 50))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes/vol_100/wait":
			writeJSON(w, http.StatusOK, makeVol(kibotos.VolumeStatusAvailable, 100))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes":
			if got := r.URL.Query().Get("attached"); got != "false" {
				t.Errorf("expected attached=false, got %q", got)
			}
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Volume]{
				Data:             []kibotos.Volume{makeVol(kibotos.VolumeStatusAvailable, 50)},
				UnavailableZones: []string{},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/volumes/vol_100":
			if volDelPolls.Load() > 0 {
				writeAPIError(w, http.StatusNotFound, "volume_not_found", "gone", false)
				return
			}
			writeJSON(w, http.StatusOK, makeVol(kibotos.VolumeStatusAvailable, 50))

		case r.Method == http.MethodPatch && r.URL.Path == "/v1/volumes/vol_100":
			writeJSON(w, http.StatusOK, makeVol(kibotos.VolumeStatusAvailable, 50))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/volumes/vol_100/resize":
			writeJSON(w, http.StatusAccepted, makeVol(kibotos.VolumeStatusResizing, 100))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/volumes/vol_100":
			volDelPolls.Store(1)
			writeJSON(w, http.StatusAccepted, makeVol(kibotos.VolumeStatusDeleting, 100))

		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_123/exposures":
			writeJSON(w, http.StatusCreated, makeExp(kibotos.ExposureStatusActive))

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_123/exposures":
			if got := r.URL.Query().Get("include_revoked"); got != "true" {
				t.Errorf("expected include_revoked=true, got %q", got)
			}
			writeJSON(w, http.StatusOK, kibotos.Page[kibotos.Exposure]{
				Data: []kibotos.Exposure{makeExp(kibotos.ExposureStatusActive)},
			})

		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_123/exposures/exp_100":
			writeJSON(w, http.StatusOK, makeExp(kibotos.ExposureStatusActive))

		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_123/exposures/exp_100":
			writeJSON(w, http.StatusOK, makeExp(kibotos.ExposureStatusRevoked))

		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_KEY"] = "kb_test_key_5678"
	h.env["KIBOTOS_API_URL"] = srv.URL

	// Zone list (table with COUNTRY column and JSON with country field)
	if code, out, errOut := h.run("zone", "list"); code != ExitOK || !strings.Contains(out, "COUNTRY") || !strings.Contains(out, "JP") || !strings.Contains(out, "global-1") || !strings.Contains(out, "eu-1") {
		t.Fatalf("zone list failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("zone", "list", "--json"); code != ExitOK || !strings.Contains(out, `"country": "JP"`) || !strings.Contains(out, `"country": null`) {
		t.Fatalf("zone list --json failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// Templates
	if code, out, errOut := h.run("template", "list", "--owner", "platform", "--zone", "global-1"); code != ExitOK || !strings.Contains(out, "python-3.12") {
		t.Fatalf("template list failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("template", "get", "python-3.12"); code != ExitOK || !strings.Contains(out, "Python 3.12") {
		t.Fatalf("template get failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("template", "revisions", "python-3.12"); code != ExitOK || !strings.Contains(out, "r42") {
		t.Fatalf("template revisions failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("template", "revision", "python-3.12", "r42"); code != ExitOK || !strings.Contains(out, "2048 MiB") {
		t.Fatalf("template revision failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// Volumes (using alias `vol`)
	if code, out, errOut := h.run("vol", "create", "--size", "50", "--name", "datasets", "--zone", "global-1", "--label", "tier=fast", "--wait", "--json"); code != ExitOK || !strings.Contains(out, `"status": "available"`) {
		t.Fatalf("vol create failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("vol", "list", "--attached", "false", "--zone", "global-1"); code != ExitOK || !strings.Contains(out, "vol_100") {
		t.Fatalf("vol list failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, _, errOut := h.run("vol", "get", "vol_100"); code != ExitOK {
		t.Fatalf("vol get failed: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := h.run("vol", "update", "vol_100", "--name", "renamed", "--label", "k=v"); code != ExitOK {
		t.Fatalf("vol update failed: code=%d err=%q", code, errOut)
	}
	if code, out, errOut := h.run("vol", "resize", "vol_100", "--size", "100", "--wait", "--json"); code != ExitOK || !strings.Contains(out, `"size_gib": 100`) {
		t.Fatalf("vol resize failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, _, errOut := h.run("vol", "wait", "vol_100", "--status", "available,failed", "--timeout", "10"); code != ExitOK {
		t.Fatalf("vol wait failed: code=%d err=%q", code, errOut)
	}
	if code, out, errOut := h.run("vol", "delete", "vol_100", "--wait", "--json"); code != ExitOK || !strings.Contains(out, `"status": "deleted"`) {
		t.Fatalf("vol delete failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// Exposures (using alias `exp`)
	if code, out, errOut := h.run("exp", "create", "wks_123", "--port", "8080", "--ttl", "1800", "--json"); code != ExitOK || !strings.Contains(out, "https://abc123.kibo.run") {
		t.Fatalf("exp create failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, out, errOut := h.run("exp", "list", "wks_123", "--include-revoked"); code != ExitOK || !strings.Contains(out, "exp_100") {
		t.Fatalf("exp list failed: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, _, errOut := h.run("exp", "get", "wks_123", "exp_100"); code != ExitOK {
		t.Fatalf("exp get failed: code=%d err=%q", code, errOut)
	}
	if code, out, errOut := h.run("exp", "revoke", "wks_123", "exp_100", "--json"); code != ExitOK || !strings.Contains(out, `"status": "revoked"`) {
		t.Fatalf("exp revoke failed: code=%d out=%q err=%q", code, out, errOut)
	}

	// Version
	if code, out, errOut := h.run("version", "--json"); code != ExitOK || !strings.Contains(out, `"version": "0.1.0-test"`) {
		t.Fatalf("version --json failed: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestExitCodes(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/workspaces/wks_conflict":
			writeAPIError(w, http.StatusConflict, "invalid_state", "workspace is stopped", false)
		case "/v1/workspaces/wks_unauth":
			writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "expired key", false)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_URL"] = srv.URL

	// Exit 3 when not logged in.
	if code, _, _ := h.run("zone", "list"); code != ExitUnauthenticated {
		t.Fatalf("no credentials exit = %d, want %d", code, ExitUnauthenticated)
	}

	// Set credentials for remaining checks.
	h.env["KIBOTOS_API_KEY"] = "kb_test_exit_codes_1234"

	// Exit 3 on HTTP 401 from API.
	if code, _, _ := h.run("workspace", "get", "wks_unauth"); code != ExitUnauthenticated {
		t.Fatalf("401 response exit = %d, want %d", code, ExitUnauthenticated)
	}

	// Exit 1 on HTTP 409 API error.
	if code, stdout, stderr := h.run("workspace", "get", "wks_conflict", "-o", "json"); code != ExitError || stdout != "" || !strings.Contains(stderr, `"code": "invalid_state"`) {
		t.Fatalf("409 response exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// Exit 2 on usage / validation errors.
	usageCases := [][]string{
		{"nonexistent-cmd"},
		{"workspace", "create"},
		{"workspace", "get"},
		{"workspace", "create", "--template", "t", "--network", "bad_mode"},
		{"workspace", "update", "wks_1", "--name", "a", "--clear-name"},
		{"workspace", "update", "wks_1"},
		{"volume", "list", "--attached", "maybe"},
		{"volume", "create", "--size", "0"},
		{"exposure", "create", "wks_1", "--port", "70000"},
		{"zone", "list", "-o", "yaml"},
		{"zone", "list", "--unknown-flag"},
	}
	for _, args := range usageCases {
		if code, _, stderr := h.run(args...); code != ExitUsage {
			t.Errorf("args %v exit = %d, want %d (stderr=%s)", args, code, ExitUsage, stderr)
		}
	}
}
