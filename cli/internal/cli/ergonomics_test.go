package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

func (h *testHarness) runInteractive(stdin string, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	yes := true
	noColor := false
	code := Execute(context.Background(), args, Options{
		Stdin:                strings.NewReader(stdin),
		Stdout:               &stdout,
		Stderr:               &stderr,
		Store:                h.store,
		EnvLookup:            func(k string) string { return h.env[k] },
		HTTPClient:           h.httpClient,
		PollIntervalOverride: 5 * time.Millisecond,
		InteractiveOverride:  &yes,
		ColorOverride:        &noColor,
		OpenBrowser: func(u string) error {
			h.openedURLs = append(h.openedURLs, u)
			return h.openBrowserErr
		},
		Build: BuildInfo{Version: "0.2.0-test", Commit: "abc1234"},
	})
	return code, stdout.String(), stderr.String()
}

func TestUnitParsers(t *testing.T) {
	t.Parallel()

	durCases := []struct {
		in   string
		want int
	}{
		{"3600", 3600},
		{"30s", 30},
		{"30m", 1800},
		{"1h", 3600},
		{"1h30m", 5400},
		{"2d", 172800},
		{"1d12h", 129600},
	}
	for _, tc := range durCases {
		got, err := parseDurationSeconds(tc.in, "timeout")
		if err != nil || got != tc.want {
			t.Errorf("parseDurationSeconds(%q) = (%d, %v), want (%d, nil)", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "0", "-5", "0s", "500ms", "abc", "-1h"} {
		if _, err := parseDurationSeconds(bad, "timeout"); err == nil {
			t.Errorf("parseDurationSeconds(%q) expected error, got nil", bad)
		}
	}

	mibCases := []struct {
		in   string
		want int
	}{
		{"4096", 4096},
		{"512Mi", 512},
		{"4Gi", 4096},
		{"4G", 4096},
		{"1.5Gi", 1536},
		{"20GB", 20480},
		{"1Ti", 1048576},
	}
	for _, tc := range mibCases {
		got, err := parseSizeMiB(tc.in, "memory")
		if err != nil || got != tc.want {
			t.Errorf("parseSizeMiB(%q) = (%d, %v), want (%d, nil)", tc.in, got, err, tc.want)
		}
	}

	gibCases := []struct {
		in   string
		want int
	}{
		{"50", 50},
		{"50Gi", 50},
		{"50G", 50},
		{"1Ti", 1024},
	}
	for _, tc := range gibCases {
		got, err := parseSizeGiB(tc.in, "size")
		if err != nil || got != tc.want {
			t.Errorf("parseSizeGiB(%q) = (%d, %v), want (%d, nil)", tc.in, got, err, tc.want)
		}
	}

	tpl, rev, err := parseTemplateSpec("python-3.12@r42", "")
	if err != nil || tpl != "python-3.12" || rev != "r42" {
		t.Fatalf("parseTemplateSpec inline = (%q, %q, %v)", tpl, rev, err)
	}
	if _, _, err := parseTemplateSpec("python-3.12@r42", "r99"); err == nil {
		t.Fatalf("expected conflict error when inline revision differs from --revision")
	}
}

func TestDefaultWaitAndNoWaitBehavior(t *testing.T) {
	t.Parallel()

	var waitHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces":
			var body kibotos.CreateWorkspaceParams
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Template.ID != "python-3.12" || body.Template.Revision != "r42" {
				t.Errorf("unexpected template: %+v", body.Template)
			}
			if body.Resources == nil || body.Resources.MemoryMiB != 4096 || body.Resources.DiskMiB != 20480 {
				t.Errorf("unexpected resources: %+v", body.Resources)
			}
			if body.Timeout == nil || body.Timeout.Seconds != 5400 {
				t.Errorf("unexpected timeout: %+v", body.Timeout)
			}
			writeJSON(w, http.StatusAccepted, kibotos.Workspace{
				ID:       "wks_wait_1",
				Name:     body.Name,
				Zone:     "global-1",
				Template: kibotos.WorkspaceTemplate{ID: body.Template.ID, Revision: body.Template.Revision},
				Status:   kibotos.WorkspaceStatusCreating,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_wait_1/wait":
			waitHits.Add(1)
			writeJSON(w, http.StatusOK, kibotos.Workspace{
				ID:       "wks_wait_1",
				Name:     "fast-box",
				Zone:     "global-1",
				Template: kibotos.WorkspaceTemplate{ID: "python-3.12", Revision: "r42"},
				Status:   kibotos.WorkspaceStatusAvailable,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_KEY"] = "kb_test_key_1234"
	h.env["KIBOTOS_API_URL"] = srv.URL

	// 1. Default behavior: waits automatically WITHOUT --wait, supports human-friendly units & @rev!
	code, stdout, stderr := h.run(
		"ws", "create",
		"-t", "python-3.12@r42",
		"-n", "fast-box",
		"--memory", "4Gi",
		"--disk", "20Gi",
		"--timeout", "1h30m",
		"--json",
	)
	if code != ExitOK {
		t.Fatalf("default wait create failed: code=%d stderr=%s", code, stderr)
	}
	if waitHits.Load() != 1 {
		t.Fatalf("expected 1 wait call by default, got %d", waitHits.Load())
	}
	var ws kibotos.Workspace
	if err := json.Unmarshal([]byte(stdout), &ws); err != nil || ws.Status != kibotos.WorkspaceStatusAvailable {
		t.Fatalf("expected available workspace JSON, got err=%v stdout=%s", err, stdout)
	}

	// 2. --no-wait skips waiting and returns creating immediately.
	code, stdout, stderr = h.run(
		"ws", "create",
		"-t", "python-3.12@r42",
		"-n", "fast-box",
		"--memory", "4Gi",
		"--disk", "20Gi",
		"--timeout", "1h30m",
		"--no-wait",
		"--json",
	)
	if code != ExitOK {
		t.Fatalf("--no-wait create failed: code=%d stderr=%s", code, stderr)
	}
	if waitHits.Load() != 1 {
		t.Fatalf("expected no additional wait call with --no-wait, got %d", waitHits.Load())
	}
	if err := json.Unmarshal([]byte(stdout), &ws); err != nil || ws.Status != kibotos.WorkspaceStatusCreating {
		t.Fatalf("expected creating status with --no-wait, got %q", ws.Status)
	}

	// 3. --wait=false also skips waiting.
	code, stdout, _ = h.run(
		"ws", "create",
		"-t", "python-3.12@r42",
		"--memory", "4Gi",
		"--disk", "20Gi",
		"--timeout", "1h30m",
		"--wait=false",
		"--json",
	)
	if code != ExitOK || !strings.Contains(stdout, `"status": "creating"`) {
		t.Fatalf("--wait=false failed: code=%d stdout=%s", code, stdout)
	}

	// 4. --wait and --no-wait together is a usage error (exit 2).
	if code, _, _ := h.run("ws", "create", "-t", "python-3.12", "--wait", "--no-wait"); code != ExitUsage {
		t.Fatalf("expected exit 2 when combining --wait and --no-wait, got %d", code)
	}

	// 5. -q / --quiet outputs only the workspace ID.
	code, stdout, stderr = h.run(
		"ws", "create",
		"-t", "python-3.12@r42",
		"--memory", "4Gi",
		"--disk", "20Gi",
		"--timeout", "1h30m",
		"--no-wait",
		"-q",
	)
	if code != ExitOK || strings.TrimSpace(stdout) != "wks_wait_1" || stderr != "" {
		t.Fatalf("-q create failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestInteractiveGuidedFlowsAndConfirmations(t *testing.T) {
	t.Parallel()

	var deleteCalls atomic.Int32
	var lastCreate kibotos.CreateWorkspaceParams
	var lastExposure kibotos.CreateExposureParams

	ws1 := kibotos.Workspace{
		ID:       "wks_alpha",
		Name:     "alpha",
		Zone:     "global-1",
		Template: kibotos.WorkspaceTemplate{ID: "python-3.12"},
		Status:   kibotos.WorkspaceStatusAvailable,
		Network:  kibotos.WorkspaceNetwork{Mode: kibotos.NetworkModePublicInternet, Applied: true},
	}
	ws2 := kibotos.Workspace{
		ID:       "wks_beta",
		Name:     "beta",
		Zone:     "global-1",
		Template: kibotos.WorkspaceTemplate{ID: "node-22"},
		Status:   kibotos.WorkspaceStatusStopped,
		Network:  kibotos.WorkspaceNetwork{Mode: kibotos.NetworkModePublicInternet, Applied: true},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates":
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Template]{
				Data: []kibotos.Template{
					{ID: "python-3.12", DisplayName: "Python 3.12", Zones: []string{"global-1", "eu-1"}},
					{ID: "node-22", DisplayName: "Node 22", Zones: []string{"global-1"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces":
			_ = json.NewDecoder(r.Body).Decode(&lastCreate)
			writeJSON(w, http.StatusAccepted, kibotos.Workspace{
				ID:       "wks_new",
				Name:     lastCreate.Name,
				Zone:     "eu-1",
				Template: kibotos.WorkspaceTemplate{ID: lastCreate.Template.ID},
				Status:   kibotos.WorkspaceStatusAvailable,
				Network:  kibotos.WorkspaceNetwork{Mode: kibotos.NetworkModeDenyAll, Applied: true},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces":
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Workspace]{
				Data: []kibotos.Workspace{ws1, ws2},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/workspaces/wks_alpha":
			deleteCalls.Add(1)
			writeJSON(w, http.StatusAccepted, kibotos.Workspace{ID: "wks_alpha", Status: kibotos.WorkspaceStatusDeleting})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_alpha":
			if deleteCalls.Load() > 0 {
				writeAPIError(w, http.StatusNotFound, "workspace_not_found", "gone", false)
				return
			}
			writeJSON(w, http.StatusOK, ws1)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_alpha/exposures":
			_ = json.NewDecoder(r.Body).Decode(&lastExposure)
			writeJSON(w, http.StatusCreated, kibotos.Exposure{
				ID:          "exp_99",
				WorkspaceID: "wks_alpha",
				Port:        lastExposure.Port,
				URL:         "https://demo.kibo.run",
				Status:      kibotos.ExposureStatusActive,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_KEY"] = "kb_test_interactive_1234"
	h.env["KIBOTOS_API_URL"] = srv.URL

	// 1. Minimal interactive `kibo ws create`: only asks template + optional name (no verbose advanced prompts).
	code, stdout, stderr := h.runInteractive("1\nquickbox\n", "ws", "create")
	if code != ExitOK {
		t.Fatalf("interactive ws create failed: code=%d stderr=%s", code, stderr)
	}
	if lastCreate.Template.ID != "python-3.12" || lastCreate.Name != "quickbox" {
		t.Fatalf("unexpected create params: %+v", lastCreate)
	}
	if !strings.Contains(stdout, "wks_new") || !strings.Contains(stderr, "Hint: Expose a port") {
		t.Fatalf("expected details and hint, stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Contains(stderr, "Customize") || strings.Contains(stderr, "Outbound network") || strings.Contains(stderr, "Timeout action") {
		t.Fatalf("interactive ws create should stay minimal without advanced prompts, got stderr=%q", stderr)
	}

	// 2. Advanced options remain available directly via flags (with human-friendly units).
	code, _, stderr = h.run(
		"ws", "create",
		"-t", "python-3.12",
		"-n", "custom",
		"-z", "eu-1",
		"--vcpus", "4",
		"--memory", "8Gi",
		"--disk", "40Gi",
		"--network", "deny_all",
		"--timeout", "2h",
		"--timeout-action", "delete",
	)
	if code != ExitOK {
		t.Fatalf("flag-driven custom ws create failed: code=%d stderr=%s", code, stderr)
	}
	if lastCreate.Zone != "eu-1" ||
		lastCreate.Resources == nil || lastCreate.Resources.VCPUs != 4 || lastCreate.Resources.MemoryMiB != 8192 || lastCreate.Resources.DiskMiB != 40960 ||
		lastCreate.Network == nil || lastCreate.Network.Mode != kibotos.NetworkModeDenyAll ||
		lastCreate.Timeout == nil || lastCreate.Timeout.Seconds != 7200 || lastCreate.Timeout.Action != kibotos.TimeoutActionDelete {
		t.Fatalf("unexpected custom create params: %+v", lastCreate)
	}

	// 3. Interactive `kibo ws delete` declined -> must NOT call DELETE API.
	code, _, stderr = h.runInteractive("1\nn\n", "ws", "delete")
	if code == ExitOK {
		t.Fatalf("expected cancelled exit code when declining delete, stderr=%s", stderr)
	}
	if deleteCalls.Load() != 0 {
		t.Fatalf("expected 0 delete calls when user declined, got %d", deleteCalls.Load())
	}

	// 4. Interactive `kibo ws delete` confirmed -> calls DELETE API and waits.
	code, stdout, stderr = h.runInteractive("1\ny\n", "ws", "delete")
	if code != ExitOK || deleteCalls.Load() != 1 || !strings.Contains(stdout, "deleted") {
		t.Fatalf("interactive delete confirm failed: code=%d calls=%d stdout=%q stderr=%q", code, deleteCalls.Load(), stdout, stderr)
	}

	// 5. Interactive `kibo exp create` with no args picks available workspace and prompts port.
	code, stdout, stderr = h.runInteractive("1\n3000\n", "exp", "create")
	if code != ExitOK || lastExposure.Port != 3000 || !strings.Contains(stdout, "https://demo.kibo.run") {
		t.Fatalf("interactive exp create failed: code=%d port=%d stdout=%q stderr=%q", code, lastExposure.Port, stdout, stderr)
	}

	// 6. --no-input overrides interactive TTY and fails fast with actionable JSON hint.
	code, stdout, stderr = h.runInteractive("", "ws", "create", "--no-input", "--json")
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, `"missing_flags"`) || !strings.Contains(stderr, "kibo template list --json") {
		t.Fatalf("--no-input missing flag error unexpected: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestNameResolutionAndAgentEnvVars(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces":
			writeJSON(w, http.StatusOK, kibotos.MultiZonePage[kibotos.Workspace]{
				Data: []kibotos.Workspace{
					{ID: "wks_u1", Name: "unique-box", Zone: "global-1", Status: kibotos.WorkspaceStatusAvailable},
					{ID: "wks_d1", Name: "dup-box", Zone: "global-1", Status: kibotos.WorkspaceStatusAvailable},
					{ID: "wks_d2", Name: "dup-box", Zone: "eu-1", Status: kibotos.WorkspaceStatusStopped},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workspaces/wks_u1":
			writeJSON(w, http.StatusOK, kibotos.Workspace{
				ID: "wks_u1", Name: "unique-box", Zone: "global-1", Status: kibotos.WorkspaceStatusAvailable,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/workspaces/wks_u1/exposures":
			writeJSON(w, http.StatusCreated, kibotos.Exposure{
				ID: "exp_pos", WorkspaceID: "wks_u1", Port: 9090, URL: "https://pos.kibo.run", Status: kibotos.ExposureStatusActive,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := newTestHarness(t, srv.Client())
	h.env["KIBOTOS_API_KEY"] = "kb_test_name_1234"
	h.env["KIBOTOS_API_URL"] = srv.URL
	h.env["KIBO_OUTPUT"] = "json"

	// 1. Unique name resolution + KIBO_OUTPUT=json env var (no --json flag needed).
	code, stdout, stderr := h.run("ws", "get", "unique-box")
	if code != ExitOK || !strings.Contains(stdout, `"id": "wks_u1"`) {
		t.Fatalf("name resolution failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// 2. Positional port + name resolution: `kibo exp create unique-box 9090 --ttl 30m`.
	code, stdout, stderr = h.run("exp", "create", "unique-box", "9090", "--ttl", "30m")
	if code != ExitOK || !strings.Contains(stdout, `"port": 9090`) {
		t.Fatalf("positional exp create failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// 3. Ambiguous name in non-interactive mode returns structured JSON error with candidates.
	code, stdout, stderr = h.run("ws", "get", "dup-box")
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, `"code": "ambiguous_name"`) || !strings.Contains(stderr, "wks_d1") || !strings.Contains(stderr, "wks_d2") {
		t.Fatalf("expected ambiguous_name error with candidates: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestUserSessionDeviceLoginOrgSwitchAndLogout(t *testing.T) {
	t.Parallel()

	var logoutCalls atomic.Int32
	var lastOrgHeader string

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/zones" {
			if r.Header.Get("Authorization") != "Bearer kbs_user_session_secret_9876543210" {
				writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "bad session", false)
				return
			}
			lastOrgHeader = r.Header.Get("X-Kibotos-Organization")
			writeJSON(w, http.StatusOK, map[string]any{
				"data":            []kibotos.Zone{{ID: "global-1", Status: kibotos.ZoneStatusAvailable, Default: true}},
				"next_page_token": nil,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer apiSrv.Close()

	orgs := []map[string]any{
		{"id": "org_alpha", "display_name": "Alpha Org", "region": "global", "role": "owner", "status": "active"},
		{"id": "org_beta", "display_name": "Beta Org", "region": "global", "role": "member", "status": "active"},
	}

	consoleSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/device/start":
			writeJSON(w, http.StatusCreated, map[string]any{
				"device_code":               "dev_sess_code_1",
				"user_code":                 "ABCD-EFGH",
				"verification_url":          "https://dash.kibo.to/device",
				"verification_url_complete": "https://dash.kibo.to/device?user_code=ABCD-EFGH",
				"expires_in":                600,
				"interval":                  1,
			})
		case "/api/auth/device/token":
			writeJSON(w, http.StatusOK, map[string]any{
				"status":            "approved",
				"token_type":        "user_session",
				"access_token":      "kbs_user_session_secret_9876543210",
				"session_id":        "ses_01abc",
				"user_id":           "usr_01abc",
				"user_email":        "dev@acme.com",
				"organization_id":   "org_alpha",
				"organization_name": "Alpha Org",
				"organizations":     orgs,
				"api_url":           apiSrv.URL + "/v1",
			})
		case "/api/organizations":
			if r.Header.Get("Authorization") != "Bearer kbs_user_session_secret_9876543210" {
				writeAPIError(w, http.StatusUnauthorized, "unauthenticated", "bad token", false)
				return
			}
			writeJSON(w, http.StatusOK, orgs)
		case "/api/auth/logout":
			if r.Header.Get("Authorization") == "Bearer kbs_user_session_secret_9876543210" {
				logoutCalls.Add(1)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer consoleSrv.Close()

	h := newTestHarness(t, apiSrv.Client())

	// 1. Device login receives user_session token (kbs_...).
	code, stdout, stderr := h.run("login", "--console-url", consoleSrv.URL, "--json")
	if code != ExitOK {
		t.Fatalf("device login failed: code=%d stderr=%s", code, stderr)
	}
	var loginOut loginResult
	if err := json.Unmarshal([]byte(stdout), &loginOut); err != nil {
		t.Fatalf("decode login json: %v", err)
	}
	if loginOut.AuthType != "user_session" || loginOut.SessionID != "ses_01abc" || loginOut.TokenMasked != "kbs_...3210" {
		t.Fatalf("unexpected login result: %+v", loginOut)
	}

	// 2. Whoami sends Bearer kbs_... + X-Kibotos-Organization: org_alpha.
	code, stdout, stderr = h.run("whoami", "--json")
	if code != ExitOK || lastOrgHeader != "org_alpha" || !strings.Contains(stdout, `"auth_type": "user_session"`) {
		t.Fatalf("whoami failed: code=%d orgHeader=%q stdout=%s stderr=%s", code, lastOrgHeader, stdout, stderr)
	}

	// 3. `kibo org list` and `kibo org use "Beta Org"` switch org without re-logging in.
	code, stdout, stderr = h.run("org", "list", "--json")
	if code != ExitOK || !strings.Contains(stdout, "Beta Org") {
		t.Fatalf("org list failed: code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	code, _, stderr = h.run("org", "use", "Beta Org", "--json")
	if code != ExitOK {
		t.Fatalf("org use failed: code=%d stderr=%s", code, stderr)
	}
	code, _, stderr = h.run("zone", "list", "--json")
	if code != ExitOK || lastOrgHeader != "org_beta" {
		t.Fatalf("zone list after org switch failed: code=%d orgHeader=%q stderr=%s", code, lastOrgHeader, stderr)
	}

	// 4. --org flag overrides active org per invocation.
	code, _, stderr = h.run("zone", "list", "--org", "org_alpha", "--json")
	if code != ExitOK || lastOrgHeader != "org_alpha" {
		t.Fatalf("--org override failed: code=%d orgHeader=%q stderr=%s", code, lastOrgHeader, stderr)
	}

	// 5. Logout revokes server-side session too.
	code, stdout, stderr = h.run("logout", "--json")
	if code != ExitOK || logoutCalls.Load() != 1 || !strings.Contains(stdout, `"session_revoked": true`) {
		t.Fatalf("logout failed: code=%d calls=%d stdout=%s stderr=%s", code, logoutCalls.Load(), stdout, stderr)
	}
}

func TestClientSideInputValidation(t *testing.T) {
	t.Parallel()

	h := newTestHarness(t, http.DefaultClient)
	h.env["KIBOTOS_API_KEY"] = "kb_validation_test_1234"
	h.env["KIBOTOS_API_URL"] = "https://unused.invalid/v1"

	cases := []struct {
		name     string
		args     []string
		wantSub  string
		wantHint string
	}{
		{
			name:     "memory too small bare int suggests Gi",
			args:     []string{"ws", "create", "-t", "debian", "--memory", "8"},
			wantSub:  "at least 128 MiB",
			wantHint: "Did you mean --memory 8Gi?",
		},
		{
			name:     "disk too small bare int suggests Gi",
			args:     []string{"ws", "create", "-t", "debian", "--disk", "20"},
			wantSub:  "at least 128 MiB",
			wantHint: "Did you mean --disk 20Gi?",
		},
		{
			name:    "vcpus out of range",
			args:    []string{"ws", "create", "-t", "debian", "--vcpus", "512"},
			wantSub: "--vcpus must be between 1 and 256",
		},
		{
			name:    "template empty before at-sign",
			args:    []string{"ws", "create", "-t", "@rev1"},
			wantSub: "template ID before '@' cannot be empty",
		},
		{
			name:    "invalid label key uppercase",
			args:    []string{"ws", "create", "-t", "debian", "-l", "Env=prod"},
			wantSub: "invalid label key",
		},
		{
			name:    "invalid label value spaces",
			args:    []string{"ws", "create", "-t", "debian", "-l", "env=hello world"},
			wantSub: "invalid label value",
		},
		{
			name:    "invalid mount path relative",
			args:    []string{"ws", "create", "-t", "debian", "--volume", "vol_1", "--mount-path", "data"},
			wantSub: "must be an absolute path",
		},
		{
			name:    "invalid mount path root",
			args:    []string{"ws", "create", "-t", "debian", "--volume", "vol_1", "--mount-path", "/"},
			wantSub: "cannot mount directly at '/'",
		},
		{
			name:    "workspace list invalid status",
			args:    []string{"ws", "list", "--status", "running"},
			wantSub: "invalid workspace status",
		},
		{
			name:    "workspace list limit over 100",
			args:    []string{"ws", "list", "--limit", "101"},
			wantSub: "--limit must be between 1 and 100",
		},
		{
			name:    "workspace wait invalid status",
			args:    []string{"ws", "wait", "wks_1", "--status", "ready"},
			wantSub: "invalid workspace status",
		},
		{
			name:    "workspace update empty name",
			args:    []string{"ws", "update", "wks_1", "--name", ""},
			wantSub: "use --clear-name",
		},
		{
			name:    "volume wait invalid status",
			args:    []string{"vol", "wait", "vol_1", "--status", "running"},
			wantSub: "invalid volume status",
		},
		{
			name:    "volume create size too large",
			args:    []string{"vol", "create", "--size", "100Ti"},
			wantSub: "exceeds maximum supported size",
		},
		{
			name:    "exposure ttl over 24h",
			args:    []string{"exp", "create", "wks_1", "8080", "--ttl", "48h"},
			wantSub: "--ttl must be at most 24h",
		},
		{
			name:    "zone list limit over 100",
			args:    []string{"zone", "list", "--limit", "200"},
			wantSub: "--limit must be between 1 and 100",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := h.run(tc.args...)
			if code != ExitUsage {
				t.Fatalf("args=%v: exit=%d, want %d (stdout=%q stderr=%q)", tc.args, code, ExitUsage, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.wantSub) {
				t.Fatalf("args=%v: stderr=%q missing %q", tc.args, stderr, tc.wantSub)
			}
			if tc.wantHint != "" && !strings.Contains(stderr, tc.wantHint) {
				t.Fatalf("args=%v: stderr=%q missing hint %q", tc.args, stderr, tc.wantHint)
			}
		})
	}
}
