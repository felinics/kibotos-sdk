package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	shortcutStopped  = `{"id":"wks_test","name":"dev","status":"stopped"}`
	shortcutStopping = `{"id":"wks_test","status":"stopping"}`
	shortcutStarting = `{"id":"wks_test","status":"starting"}`
)

func TestShortcutOfflineOrchestration(t *testing.T) {
	for _, operation := range []struct {
		name, action  string
		args          []string
		mutation      expectedRequest
		readByDefault bool
	}{
		{"resize", "resize", []string{"ws", "resize", "wks_test", "--vcpus", "4"},
			expectedRequest{"PATCH", wave3Workspace, `{"resources":{"vcpus":4}}`, shortcutStopped}, false},
		{"attach", "update volumes", []string{"ws", "volume", "attach", "wks_test", "vol_new", "--mount", "/data"},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[{"id":"vol_new","mount_path":"/data"}]}`, shortcutStopped}, true},
	} {
		t.Run(operation.name, func(t *testing.T) {
			const stopWait = wave3Workspace + "/wait?status=stopped&timeout=4"
			const startWait = wave3Workspace + "/wait?status=available&timeout=4"
			current := expectedRequest{"GET", wave3Workspace, "", availableWorkspace}
			sequence := []expectedRequest{
				current,
				{"POST", wave3Workspace + "/stop", "", shortcutStopping},
				{"GET", stopWait, "", shortcutStopped},
				operation.mutation,
				{"POST", wave3Workspace + "/start", "", shortcutStarting},
				{"GET", startWait, "", availableWorkspace},
			}
			args := append(slices.Clone(operation.args), "--wait-timeout", "4s")
			question := "Workspace dev (wks_test) is running. Stop, " + operation.action + ", and start it again? [Y/n]:"
			t.Run("interactive accept", func(t *testing.T) {
				stdout, stderr := guidedResult(t, wave3Harness(t, sequence...), "\n", ExitOK, args...)
				requireText(t, stdout, "available")
				requireText(t, stderr, question, "Stopping workspace", "Applying changes", "Starting workspace")
				if strings.Count(stderr, "[Y/n]") != 1 {
					t.Fatalf("expected one confirmation, got %q", stderr)
				}
			})
			t.Run("interactive decline", func(t *testing.T) {
				stdout, stderr := guidedResult(t, wave3Harness(t, current), "n\n", ExitError, args...)
				requireText(t, stderr, question, "operation cancelled")
				rejectText(t, stderr, "Stopping workspace")
				if stdout != "" {
					t.Fatalf("stdout = %q", stdout)
				}
			})
			t.Run("explicit restart in JSON mode", func(t *testing.T) {
				h := wave3Harness(t, sequence...)
				stdout, stderr := guidedResult(t, h, "", ExitOK, append(slices.Clone(args), "--restart", "--json")...)
				requireText(t, stdout, `"status": "available"`)
				rejectText(t, stderr, "[Y/n]")
			})
			for _, flag := range []string{"--no-input", "--json", "-q", "CI"} {
				t.Run("running default "+flag, func(t *testing.T) {
					var requests []string
					h := newTestHarness(t, &http.Client{Transport: cliRoundTripFunc(func(r *http.Request) (*http.Response, error) {
						requests = append(requests, r.Method+" "+r.URL.Path)
						w := httptest.NewRecorder()
						switch {
						case operation.readByDefault && r.Method == "GET" && r.URL.Path == wave3Workspace:
							_, _ = w.WriteString(availableWorkspace)
						case r.Method == operation.mutation.method && r.URL.Path == operation.mutation.uri:
							writeAPIError(w, http.StatusConflict, "invalid_state", "workspace must be stopped", false)
						default:
							t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
						}
						return w.Result(), nil
					})})
					h.env["KIBOTOS_API_KEY"] = "kb_shortcuts"
					invocation := slices.Clone(args)
					if flag == "CI" {
						h.env["CI"] = "true"
					} else {
						invocation = append(invocation, flag)
					}
					stdout, stderr := guidedResult(t, h, "", ExitError, invocation...)
					requireText(t, stderr, "workspace must be stopped")
					rejectText(t, stderr, "[Y/n]")
					if flag == "--json" {
						requireText(t, stderr, `"code": "invalid_state"`, `"status": 409`)
					}
					want := []string{operation.mutation.method + " " + operation.mutation.uri}
					if operation.readByDefault {
						want = append([]string{"GET " + wave3Workspace}, want...)
					}
					if stdout != "" || !slices.Equal(requests, want) {
						t.Fatalf("requests = %v, want %v, stdout = %q", requests, want, stdout)
					}
				})
			}
		})
	}
}

func TestShortcutVolumeRemoval(t *testing.T) {
	const running = `{"id":"wks_test","status":"available","volumes":[{"id":"vol_old","mount_path":"/data"}]}`
	for _, tc := range []struct {
		args     []string
		mutation expectedRequest
	}{
		{[]string{"volume", "detach", "wks_test", "vol_old"}, expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[]}`, shortcutStopped}},
		{[]string{"volumes", "set", "wks_test", "vol_new:/data"}, expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[{"id":"vol_new","mount_path":"/data"}]}`, shortcutStopped}},
		{[]string{"volume", "detach", "wks_test"}, expectedRequest{"DELETE", wave3Workspace + "/volume", "", shortcutStopped}},
		{[]string{"volumes", "set", "wks_test"}, expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[]}`, shortcutStopped}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"GET", wave3Workspace, "", running},
				expectedRequest{"POST", wave3Workspace + "/stop", "", shortcutStopped},
				tc.mutation,
				expectedRequest{"POST", wave3Workspace + "/start", "", availableWorkspace},
			)
			_, stderr := guidedResult(t, h, "y\n", ExitOK, append([]string{"ws"}, tc.args...)...)
			requireText(t, stderr, "is running. Stop,", "and start it again? [Y/n]:")
			if strings.Count(stderr, "[Y/n]") != 1 || strings.Contains(stderr, "[y/N]") {
				t.Fatalf("expected one confirmation, got %q", stderr)
			}
		})
	}
}

func TestShortcutWorkspacePacks(t *testing.T) {
	const current = `{"id":"wks_test","name":"dev","status":"available","zone":"global-1","packs":[{"id":"node"},{"id":"python"}]}`
	get := expectedRequest{"GET", wave3Workspace, "", current}
	add := expectedRequest{"PUT", wave3Workspace + "/packs", `{"packs":["node","python","desk"]}`, current}
	remove := expectedRequest{"PUT", wave3Workspace + "/packs", `{"packs":["node"]}`, current}
	const question = "Restart workspace now for Pack changes to take effect? [Y/n]:"
	t.Run("add and remove are incremental without an implicit restart", func(t *testing.T) {
		h := wave3Harness(t, get, add, get, remove)
		expectSuccess(t, h, "ws", "pack", "add", "wks_test", "desk", "--json")
		expectSuccess(t, h, "ws", "packs", "remove", "wks_test", "python", "--no-input")
	})
	t.Run("duplicate add and absent remove are no-ops", func(t *testing.T) {
		h := wave3Harness(t, get, get)
		expectSuccess(t, h, "ws", "packs", "add", "wks_test", "node", "--restart", "--json")
		expectSuccess(t, h, "ws", "pack", "remove", "wks_test", "desk", "--restart", "--json")
	})
	t.Run("a fifth Pack is rejected before mutation", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","status":"available","packs":[{"id":"one"},{"id":"two"},{"id":"three"},{"id":"four"}]}`})
		code, _, stderr := h.run("ws", "pack", "add", "wks_test", "five", "--restart", "--json")
		if code != ExitUsage {
			t.Fatalf("exit = %d, stderr = %q", code, stderr)
		}
		requireText(t, stderr, "at most four Packs")
	})
	t.Run("interactive add filters Packs by zone and confirms once", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", availableWorkspaceList},
			get,
			expectedRequest{"GET", "/v1/packs?zone=global-1", "", `{"data":[{"id":"desk"}]}`},
			add,
			expectedRequest{"POST", wave3Workspace + "/stop", "", shortcutStopped},
			expectedRequest{"POST", wave3Workspace + "/start", "", availableWorkspace},
		)
		_, stderr := guidedResult(t, h, "2\n\ny\n", ExitOK, "ws", "pack", "add")
		requireText(t, stderr, "Select a workspace:", "Select a Pack to add:", question)
		if strings.Count(stderr, "[Y/n]") != 1 {
			t.Fatalf("expected one confirmation, got %q", stderr)
		}
	})
	t.Run("interactive remove can cancel before mutation", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", `{"data":[` + stoppedWorkspaceJSON + `,` + current + `]}`}, get,
		)
		_, stderr := guidedResult(t, h, "\n2\nn\n", ExitError, "ws", "packs", "remove")
		requireText(t, stderr, "Select a workspace with bound Packs:", "Select a bound Pack to remove:", question, "operation cancelled")
		rejectText(t, stderr, "wks_other")
	})
	t.Run("explicit Pack restart always waits stopped", func(t *testing.T) {
		h := wave3Harness(t, get, remove,
			expectedRequest{"POST", wave3Workspace + "/stop", "", shortcutStopping},
			expectedRequest{"GET", wave3Workspace + "/wait?status=stopped&timeout=4", "", shortcutStopped},
			expectedRequest{"POST", wave3Workspace + "/start", "", shortcutStarting},
		)
		stdout, stderr := guidedResult(t, h, "", ExitOK, "ws", "pack", "remove", "wks_test", "python", "--restart", "--no-wait", "--wait-timeout", "4s", "--json")
		requireText(t, stdout, `"status": "starting"`)
		rejectText(t, stderr, "[Y/n]")
	})
	t.Run("set offers restart only when the selection changes", func(t *testing.T) {
		h := wave3Harness(t, get, get)
		_, stderr := guidedResult(t, h, "", ExitOK, "ws", "packs", "set", "wks_test", "node", "python")
		rejectText(t, stderr, question)
		_, stderr = guidedResult(t, h, "n\n", ExitError, "ws", "packs", "set", "wks_test", "desk")
		requireText(t, stderr, question)
	})
}

func TestShortcutWorkspaceRestart(t *testing.T) {
	t.Run("graceful restart waits for both states", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"POST", wave3Workspace + "/stop", "", shortcutStopping},
			expectedRequest{"GET", wave3Workspace + "/wait?status=stopped&timeout=4", "", shortcutStopped},
			expectedRequest{"POST", wave3Workspace + "/start", "", shortcutStarting},
			expectedRequest{"GET", wave3Workspace + "/wait?status=available&timeout=4", "", availableWorkspace},
		)
		stdout, _ := guidedResult(t, h, "", ExitOK, "ws", "restart", "wks_test", "--json", "--wait-timeout", "4s")
		requireText(t, stdout, `"status": "available"`)
	})
	t.Run("force confirmation defaults to no", func(t *testing.T) {
		_, stderr := guidedResult(t, wave3Harness(t), "\n", ExitError, "ws", "restart", "wks_test", "--force")
		requireText(t, stderr, "Force power-off workspace wks_test before starting it again? [y/N]:", "operation cancelled")
	})
	for _, confirm := range []bool{true, false} {
		name, input := "confirm", "y\n"
		args := []string{"ws", "restart", "wks_test", "--force", "--timeout", "1h", "--timeout-action", "stop", "--no-wait", "--wait-timeout", "4s"}
		if !confirm {
			name, input = "yes flag", ""
			args = append(args, "--yes")
		}
		t.Run(name, func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"POST", wave3Workspace + "/stop", `{"force":true}`, shortcutStopping},
				expectedRequest{"GET", wave3Workspace + "/wait?status=stopped&timeout=4", "", shortcutStopped},
				expectedRequest{"POST", wave3Workspace + "/start", `{"timeout":{"seconds":3600,"action":"stop"}}`, shortcutStarting},
			)
			stdout, stderr := guidedResult(t, h, input, ExitOK, args...)
			requireText(t, stdout, "starting")
			if confirm {
				requireText(t, stderr, "[y/N]")
			} else {
				rejectText(t, stderr, "[y/N]")
			}
		})
	}
}

func TestShortcutCopy(t *testing.T) {
	const endpoint = wave3Workspace + "/files/content"
	dir := t.TempDir()
	source := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(source, []byte("upload"), 0600); err != nil {
		t.Fatal(err)
	}
	h := wave3Harness(t,
		expectedRequest{"PUT", endpoint + "?create_only=true&mode=0640&path=%2Fdata%2Freport.txt&user=app", "upload", `{"path":"/data/report.txt","size_bytes":6}`},
		expectedRequest{"PUT", endpoint + "?path=report.txt", "upload", `{"path":"report.txt","size_bytes":6}`},
		expectedRequest{"GET", endpoint + "?path=%2Fdata%2Ftarget.txt&user=app", "", "from workspace"},
		expectedRequest{"GET", endpoint + "?path=%2Fdata%2Fother.txt", "", "other"},
		expectedRequest{"GET", endpoint + "?path=%2Fdata%2Ftarget.txt", "", "stream"},
	)
	stdout := expectSuccess(t, h, "cp", source, "wks_test:/data/", "--mode", "0640", "--user", "app", "--create-only", "--json")
	requireText(t, stdout, `"path": "/data/report.txt"`, `"size_bytes": 6`)
	expectSuccess(t, h, "cp", source, "wks_test:.")
	stdout = expectSuccess(t, h, "cp", "wks_test:/data/target.txt", dir, "--user", "app", "--json")
	requireText(t, stdout, `"path": "/data/target.txt"`, `"size_bytes": 14`)
	expectSuccess(t, h, "cp", "wks_test:/data/other.txt", dir+"/")
	for name, want := range map[string]string{"target.txt": "from workspace", "other.txt": "other"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("download %s = %q, %v", name, got, err)
		}
	}
	if stdout := expectSuccess(t, h, "cp", "wks_test:/data/target.txt", "-", "-q"); stdout != "stream" {
		t.Fatalf("stdout = %q, want stream", stdout)
	}
	for _, local := range []string{`C:\data\local.txt`, "d:/data/local.txt", "./file:name"} {
		if _, got, remote := splitCopyOperand(local); remote || got != local {
			t.Errorf("%q parsed as remote=%v, path=%q", local, remote, got)
		}
	}
}

func TestShortcutCopyUploadSizeLimit(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "oversize-")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1<<30 + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := wave3Harness(t).run("cp", f.Name(), "wks_test:/data/", "--json")
	if code != ExitUsage || stdout != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireText(t, stderr, "exceeds the 1 GiB upload limit")
}

func TestShortcutNetworkEdits(t *testing.T) {
	const allow = `,"allow":["203.0.113.0/24"]`
	const deny = `,"deny":["198.51.100.2"]`
	const egress = `,"egresses":[{"egress_id":"egr_old","destinations":["10.0.0.0/8"]}]`
	const resolver = `,"resolvers":[{"resolver_id":"rsv_old","path":{"type":"direct"}}]`
	const mode = `{"mode":"public_internet"`
	const current = `{"id":"wks_test","status":"available","network":` + mode + allow + deny + egress + resolver + `,"applied":true}}`
	const endpoint = wave3Workspace + "/network"
	for _, tc := range []struct {
		name   string
		args   []string
		fields string
	}{
		{"allow adds and deduplicates", []string{"allow", "wks_test", "203.0.113.2/24", "2001:db8::1", "2001:0db8::1"},
			`,"allow":["203.0.113.0/24","2001:db8::1"]` + deny + egress + resolver},
		{"allow removes", []string{"allow", "wks_test", "203.0.113.0/24", "--remove"}, deny + egress + resolver},
		{"deny adds", []string{"deny", "wks_test", "192.0.2.1", "198.51.100.2/32"},
			allow + `,"deny":["198.51.100.2","192.0.2.1"]` + egress + resolver},
		{"deny removes", []string{"deny", "wks_test", "198.51.100.2", "--remove"}, allow + egress + resolver},
		{"egress replaces", []string{"use-egress", "wks_test", "egr_new", "0.0.0.0/0", "2001:db8::/32"},
			allow + deny + `,"egresses":[{"egress_id":"egr_new","destinations":["0.0.0.0/0","2001:db8::/32"]}]` + resolver},
		{"egress clears", []string{"use-egress", "wks_test", "--clear"}, allow + deny + resolver},
		{"resolver replaces", []string{"use-resolver", "wks_test", "rsv_new"},
			allow + deny + egress + `,"resolvers":[{"resolver_id":"rsv_new","path":{"type":"direct"}}]`},
		{"resolver through egress", []string{"use-resolver", "wks_test", "rsv_new", "--via-egress"},
			allow + deny + egress + `,"resolvers":[{"resolver_id":"rsv_new","path":{"type":"egress","egress_id":"egr_old"}}]`},
		{"resolver clears", []string{"use-resolver", "wks_test", "--clear"}, allow + deny + egress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"GET", wave3Workspace, "", current},
				expectedRequest{"PUT", endpoint, mode + tc.fields + "}", `{"id":"wks_test","status":"available","network":` + mode + tc.fields + `,"applied":false}}`},
			)
			args := append([]string{"ws", "network"}, tc.args...)
			expectSuccess(t, h, append(args, "--no-wait", "--json")...)
		})
	}

	t.Run("waits for the edited policy by default", func(t *testing.T) {
		const edited = `{"mode":"public_internet","deny":["198.51.100.2","192.0.2.1"]`
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","status":"available","network":` + mode + deny + `,"applied":true}}`},
			expectedRequest{"PUT", endpoint, edited + "}", `{"id":"wks_test","status":"available","network":` + edited + `,"applied":false}}`},
			expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","status":"available","network":` + mode + deny + `,"applied":true}}`},
			expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","status":"available","network":` + edited + `,"applied":true}}`},
		)
		stdout := expectSuccess(t, h, "ws", "network", "deny", "wks_test", "192.0.2.1", "--json")
		requireText(t, stdout, `"applied": true`, "192.0.2.1")
	})
	t.Run("deny_all asks caller to switch mode before adding", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", `{"id":"wks_test","network":{"mode":"deny_all"}}`})
		code, _, stderr := h.run("ws", "network", "allow", "wks_test", "192.0.2.1", "--json")
		if code != ExitError {
			t.Fatalf("exit = %d, stderr = %q", code, stderr)
		}
		requireText(t, stderr, `"code": "invalid_state"`, "Switch to public_internet first")
	})
	t.Run("guided egress supplies a default destination", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", current},
			expectedRequest{"GET", "/v1/egresses", "", `{"data":[{"id":"egr_new","name":"office"}]}`},
			expectedRequest{"PUT", endpoint, mode + allow + deny + `,"egresses":[{"egress_id":"egr_new","destinations":["0.0.0.0/0"]}]` + resolver + "}", shortcutStopped},
		)
		_, stderr := guidedResult(t, h, "\n\n", ExitOK, "ws", "network", "use-egress", "wks_test")
		requireText(t, stderr, "Select an egress:", "Destination CIDRs (comma-separated, e.g. 0.0.0.0/0) [0.0.0.0/0]:")
	})
	t.Run("guided allow can omit workspace", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", availableWorkspaceList},
			expectedRequest{"GET", wave3Workspace, "", current},
			expectedRequest{"PUT", endpoint, mode + `,"allow":["203.0.113.0/24","192.0.2.1"]` + deny + egress + resolver + "}", shortcutStopped},
		)
		_, stderr := guidedResult(t, h, "2\n", ExitOK, "ws", "network", "allow", "192.0.2.1")
		requireText(t, stderr, "Select a workspace to update network policy:")
	})
	t.Run("legacy full policy supports a workspace named allow", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", `{"data":[{"id":"wks_test","name":"allow","status":"available"}]}`},
			expectedRequest{"PUT", endpoint, `{"mode":"deny_all","drain_seconds":0}`, shortcutStopped},
		)
		expectSuccess(t, h, "ws", "network", "allow", "--mode", "deny_all", "--drain-seconds", "0", "--no-wait", "--json")
	})
}

func TestShortcutOpen(t *testing.T) {
	const endpoint = wave3Workspace + "/exposures"
	const url = "https://preview.example.test"
	const active = `{"id":"exp_active","port":8080,"url":"` + url + `","status":"active"}`
	const activeList = `{"data":[` + active + `]}`
	t.Run("reuse matching active exposure across pages", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", endpoint, "", `{"data":[{"port":8080,"url":"https://expired.example.test","status":"active","expires_at":"2000-01-01T00:00:00Z"}],"next_page_token":"second"}`},
			expectedRequest{"GET", endpoint + "?page_token=second", "", activeList},
		)
		stdout := expectSuccess(t, h, "open", "wks_test", "8080", "--ttl", "30m", "-q")
		if stdout != url+"\n" || len(h.openedURLs) != 0 {
			t.Fatalf("stdout = %q, opened = %v", stdout, h.openedURLs)
		}
	})
	t.Run("create with TTL and return JSON without opening browser", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", endpoint, "", `{"data":[{"port":8080,"status":"revoked"},{"port":9090,"status":"active"}]}`},
			expectedRequest{"POST", endpoint, `{"port":8080,"ttl_seconds":1800}`, active},
		)
		stdout := expectSuccess(t, h, "open", "wks_test", "8080", "--ttl", "30m", "--json")
		requireText(t, stdout, `"id": "exp_active"`, `"url": "`+url+`"`)
		if len(h.openedURLs) != 0 {
			t.Fatalf("opened = %v", h.openedURLs)
		}
	})
	t.Run("guided open picks available workspace and defaults port", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", availableWorkspaceList},
			expectedRequest{"GET", endpoint, "", activeList},
		)
		stdout, stderr := guidedResult(t, h, "\n\n", ExitOK, "open")
		requireText(t, stderr, "Select an available workspace:", "Port (1-65535) [8080]:")
		rejectText(t, stderr, "wks_other")
		if stdout != url+"\n" || !slices.Equal(h.openedURLs, []string{url}) {
			t.Fatalf("stdout = %q, opened = %v", stdout, h.openedURLs)
		}
	})
	t.Run("no-open prints the URL without opening browser", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", endpoint, "", activeList})
		stdout := expectSuccess(t, h, "open", "wks_test", "8080", "--no-open")
		if stdout != url+"\n" || len(h.openedURLs) != 0 {
			t.Fatalf("stdout = %q, opened = %v", stdout, h.openedURLs)
		}
	})
}

func TestShortcutValidationBeforeAuth(t *testing.T) {
	h := newTestHarness(t, &http.Client{Transport: cliRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		return nil, nil
	})})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ws", "restart"}, "Pass a workspace ID or name"},
		{[]string{"ws", "restart", "wks_test", "--timeout-action", "stop"}, "Pass --timeout with --timeout-action"},
		{[]string{"ws", "restart", "wks_test", "--no-wait", "--wait-timeout", "bogus"}, "--wait-timeout"},
		{[]string{"ws", "pack", "add"}, "Pass a workspace ID or name"},
		{[]string{"ws", "pack", "add", "wks_test"}, "Pass a Pack ID"},
		{[]string{"ws", "packs", "remove", "wks_test"}, "Pass a Pack ID"},
		{[]string{"cp", `C:\local.txt`, "d:/out.txt"}, "Use exactly one workspace:path"},
		{[]string{"cp", "wks_one:/file", "wks_two:/file"}, "Use exactly one workspace:path"},
		{[]string{"cp", "wks_one:", "./file"}, "nonempty paths"},
		{[]string{"cp", "wks_one:/file", "out", "--create-only"}, "upload options"},
		{[]string{"ws", "network", "allow"}, "Pass a workspace ID or name"},
		{[]string{"ws", "network", "deny", "wks_test"}, "Pass one or more IP addresses"},
		{[]string{"ws", "network", "allow", "wks_test", "10.0.0.0/99"}, "requires IP addresses or CIDRs"},
		{[]string{"ws", "network", "use-egress", "wks_test", "egr_test"}, "Pass destination CIDRs"},
		{[]string{"ws", "network", "use-egress", "wks_test", "egr_test", "bad"}, "invalid destination CIDR"},
		{[]string{"ws", "network", "use-egress", "wks_test", "egr_test", "--clear"}, "--clear cannot be combined"},
		{[]string{"ws", "network", "use-resolver", "wks_test"}, "resolver ID or name"},
		{[]string{"ws", "network", "use-resolver", "wks_test", "--clear", "--via-egress"}, "--clear cannot be combined"},
		{[]string{"open"}, "Pass a workspace ID or name"},
		{[]string{"open", "wks_test"}, "Pass a port"},
		{[]string{"open", "wks_test", "65536"}, "invalid port"},
		{[]string{"open", "wks_test", "8080", "--ttl", "25h"}, "--ttl must be at most 24h"},
		{[]string{"shell"}, "Pass a workspace ID or name"},
		{[]string{"shell", "wks_test", "--env", "invalid"}, "invalid --env entry"},
	} {
		for _, flag := range []string{"--no-input", "--json"} {
			t.Run(strings.Join(tc.args, " ")+" "+flag, func(t *testing.T) {
				stdout, stderr := guidedResult(t, h, "", ExitUsage, append(slices.Clone(tc.args), flag)...)
				requireText(t, stderr, tc.want)
				if stdout != "" {
					t.Fatalf("stdout = %q", stdout)
				}
				if flag == "--json" {
					requireText(t, stderr, `"code": "invalid_usage"`, `"exit_code": 2`)
				}
			})
		}
	}
	for _, args := range [][]string{{"shell"}, {"shell", "wks_test", "--start"}, {"shell", "wks_test", "--no-input"}} {
		_, stderr := guidedResult(t, h, "", ExitUsage, args...)
		requireText(t, stderr, "shell requires a terminal on stdin")
	}
	_, stderr := guidedResult(t, h, "", ExitUsage, "shell", "wks_test", "--json")
	requireText(t, stderr, "shell cannot be combined with JSON output")
}

func TestShortcutVersionFlags(t *testing.T) {
	h := wave3Harness(t)
	for _, tc := range []struct {
		flags, subcommand []string
		check             string
	}{
		{[]string{"--version"}, []string{"version"}, "CLI Version:"},
		{[]string{"-v", "--json"}, []string{"version", "--json"}, `"version": "0.1.0-test"`},
		{[]string{"--version", "-q"}, []string{"version", "-q"}, "0.1.0-test\n"},
	} {
		actual := expectSuccess(t, h, tc.flags...)
		baseline := expectSuccess(t, h, tc.subcommand...)
		requireText(t, actual, tc.check)
		if actual != baseline {
			t.Fatalf("%v output = %q, want %q", tc.flags, actual, baseline)
		}
		if slices.Contains(tc.flags, "-q") && actual != tc.check {
			t.Fatalf("quiet output = %q, want %q", actual, tc.check)
		}
	}
}
