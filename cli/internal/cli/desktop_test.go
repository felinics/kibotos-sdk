package cli

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/felinics/kibotos-sdk/cli/internal/config"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

const (
	desktopTestURL = "https://dash.kibo.to/desktop/wks_test"
	desktopCatalog = `{"data":[{"id":"kibotos-desktop-env","provided_capabilities":["desktop.v1"]}]}`
	desktopBinding = `,"packs":[{"id":"kibotos-desktop-env","provided_capabilities":["desktop.v1"]}]}`
	desktopReady   = `{"id":"wks_test","name":"dev","status":"available","zone":"global-1"` + desktopBinding
	desktopStopped = `{"id":"wks_test","name":"dev","status":"stopped","zone":"global-1","packs":[{"id":"node-22"}]}`
)

func TestDesktopWorkspaceCreate(t *testing.T) {
	const creating = `{"id":"wks_test","status":"creating"}`
	for _, tc := range []struct {
		name, request string
		args          []string
		browserError  error
	}{
		{"human with browser warning", `{"template":{"id":"debian"},"zone":"global-1","packs":["node-22","kibotos-desktop-env"]}`,
			[]string{"--pack", "node-22", "--open", "--console-url", "https://dash.example.test/", "--org", "org_create"}, errors.New("browser unavailable")},
		{"json with explicit Pack", `{"template":{"id":"debian"},"zone":"global-1","packs":["kibotos-desktop-env"]}`,
			[]string{"--pack", "kibotos-desktop-env", "--json"}, nil},
		{"quiet", `{"template":{"id":"debian"},"zone":"global-1","packs":["kibotos-desktop-env"]}`,
			[]string{"-q"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"GET", "/v1/packs?zone=global-1", "", `{"data":[{"id":"alternate","provided_capabilities":["desktop.v1"]}],"next_page_token":"next"}`},
				expectedRequest{"GET", "/v1/packs?page_token=next&zone=global-1", "", desktopCatalog},
				expectedRequest{"POST", workspaceListPath, tc.request, creating},
				expectedRequest{"GET", wave3Workspace + "/wait?status=available&timeout=60", "", availableWorkspace},
			)
			h.openBrowserErr = tc.browserError
			args := append([]string{"ws", "create", "-t", "debian", "-z", " global-1 ", "--desktop"}, tc.args...)
			code, stdout, stderr := h.run(args...)
			if code != ExitOK {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			switch {
			case slices.Contains(tc.args, "--json"):
				var got map[string]any
				if err := json.Unmarshal([]byte(stdout), &got); err != nil || got["id"] != "wks_test" || got["status"] != "available" {
					t.Fatalf("workspace JSON = %q, error = %v", stdout, err)
				}
				rejectText(t, stdout, "desktop_url", "desktop_enabled", "Desktop URL:")
			case slices.Contains(tc.args, "-q"):
				if stdout != "wks_test\n" || stderr != "" {
					t.Fatalf("quiet output = %q, stderr = %q", stdout, stderr)
				}
			default:
				const url = "https://dash.example.test/desktop/wks_test?org=org_create"
				requireText(t, stdout, "Desktop URL: "+url+"\n")
				requireText(t, stderr, "Warning:", "browser unavailable")
				if !slices.Equal(h.openedURLs, []string{url}) {
					t.Fatalf("opened URLs = %v, want [%s]", h.openedURLs, url)
				}
			}
		})
	}

	t.Run("no wait does not open a creating workspace", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", "/v1/packs", "", `{"data":[{"id":"custom-desktop","provided_capabilities":["desktop.v1"]},{"id":"other","provided_capabilities":["desktop.v1"]}]}`},
			expectedRequest{"POST", workspaceListPath, `{"template":{"id":"debian"},"packs":["custom-desktop"]}`, creating},
		)
		requireText(t, expectSuccess(t, h, "ws", "create", "-t", "debian", "--desktop", "--open", "--no-wait"), "Desktop URL: "+desktopTestURL)
		if len(h.openedURLs) != 0 {
			t.Errorf("opened URLs = %v", h.openedURLs)
		}
	})
}

func TestDesktopLocalValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		hint string
	}{
		{"open requires desktop", []string{"ws", "create", "-t", "debian", "--open"}, "--open requires --desktop", ""},
		{"empty console URL", []string{"ws", "create", "-t", "debian", "--console-url="}, "--console-url", ""},
		{"invalid console URL", []string{"desktop", "wks_test", "--console-url", "ftp://dash.example.test"}, "--console-url", ""},
		{"missing workspace", []string{"desktop"}, "workspace ID or name is required", "Pass a workspace ID or name. Run `kibo workspace list --json` to see available workspaces."},
		{"blank workspace", []string{"ws", "desktop", " "}, "workspace ID or name is required", "Pass a workspace ID or name."},
		{"conflicting wait flags", []string{"desktop", "wks_test", "--wait", "--no-wait"}, "--wait and --no-wait cannot be used together", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t)
			delete(h.env, "KIBOTOS_API_KEY")
			code, stdout, stderr := h.run(append(tc.args, "--no-input", "--json")...)
			var got jsonErrorEnvelope
			if err := json.Unmarshal([]byte(stderr), &got); code != ExitUsage || stdout != "" || err != nil ||
				got.Error.Code != "invalid_usage" || !strings.Contains(got.Error.Message, tc.want) ||
				!strings.Contains(got.Error.Hint, tc.hint) {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
			}
		})
	}
}

func TestDesktopPackErrors(t *testing.T) {
	t.Run("requires advertised capability", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", "/v1/packs?zone=global-1", "", `{"data":[{"id":"kibotos-desktop-env"}]}`},
		)
		code, stdout, stderr := h.run("ws", "create", "-t", "debian", "--desktop", "--zone", "global-1", "--json")
		var got jsonErrorEnvelope
		if err := json.Unmarshal([]byte(stderr), &got); code != ExitError || stdout != "" || err != nil || got.Error.Code != "desktop_pack_unavailable" {
			t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
		requireText(t, got.Error.Message, "desktop.v1", "global-1")
		requireText(t, got.Error.Hint, "Run `kibo pack list` to inspect available Packs.")
	})
	t.Run("creation Pack limit", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", "/v1/packs", "", desktopCatalog})
		_, stderr := guidedResult(t, h, "", ExitUsage, "ws", "create", "-t", "debian", "--desktop", "--pack", "a", "--pack", "b", "--pack", "c", "--pack", "d")
		requireText(t, stderr, "at most four Packs")
	})
	t.Run("existing Pack limit", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","status":"available","zone":"global-1","packs":[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"}]}`},
			expectedRequest{"GET", "/v1/packs?zone=global-1", "", desktopCatalog},
		)
		_, stderr := guidedResult(t, h, "", ExitUsage, "desktop", "wks_test")
		requireText(t, stderr, "at most four Packs")
		rejectText(t, stderr, "Restart workspace now")
	})
}

func TestDesktopPickerAlreadyEnabled(t *testing.T) {
	const legacy = `{"id":"wks_test","name":"dev","status":"available","zone":"global-1","packs":[{"id":"kibotos-desktop-env"}]}`
	h := wave3Harness(t,
		expectedRequest{"GET", workspaceListPath, "", `{"data":[` + stoppedWorkspaceJSON + `,` + legacy + `]}`},
	)
	stdout, stderr := guidedResult(t, h, "2\n", ExitOK, "desktop", "--open")
	requireText(t, stderr, "Select a workspace for desktop:", "offline (wks_other)", "dev (wks_test)")
	rejectText(t, stderr, "Start workspace now", "Restart workspace now")
	requireText(t, stdout, "Desktop Pack:", defaultDesktopPackID, "Desktop URL:", desktopTestURL)
	if !slices.Equal(h.openedURLs, []string{desktopTestURL}) {
		t.Fatalf("opened URLs = %v", h.openedURLs)
	}
}

func TestDesktopStartStoppedWorkspace(t *testing.T) {
	const stoppedWithPack = `{"id":"wks_test","status":"stopped"` + desktopBinding
	for _, tc := range []struct {
		name, input          string
		args                 []string
		prompt, attach, boot bool
		exit                 int
	}{
		{"accept", "\n", nil, true, true, true, ExitOK},
		{"decline start", "n\n", nil, true, true, false, ExitOK},
		{"cancel", "", nil, true, false, false, ExitError},
		{"yes", "", []string{"--yes"}, false, true, true, ExitOK},
		{"explicit start", "", []string{"--start", "--no-input"}, false, true, true, ExitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := []expectedRequest{
				{"GET", wave3Workspace, "", desktopStopped},
				{"GET", "/v1/packs?zone=global-1", "", desktopCatalog},
			}
			if tc.attach {
				requests = append(requests, expectedRequest{"PUT", wave3Workspace + "/packs", `{"packs":["node-22","kibotos-desktop-env"]}`, stoppedWithPack})
			}
			if tc.boot {
				requests = append(requests,
					expectedRequest{"POST", wave3Workspace + "/start", "", `{"id":"wks_test","status":"starting"}`},
					expectedRequest{"GET", wave3Workspace + "/wait?status=available&timeout=60", "", desktopReady},
				)
			}
			h := wave3Harness(t, requests...)
			stdout, stderr := guidedResult(t, h, tc.input, tc.exit, append([]string{"desktop", "wks_test", "--open"}, tc.args...)...)
			promptCount := strings.Count(stderr, "Start workspace now to open desktop? [Y/n]")
			if (tc.prompt && promptCount != 1) || (!tc.prompt && promptCount != 0) {
				t.Errorf("unexpected prompt output %q", stderr)
			}
			if tc.attach {
				requireText(t, stdout, desktopTestURL)
			} else if stdout != "" {
				t.Errorf("stdout after cancellation = %q", stdout)
			}
			if tc.boot && !slices.Equal(h.openedURLs, []string{desktopTestURL}) || !tc.boot && len(h.openedURLs) != 0 {
				t.Errorf("opened URLs = %v", h.openedURLs)
			}
		})
	}
}

func TestDesktopRestartAvailableWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		args        []string
		restart     bool
		noWait      bool
		prompt      bool
	}{
		{"explicit restart", "", []string{"--restart", "--json"}, true, false, false},
		{"restart without waiting for start", "", []string{"--restart", "--no-wait", "--wait-timeout", "15s", "--json"}, true, true, false},
		{"non-interactive pending", "", []string{"--json"}, false, false, false},
		{"guided restart", "y\n", nil, true, false, true},
		{"guided decline", "n\n", nil, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := []expectedRequest{
				{"GET", wave3Workspace, "", availableWorkspace},
				{"GET", "/v1/packs?zone=global-1", "", desktopCatalog},
				{"PUT", wave3Workspace + "/packs", `{"packs":["kibotos-desktop-env"]}`, desktopReady},
			}
			status := kibotos.WorkspaceStatusAvailable
			if tc.restart {
				timeout := "60"
				if tc.noWait {
					timeout = "15"
				}
				requests = append(requests,
					expectedRequest{"POST", wave3Workspace + "/stop", "", `{"id":"wks_test","status":"stopping"}`},
					expectedRequest{"GET", wave3Workspace + "/wait?status=stopped&timeout=" + timeout, "", `{"id":"wks_test","status":"stopped"}`},
					expectedRequest{"POST", wave3Workspace + "/start", "", `{"id":"wks_test","status":"starting"}`},
				)
				if tc.noWait {
					status = kibotos.WorkspaceStatusStarting
				} else {
					requests = append(requests, expectedRequest{"GET", wave3Workspace + "/wait?status=available&timeout=60", "", desktopReady})
				}
			}
			h := wave3Harness(t, requests...)
			stdout, stderr := guidedResult(t, h, tc.input, ExitOK, append([]string{"ws", "desktop", "wks_test", "--open"}, tc.args...)...)
			if slices.Contains(tc.args, "--json") {
				var got desktopResult
				want := desktopResult{"wks_test", status, true, defaultDesktopPackID, !tc.restart, desktopTestURL}
				if err := json.Unmarshal([]byte(stdout), &got); err != nil || got != want {
					t.Fatalf("desktop JSON = %q, want %+v, error = %v", stdout, want, err)
				}
				requireText(t, stdout, `"workspace_id"`, `"status"`, `"desktop_enabled"`, `"pack_id"`, `"restart_required"`, `"url"`)
			} else {
				requireText(t, stdout, "Desktop URL:", desktopTestURL)
				if !tc.restart {
					requireText(t, stderr, "Desktop Pack attached; restart the workspace (wks_test) for it to take effect.")
				}
			}
			promptCount := strings.Count(stderr, "Restart workspace now to activate desktop? [Y/n]")
			if (tc.prompt && promptCount != 1) || (!tc.prompt && promptCount != 0) {
				t.Errorf("unexpected prompt output %q", stderr)
			}
			if tc.restart && !tc.noWait {
				if !slices.Equal(h.openedURLs, []string{desktopTestURL}) {
					t.Errorf("opened URLs = %v", h.openedURLs)
				}
			} else if len(h.openedURLs) != 0 {
				t.Errorf("opened URLs = %v", h.openedURLs)
			}
		})
	}
}

func TestDesktopQuietURLConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, env, base, org string
		args                 []string
	}{
		{"profile", "", "https://profile.example.test", "org_saved", nil},
		{"organization override", "KIBOTOS_CONSOLE_URL", "https://env.example.test", "org%2Fa+%26", []string{"--org", "org/a &"}},
		{"short console env", "KIBO_CONSOLE_URL", "https://env.example.test", "org_saved", nil},
		{"console override", "KIBOTOS_CONSOLE_URL", "http://override.example.test/dash", "org_saved", []string{"--console-url", "http://override.example.test/dash///?from=console#home"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", desktopReady})
			if tc.env != "" {
				h.env[tc.env] = "https://env.example.test/"
			}
			if err := h.store.Save(&config.Config{CurrentProfile: "desktop", Profiles: map[string]*config.Profile{
				"desktop": {APIKey: h.env["KIBOTOS_API_KEY"], ConsoleURL: "https://profile.example.test/", OrganizationID: "org_saved"},
			}}); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := h.run(append([]string{"desktop", "wks_test", "--no-input", "-q"}, tc.args...)...)
			if want := tc.base + "/desktop/wks_test?org=" + tc.org + "\n"; code != ExitOK || stdout != want || stderr != "" {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, want stdout = %q", code, stdout, stderr, want)
			}
		})
	}
}

func TestDesktopNameResolution(t *testing.T) {
	const exact = `{"id":"wks_exact","name":"Dev","status":"available","packs":[{"id":"custom-desktop","provided_capabilities":["desktop.v1"]}]}`
	first := expectedRequest{"GET", workspaceListPath, "", `{"data":[` + desktopReady + `],"next_page_token":"next"}`}
	second := expectedRequest{"GET", workspaceListPath + "?page_token=next", "", `{"data":[` + exact + `]}`}
	h := wave3Harness(t, first, second, first, second)
	requireText(t, expectSuccess(t, h, "desktop", "Dev", "--json"), `"workspace_id": "wks_exact"`, `"pack_id": "custom-desktop"`)
	code, stdout, stderr := h.run("desktop", "DEV", "--no-input", "--json")
	if code != ExitUsage || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	requireText(t, stderr, "ambiguous_name", "wks_test", "wks_exact")

	h = wave3Harness(t, expectedRequest{"GET", workspaceListPath, "", `{"data":[` + exact + `]}`})
	if stdout := expectSuccess(t, h, "desktop", "DEV", "-q"); stdout != "https://dash.kibo.to/desktop/wks_exact\n" {
		t.Fatalf("folded name URL = %q", stdout)
	}
}
