package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/felinics/kibotos-sdk/cli/internal/testhttp"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

type cliRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cliRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type expectedRequest struct {
	method, uri, request, response string
}

func wave3Harness(t *testing.T, requests ...expectedRequest) *testHarness {
	t.Helper()
	pos := 0
	h := newTestHarness(t, &http.Client{Transport: cliRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if pos >= len(requests) {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		want := requests[pos]
		pos++
		if r.Method != want.method || r.URL.RequestURI() != want.uri {
			t.Fatalf("request = %s %s, want %s %s", r.Method, r.URL.RequestURI(), want.method, want.uri)
		}
		if r.Header.Get("Authorization") != "Bearer kb_wave3_cli" {
			t.Errorf("missing auth: %v", r.Header)
		}
		if want.request != "" {
			if r.Body == nil {
				t.Fatal("request body missing")
			}
			defer r.Body.Close()
			if strings.HasPrefix(want.request, "{") {
				var got, expected any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(want.request), &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, expected) {
					t.Errorf("request JSON = %#v, want %#v", got, expected)
				}
			} else {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != want.request || r.Header.Get("Content-Type") != "application/octet-stream" {
					t.Errorf("upload = %q, content type = %q, error = %v", body, r.Header.Get("Content-Type"), err)
				}
			}
		}
		w := httptest.NewRecorder()
		switch {
		case want.response == "":
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(want.response, "event:"):
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Errorf("stream Accept = %q", r.Header.Get("Accept"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
		case r.Header.Get("Range") == "bytes=1-3":
			w.Header().Set("Content-Range", "bytes 1-3/5")
			w.Header().Set("ETag", `"tag"`)
			w.WriteHeader(http.StatusPartialContent)
		}
		if want.response != "" {
			_, _ = io.WriteString(w, want.response)
		}
		return w.Result(), nil
	})})
	h.env["KIBOTOS_API_KEY"] = "kb_wave3_cli"
	t.Cleanup(func() {
		if pos != len(requests) {
			t.Errorf("handled %d of %d expected requests", pos, len(requests))
		}
	})
	return h
}

func expectSuccess(t *testing.T, h *testHarness, args ...string) string {
	t.Helper()
	code, stdout, stderr := h.run(args...)
	if code != 0 {
		t.Fatalf("%v returned %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
	}
	return stdout
}

func TestWave3CLIExecAndExitStatus(t *testing.T) {
	h := wave3Harness(t,
		expectedRequest{"POST", "/v1/workspaces/wks_test/exec",
			`{"argv":["cat","--number"],"working_dir":"/code","env":{"A":"v,1","B":"x=y"},"timeout_seconds":60,"user":"root","stdin":"input"}`,
			`{"stdout":"answer","stderr":"trace","exit_code":7,"termination_reason":"exited"}`},
		expectedRequest{"POST", "/v1/workspaces/wks_test/exec",
			`{"argv":["cat"]}`,
			"event: stdout\ndata: {\"data\":\"live\"}\n\nevent: exited\ndata: {\"signal\":15,\"termination_reason\":\"signaled\"}\n\n"},
		expectedRequest{"POST", "/v1/workspaces/wks_test/exec",
			`{"argv":["sleep","10"]}`, `{"exit_code":0,"termination_reason":"timed_out"}`},
	)
	code, stdout, stderr := h.run("exec", "wks_test", "--workdir", "/code", "--env", "A=v,1", "--env", "B=x=y", "--timeout", "60", "--user", "root", "--stdin", "input", "--", "cat", "--number")
	if code != 7 || stdout != "answer" || stderr != "trace" {
		t.Fatalf("exec = %d, %q, %q", code, stdout, stderr)
	}
	code, stdout, stderr = h.run("exec", "wks_test", "--stream", "--json", "--", "cat")
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if code != 143 || stderr != "" || len(lines) != 2 || !json.Valid([]byte(lines[0])) || !strings.Contains(lines[1], `"type":"exited"`) {
		t.Fatalf("stream = %d, %q, %q", code, stdout, stderr)
	}
	code, stdout, stderr = h.run("exec", "wks_test", "--json", "--", "sleep", "10")
	if code != 124 || !json.Valid([]byte(stdout)) || stderr != "" {
		t.Fatalf("timeout = %d, %q, %q", code, stdout, stderr)
	}
}

func TestWave3CLIFiles(t *testing.T) {
	const root = "/v1/workspaces/wks_test/files"
	const file = `{"path":"/data/file","mode":"0640","type":"file","size_bytes":3}`
	h := wave3Harness(t,
		expectedRequest{"GET", root + "?depth=2&path=%2Fdata&user=root", "", `{"path":"/data","entries":[` + file + `]}`},
		expectedRequest{"GET", root + "/stat?path=%2Fdata%2Ffile", "", file},
		expectedRequest{"GET", root + "/content?path=%2Fdata%2Ffile", "", "raw"},
		expectedRequest{"GET", root + "/content?path=%2Fdata%2Ffile", "", "raw"},
		expectedRequest{"PUT", root + "/content?create_only=true&mode=0640&path=%2Fdata%2Ffile&user=root", "upload", `{"path":"/data/file","size_bytes":6,"sha256":"hash"}`},
		expectedRequest{"PUT", root + "/content?path=%2Fdata%2Ffile", "upload-stdin", `{"path":"/data/file","size_bytes":12,"sha256":"hash"}`},
		expectedRequest{"POST", root + "/mkdir", `{"path":"/data/dir","mode":"0750"}`, `{"path":"/data/dir","type":"directory"}`},
		expectedRequest{"POST", root + "/move", `{"source":"/data/file","destination":"/data/renamed"}`, `{"path":"/data/renamed","type":"file"}`},
		expectedRequest{"DELETE", root + "?path=%2Fdata%2Frenamed", "", ""},
	)
	expectSuccess(t, h, "files", "ls", "wks_test", "/data", "--depth", "2", "--user", "root", "--json")
	expectSuccess(t, h, "files", "stat", "wks_test", "/data/file")
	if got := expectSuccess(t, h, "files", "get", "wks_test", "/data/file"); got != "raw" {
		t.Fatalf("download stdout = %q", got)
	}
	dest := filepath.Join(t.TempDir(), "download")
	got := expectSuccess(t, h, "files", "get", "wks_test", "/data/file", dest, "--range", "bytes=1-3", "--json")
	content, err := os.ReadFile(dest)
	if err != nil || string(content) != "raw" || !strings.Contains(got, `"content_range": "bytes 1-3/5"`) {
		t.Fatalf("download = %q, JSON = %q, error = %v", content, got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0644 {
			t.Errorf("download mode = %04o, want 0644", mode)
		}
	}
	src := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(src, []byte("upload"), 0600); err != nil {
		t.Fatal(err)
	}
	expectSuccess(t, h, "files", "put", "wks_test", src, "/data/file", "--user", "root", "--create-only", "--mode", "0640", "--json")
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"files", "put", "wks_test", "-", "/data/file", "--json"}, Options{
		Stdin: strings.NewReader("upload-stdin"), Stdout: &stdout, Stderr: &stderr, Store: h.store, HTTPClient: h.httpClient,
		EnvLookup: func(key string) string { return h.env[key] },
	})
	if code != 0 || !strings.Contains(stdout.String(), `"size_bytes": 12`) {
		t.Fatalf("stdin upload = %d, %s, %s", code, &stdout, &stderr)
	}
	expectSuccess(t, h, "files", "mkdir", "wks_test", "/data/dir", "--mode", "0750", "--json")
	expectSuccess(t, h, "files", "mv", "wks_test", "/data/file", "/data/renamed", "--json")
	expectSuccess(t, h, "files", "rm", "wks_test", "/data/renamed", "--json")
}

func TestWave3CLIWorkspaceConfiguration(t *testing.T) {
	const root = "/v1/workspaces/wks_test"
	const workspace = `{"id":"wks_test","status":"stopped","network":{"applied":null}}`
	const first = `{"id":"vol_1","mount_path":"/data","delete_with_workspace":true}`
	const second = `{"id":"vol_2","mount_path":"/cache","delete_with_workspace":true}`
	h := wave3Harness(t,
		expectedRequest{"POST", "/v1/workspaces", `{"template":{"id":"base"},"volumes":[{"id":"vol_1","mount_path":"/data"},{"id":"vol_2","mount_path":"/cache"}],"packs":["desk","node"]}`, workspace},
		expectedRequest{"GET", root, "", `{"id":"wks_test","volume":` + first + `}`},
		expectedRequest{"PUT", root + "/volumes", `{"volumes":[` + first + `,` + second + `]}`, workspace},
		expectedRequest{"GET", root, "", `{"id":"wks_test","volumes":[` + first + `,` + second + `]}`},
		expectedRequest{"PUT", root + "/volumes", `{"volumes":[` + second + `]}`, workspace},
		expectedRequest{"PUT", root + "/volumes", `{"volumes":[]}`, workspace},
		expectedRequest{"PUT", root + "/packs", `{"packs":["desk","node"]}`, workspace},
		expectedRequest{"PUT", root + "/packs", `{"packs":[]}`, workspace},
		expectedRequest{"PATCH", root, `{"resources":{"vcpus":4,"memory_mib":4096,"disk_mib":20480}}`, workspace},
		expectedRequest{"GET", root + "/metrics?end=2026-11-16T11%3A00%3A00Z&start=2026-11-16T10%3A00%3A00Z", "", `{"step_seconds":30,"host":[],"guest":[],"earliest_time":null}`},
		expectedRequest{"PUT", root + "/network", `{"mode":"public_internet","allow":["203.0.113.10"],"deny":["203.0.113.0/24"],"egresses":[{"egress_id":"egr_1","destinations":["10.0.0.0/8","192.0.2.1"]}],"resolvers":[{"resolver_id":"rsv_1","path":{"type":"egress","egress_id":"egr_1"}}],"drain_seconds":0}`, workspace},
	)
	expectSuccess(t, h, "ws", "create", "-t", "base", "--volume", "vol_1:/data", "--volume", "vol_2:/cache", "--pack", "desk", "--pack", "node", "--no-wait", "--json")
	expectSuccess(t, h, "ws", "volume", "attach", "wks_test", "vol_2", "--mount", "/cache", "--delete-with-workspace", "--json")
	expectSuccess(t, h, "ws", "volume", "detach", "wks_test", "vol_1", "--json")
	expectSuccess(t, h, "ws", "volumes", "set", "wks_test", "--json")
	expectSuccess(t, h, "ws", "packs", "set", "wks_test", "desk", "node", "--json")
	expectSuccess(t, h, "ws", "packs", "set", "wks_test", "--json")
	expectSuccess(t, h, "ws", "resize", "wks_test", "--vcpus", "4", "--memory-mib", "4096", "--disk-mib", "20480", "--json")
	expectSuccess(t, h, "ws", "metrics", "wks_test", "--start", "2026-11-16T10:00:00Z", "--end", "2026-11-16T11:00:00Z", "--json")
	expectSuccess(t, h, "ws", "network", "wks_test", "--mode", "public_internet", "--allow", "203.0.113.10", "--deny", "203.0.113.0/24", "--egress", "egr_1=10.0.0.0/8,192.0.2.1", "--resolver", "rsv_1=egr_1", "--drain-seconds", "0", "--json")
}

func TestWave3CLILegacyVolumeAttach(t *testing.T) {
	const root = "/v1/workspaces/wks_test"
	const empty = `{"id":"wks_test","status":"stopped","volumes":[]}`
	const first = `{"id":"vol_1","mount_path":"/data","delete_with_workspace":true}`
	const second = `{"id":"vol_2","mount_path":"/cache"}`
	const attached = `{"id":"wks_test","status":"stopped","volumes":[` + first + `]}`
	const legacy = `{"id":"wks_test","status":"stopped","volume":` + first + `}`
	const both = `{"id":"wks_test","status":"stopped","volumes":[` + first + `,` + second + `]}`

	for _, tc := range []struct{ name, workspace string }{
		{"volumes array", attached},
		{"legacy volume field", legacy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"GET", root, "", tc.workspace},
				expectedRequest{"PUT", root + "/volumes", `{"volumes":[` + first + `,` + second + `]}`, both},
			)
			expectSuccess(t, h, "ws", "volume", "attach", "wks_test", "--volume", "vol_2", "--mount-path", "/cache")
		})
	}
	t.Run("mount required with an existing volume", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", root, "", attached})
		code, _, stderr := h.run("ws", "volume", "attach", "wks_test", "--volume", "vol_2")
		if code != ExitUsage || !strings.Contains(stderr, "--mount-path") {
			t.Fatalf("attach without mount = %d, %q", code, stderr)
		}
	})
	t.Run("first volume may use the default mount", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", root, "", empty},
			expectedRequest{"PUT", root + "/volume", `{"id":"vol_1"}`, attached},
		)
		expectSuccess(t, h, "ws", "volume", "attach", "wks_test", "--volume", "vol_1")
	})
}

func TestWave3CLICatalog(t *testing.T) {
	const egress = `{"id":"egr_test","protocol":"socks5","socks5":{"address":"proxy:1080"}}`
	const resolver = `{"id":"rsv_test","protocol":"udp","udp":{"address":"10.0.0.53"}}`
	h := wave3Harness(t,
		expectedRequest{"GET", "/v1/packs?zone=global-1", "", `{"data":[{"id":"desk"}],"next_page_token":"next"}`},
		expectedRequest{"GET", "/v1/packs?page_token=next&zone=global-1", "", `{"data":[{"id":"node"}]}`},
		expectedRequest{"GET", "/v1/packs/desk", "", `{"id":"desk"}`},
		expectedRequest{"GET", "/v1/packs/desk/revisions/r1", "", `{"id":"r1","pack_id":"desk"}`},
		expectedRequest{"GET", "/v1/egresses?label.team=dev&limit=10", "", `{"data":[` + egress + `]}`},
		expectedRequest{"GET", "/v1/egresses/egr_test", "", egress},
		expectedRequest{"POST", "/v1/egresses", `{"name":"proxy","protocol":"socks5","udp_enabled":true,"socks5":{"address":"proxy:1080","password":"fixture","tls":{}}}`, egress},
		expectedRequest{"PATCH", "/v1/egresses/egr_test", `{"name":null,"labels":{"tier":"test"}}`, egress},
		expectedRequest{"PUT", "/v1/egresses/egr_test", `{"protocol":"socks5","udp_enabled":false,"socks5":{"address":"other:1080"},"drain_seconds":0}`, egress},
		expectedRequest{"DELETE", "/v1/egresses/egr_test", "", ""},
		expectedRequest{"GET", "/v1/resolvers", "", `{"data":[` + resolver + `]}`},
		expectedRequest{"GET", "/v1/resolvers/rsv_test", "", resolver},
		expectedRequest{"POST", "/v1/resolvers", `{"protocol":"udp","udp":{"address":"10.0.0.53"}}`, resolver},
		expectedRequest{"PATCH", "/v1/resolvers/rsv_test", `{"labels":{"old":null}}`, resolver},
		expectedRequest{"PUT", "/v1/resolvers/rsv_test", `{"protocol":"https","https":{"url":"https://dns.example/dns-query"}}`, resolver},
		expectedRequest{"DELETE", "/v1/resolvers/rsv_test", "", ""},
	)
	h.env["EGRESS_PASSWORD"] = "fixture"
	got := expectSuccess(t, h, "pack", "list", "-z", "global-1", "--json")
	if !strings.Contains(got, `"desk"`) || !strings.Contains(got, `"node"`) {
		t.Fatalf("pack list = %s", got)
	}
	expectSuccess(t, h, "pack", "get", "desk", "--json")
	expectSuccess(t, h, "pack", "get", "desk", "--revision", "r1", "--json")
	expectSuccess(t, h, "egress", "list", "--label", "team=dev", "--limit", "10", "--json")
	expectSuccess(t, h, "egress", "get", "egr_test", "--json")
	got = expectSuccess(t, h, "egress", "create", "--name", "proxy", "--address", "proxy:1080", "--tls", "--udp-enabled", "--password-env", "EGRESS_PASSWORD", "--json")
	if strings.Contains(got, "fixture") {
		t.Fatalf("egress printed the password: %s", got)
	}
	expectSuccess(t, h, "egress", "update", "egr_test", "--clear-name", "--label", "tier=test", "--json")
	expectSuccess(t, h, "egress", "update", "egr_test", "--address", "other:1080", "--drain-seconds", "0", "--json")
	expectSuccess(t, h, "egress", "delete", "egr_test", "--json")
	expectSuccess(t, h, "resolver", "list", "--json")
	expectSuccess(t, h, "resolver", "get", "rsv_test", "--json")
	expectSuccess(t, h, "resolver", "create", "--protocol", "udp", "--address", "10.0.0.53", "--json")
	expectSuccess(t, h, "resolver", "update", "rsv_test", "--unset-label", "old", "--json")
	expectSuccess(t, h, "resolver", "update", "rsv_test", "--protocol", "https", "--url", "https://dns.example/dns-query", "--json")
	expectSuccess(t, h, "resolver", "delete", "rsv_test", "--json")
}

func TestWave3CLIUsageErrors(t *testing.T) {
	h := wave3Harness(t)
	for _, args := range [][]string{
		{"exec", "wks_test", "pwd"},
		{"exec", "wks_test", "--timeout", "601", "--", "pwd"},
		{"exec", "wks_test", "-it", "--stream", "--", "sh"},
		{"exec", "wks_test", "--env", "invalid", "--", "pwd"},
		{"files", "get", "wks_test", "/app/file"},
		{"files", "ls", "wks_test", "--depth", "0"},
		{"ws", "volume", "attach", "wks_test", "vol_1"},
		{"ws", "volumes", "set", "wks_test", "vol_1:/data", "vol_2:/data"},
		{"ws", "packs", "set", "wks_test", "pack", "pack"},
		{"ws", "resize", "wks_test"},
		{"ws", "metrics", "wks_test", "--start", "invalid"},
		{"ws", "network", "wks_test", "--mode", "public_internet", "--resolver", "rsv_1=other"},
		{"egress", "update", "egr_1", "--name", "name", "--address", "proxy:1080"},
		{"resolver", "create", "--protocol", "https", "--url", "http://dns.example"},
		{"tunnel", "wks_test", "--remote", "8080", "--unix-path", "/run/app.sock"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := h.run(append([]string{"--json"}, args...)...)
			if code != ExitUsage || stdout != "" || !json.Valid([]byte(stderr)) {
				t.Fatalf("usage result = %d, %q, %q", code, stdout, stderr)
			}
		})
	}
}

func TestWave3TunnelBridge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v1/workspaces/wks_test/tunnels/ws?port=8080" {
			t.Errorf("unexpected tunnel URI %s", r.URL.RequestURI())
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		kind, body, err := ws.Read(ctx)
		if err != nil || kind != websocket.MessageBinary || string(body) != "local" {
			t.Errorf("guest got %s %q, error = %v", kind, body, err)
			return
		}
		if err := ws.Write(ctx, websocket.MessageBinary, []byte("guest")); err != nil {
			t.Error(err)
			return
		}
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	client, err := kibotos.NewClient("kb_wave3_cli", kibotos.WithBaseURL(srv.URL), kibotos.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := client.Workspaces.DialTunnel(ctx, "wks_test", kibotos.TunnelOptions{Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	local, bridge := net.Pipe()
	defer local.Close()
	_ = local.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan error, 1)
	go func() { done <- bridgeTunnel(ctx, bridge, ws) }()
	if _, err := io.WriteString(local, "local"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(local)
	if err != nil || string(response) != "guest" {
		t.Fatalf("local got %q, error = %v", response, err)
	}
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
