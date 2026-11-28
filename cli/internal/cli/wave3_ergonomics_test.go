package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/felinics/kibotos-sdk/cli/internal/testhttp"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

const (
	workspaceListPath      = "/v1/workspaces"
	wave3Workspace         = "/v1/workspaces/wks_test"
	availableWorkspace     = `{"id":"wks_test","name":"dev","status":"available","zone":"global-1"}`
	stoppedWorkspaceJSON   = `{"id":"wks_other","name":"offline","status":"stopped","zone":"global-1"}`
	availableWorkspaceList = `{"data":[` + stoppedWorkspaceJSON + `,` + availableWorkspace + `]}`
)

func guidedResult(t *testing.T, h *testHarness, input string, status int, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr := h.runInteractive(input, args...)
	if code != status {
		t.Fatalf("%v: got exit %d, want %d; stdout %q, stderr %q", args, code, status, stdout, stderr)
	}
	return stdout, stderr
}

func requireText(t *testing.T, got string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(got, fragment) {
			t.Errorf("missing %q in %q", fragment, got)
		}
	}
}

func rejectText(t *testing.T, got string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(got, fragment) {
			t.Errorf("unexpected %q in %q", fragment, got)
		}
	}
}

func TestWave3GuidedExec(t *testing.T) {
	for _, tc := range []struct {
		name, input, request string
		args                 []string
	}{
		{"shell command", "\nprintf hello\n", `{"argv":["/bin/sh","-c","printf hello"]}`, nil},
		{"explicit argv", "\n", `{"argv":["uname","-a"]}`, []string{"--", "uname", "-a"}},
		{"command with stdin", "\ncat\npayload", `{"argv":["/bin/sh","-c","cat"],"stdin":"payload"}`, []string{"--stdin", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t,
				expectedRequest{"GET", workspaceListPath, "", availableWorkspaceList},
				expectedRequest{"POST", wave3Workspace + "/exec", tc.request, `{"exit_code":0,"stdout":"hello"}`},
			)
			stdout, stderr := guidedResult(t, h, tc.input, ExitOK, append([]string{"exec"}, tc.args...)...)
			if stdout != "hello" {
				t.Errorf("exec stdout = %q", stdout)
			}
			requireText(t, stderr, "Select an available workspace:", "dev (wks_test)")
			rejectText(t, stderr, "offline", "Workdir", "Environment", "Timeout")
			if tc.name == "explicit argv" {
				rejectText(t, stderr, "Command:")
			} else {
				requireText(t, stderr, "Command:")
			}
		})
	}
}

func TestWave3EmptyWorkspacePicker(t *testing.T) {
	h := wave3Harness(t, expectedRequest{"GET", workspaceListPath, "", `{"data":[` + stoppedWorkspaceJSON + `]}`})
	_, stderr := guidedResult(t, h, "", ExitError, "files", "ls")
	requireText(t, stderr, "no available workspaces found")
	rejectText(t, stderr, "Select an available workspace:", "offline")
}

func TestWave3GuidedFiles(t *testing.T) {
	source := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(source, []byte("upload body"), 0600); err != nil {
		t.Fatal(err)
	}
	const files = wave3Workspace + "/files"
	const entry = `{"path":"/app/file","type":"file"}`
	for _, tc := range []struct {
		name, input string
		args        []string
		status      int
		requests    []expectedRequest
		prompts     []string
	}{
		{"ls", "\n", []string{"ls"}, ExitOK,
			[]expectedRequest{{"GET", files + "?depth=1&path=.", "", `{"path":".","entries":[]}`}}, nil},
		{"stat", "\n/app/file\n", []string{"stat"}, ExitOK,
			[]expectedRequest{{"GET", files + "/stat?path=%2Fapp%2Ffile", "", entry}}, []string{"Remote path:"}},
		{"get", "\n/app/file\n", []string{"get"}, ExitOK,
			[]expectedRequest{{"GET", files + "/content?path=%2Fapp%2Ffile", "", "file bytes"}}, []string{"Remote path:"}},
		{"put", "\n" + source + "\n\n", []string{"put"}, ExitOK,
			[]expectedRequest{{"PUT", files + "/content?path=.%2Fupload.txt", "upload body", `{"path":"./upload.txt"}`}},
			[]string{"Local file path:", "Remote destination path [./upload.txt]:"}},
		{"put stdin", "\n-\n/app/file\nupload body", []string{"put"}, ExitOK,
			[]expectedRequest{{"PUT", files + "/content?path=%2Fapp%2Ffile", "upload body", entry}},
			[]string{"Local file path:", "Remote destination path:"}},
		{"mkdir", "\n/app/dir\n", []string{"mkdir"}, ExitOK,
			[]expectedRequest{{"POST", files + "/mkdir", `{"path":"/app/dir"}`, `{"path":"/app/dir","type":"directory"}`}},
			[]string{"Directory path:"}},
		{"mv", "\n/app/file\n/app/dir/new\n", []string{"mv"}, ExitOK,
			[]expectedRequest{{"POST", files + "/move", `{"source":"/app/file","destination":"/app/dir/new"}`, entry}},
			[]string{"Source path:", "Destination path:"}},
		{"rm cancel", "\n/app/file\n\n", []string{"rm"}, ExitError, nil, []string{"Remote path to delete:", "[y/N]", "operation cancelled"}},
		{"rm accept", "\n/app/file\ny\n", []string{"rm"}, ExitOK,
			[]expectedRequest{{"DELETE", files + "?path=%2Fapp%2Ffile", "", ""}}, []string{"Remote path to delete:", "[y/N]"}},
		{"rm confirmed by flag", "\n/app/file\n", []string{"rm", "--yes"}, ExitOK,
			[]expectedRequest{{"DELETE", files + "?path=%2Fapp%2Ffile", "", ""}}, []string{"Remote path to delete:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t, append([]expectedRequest{{"GET", workspaceListPath, "", availableWorkspaceList}}, tc.requests...)...)
			out, errOut := guidedResult(t, h, tc.input, tc.status, append([]string{"files"}, tc.args...)...)
			requireText(t, errOut, "Select an available workspace:", "dev (wks_test)")
			requireText(t, errOut, tc.prompts...)
			rejectText(t, errOut, "offline", "User:", "Depth:", "Range:", "Mode:", "Create only:", "Local destination")
			if tc.name == "rm confirmed by flag" {
				rejectText(t, errOut, "[y/N]")
			}
			if tc.name == "get" && out != "file bytes" {
				t.Errorf("get stdout = %q", out)
			}
		})
	}
}

type guidedTunnelListener struct {
	conn      chan net.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *guidedTunnelListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conn:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *guidedTunnelListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}
func (l *guidedTunnelListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 54321}
}

type tunnelOutput chan string

func (w tunnelOutput) Write(data []byte) (int, error) {
	w <- string(data)
	return len(data), nil
}

func TestWave3GuidedTunnel(t *testing.T) {
	for _, port := range []string{"", "9090"} {
		name, expectedPort := "explicit", port
		if port == "" {
			name, expectedPort = "default", "8080"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			requests := make(chan string, 2)
			srv := testhttp.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- r.URL.RequestURI():
				default:
					t.Errorf("unexpected extra tunnel request %s", r.URL.RequestURI())
				}
				switch r.URL.Path {
				case workspaceListPath:
					_, _ = io.WriteString(w, availableWorkspaceList)
				case wave3Workspace + "/tunnels/ws":
					ws, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer ws.CloseNow()
					if err := ws.Write(ctx, websocket.MessageBinary, []byte("guest")); err != nil {
						t.Error(err)
					}
					_, _, _ = ws.Read(ctx)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			h := newTestHarness(t, srv.Client())
			h.env["KIBOTOS_API_KEY"] = "kb_wave3_cli"
			h.env["KIBOTOS_API_URL"] = srv.URL
			out := make(tunnelOutput, 1)
			var stderr bytes.Buffer
			app := newApp(Options{
				Stdin: strings.NewReader("\n" + port + "\n"), Stdout: out, Stderr: &stderr,
				Store: h.store, EnvLookup: func(k string) string { return h.env[k] }, HTTPClient: h.httpClient,
				InteractiveOverride: kibotos.Ptr(true),
			})
			listener := &guidedTunnelListener{conn: make(chan net.Conn), closed: make(chan struct{})}
			cmd := app.newTunnelCmd(func(_ context.Context, network, address string) (net.Listener, error) {
				if network != "tcp" || address != "127.0.0.1:18080" {
					t.Errorf("listen = %s %s", network, address)
				}
				return listener, nil
			})
			cmd.SetArgs([]string{})
			done := make(chan error, 1)
			go func() { done <- cmd.ExecuteContext(ctx) }()
			select {
			case line := <-out:
				if line != "Listening on 127.0.0.1:54321\n" {
					t.Errorf("tunnel stdout = %q", line)
				}
			case err := <-done:
				t.Fatalf("tunnel exited before listening: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			local, forwarded := net.Pipe()
			defer local.Close()
			select {
			case listener.conn <- forwarded:
			case <-ctx.Done():
				_ = forwarded.Close()
				t.Fatal(ctx.Err())
			}
			_ = local.SetReadDeadline(time.Now().Add(3 * time.Second))
			data := make([]byte, len("guest"))
			if _, err := io.ReadFull(local, data); err != nil || string(data) != "guest" {
				t.Errorf("forwarded %q, read error %v", data, err)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if len(requests) != 2 {
				t.Fatalf("tunnel made %d requests, want 2", len(requests))
			}
			if got := []string{<-requests, <-requests}; !reflect.DeepEqual(got, []string{
				workspaceListPath, wave3Workspace + "/tunnels/ws?port=" + expectedPort,
			}) {
				t.Errorf("tunnel request URIs = %v", got)
			}
			requireText(t, stderr.String(), "Select an available workspace:", "Guest port to forward (1-65535) [8080]:")
			rejectText(t, stderr.String(), "offline", "Listen address", "Family", "Unix path")
		})
	}
}

func TestWave3GuidedPackGetAndEmptyLists(t *testing.T) {
	const pack = `{"id":"node-22","display_name":"Node.js","version":"22","zones":["global-1"]}`
	h := wave3Harness(t,
		expectedRequest{"GET", "/v1/packs", "", `{"data":[` + pack + `]}`},
		expectedRequest{"GET", "/v1/packs/node-22", "", pack},
		expectedRequest{"GET", "/v1/packs", "", `{"data":[]}`},
		expectedRequest{"GET", "/v1/egresses", "", `{"data":[]}`},
		expectedRequest{"GET", "/v1/resolvers", "", `{"data":[]}`},
		expectedRequest{"GET", "/v1/packs", "", `{"data":[]}`},
	)
	_, stderr := guidedResult(t, h, "\n", ExitOK, "pack", "get")
	requireText(t, stderr, "Select a Pack:", "node-22", "Node.js · 22 · zones: global-1")
	rejectText(t, stderr, "Revision:")
	for _, pair := range [][2]string{{"pack", "Packs"}, {"egress", "egresses"}, {"resolver", "resolvers"}} {
		if got := expectSuccess(t, h, pair[0], "list"); got != "No "+pair[1]+" found.\n" {
			t.Errorf("%s empty list = %q", pair[0], got)
		}
	}
	if got := expectSuccess(t, h, "pack", "list", "-q"); got != "" {
		t.Errorf("quiet empty list = %q", got)
	}
}

func TestWave3GuidedNetworkResources(t *testing.T) {
	const egress = `{"id":"egr_test","name":"proxy","zone":"global-1","protocol":"socks5","socks5":{"address":"proxy:1080"}}`
	const resolver = `{"id":"rsv_test","name":"internal","zone":"global-1","protocol":"udp","udp":{"address":"10.0.0.53"}}`
	for _, tc := range []struct {
		name, input string
		args        []string
		status      int
		requests    []expectedRequest
		prompts     []string
	}{
		{"egress create", "proxy:1080\nproxy\n", []string{"egress", "create"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/egresses", `{"name":"proxy","protocol":"socks5","udp_enabled":false,"socks5":{"address":"proxy:1080"}}`, egress}},
			[]string{"SOCKS5 address (host:port):", "Name (optional):"}},
		{"egress explicit address", "", []string{"egress", "create", "--address", "proxy:1080"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/egresses", `{"protocol":"socks5","udp_enabled":false,"socks5":{"address":"proxy:1080"}}`, egress}}, nil},
		{"egress get", "\n", []string{"egress", "get"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/egresses", "", `{"data":[` + egress + `]}`}, {"GET", "/v1/egresses/egr_test", "", egress}},
			[]string{"Select an egress:", "proxy (egr_test)", "proxy:1080 · global-1"}},
		{"egress rename", "\nnew proxy\n", []string{"egress", "update"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/egresses", "", `{"data":[` + egress + `]}`}, {"PATCH", "/v1/egresses/egr_test", `{"name":"new proxy"}`, egress}},
			[]string{"Select an egress:", "New egress name [proxy]:"}},
		{"egress unchanged", "\n", []string{"egress", "update", "egr_test"}, ExitUsage,
			[]expectedRequest{{"GET", "/v1/egresses/egr_test", "", egress}},
			[]string{"New egress name [proxy]:", "no changes specified"}},
		{"egress delete confirmed", "\ny\n", []string{"egress", "delete"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/egresses", "", `{"data":[` + egress + `]}`}, {"DELETE", "/v1/egresses/egr_test", "", ""}},
			[]string{"Select an egress:", "Delete egress proxy (egr_test)? [y/N]:"}},
		{"resolver create udp", "\n10.0.0.53\ninternal\n", []string{"resolver", "create"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/resolvers", `{"name":"internal","protocol":"udp","udp":{"address":"10.0.0.53"}}`, resolver}},
			[]string{"DNS protocol:", "Standard UDP DNS", "DNS upstream address (host or host:port):", "Name (optional):"}},
		{"resolver create https", "4\nhttps://dns.example/dns-query\n\n", []string{"resolver", "create"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/resolvers", `{"protocol":"https","https":{"url":"https://dns.example/dns-query"}}`, resolver}},
			[]string{"DNS protocol:", "DNS-over-HTTPS URL (https://...):", "Name (optional):"}},
		{"resolver explicit flags", "", []string{"resolver", "create", "--protocol", "tls", "--address", "dns.example", "--name", "TLS DNS"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/resolvers", `{"name":"TLS DNS","protocol":"tls","tls":{"address":"dns.example"}}`, resolver}}, nil},
		{"resolver explicit upstream without name", "", []string{"resolver", "create", "--protocol", "udp", "--address", "10.0.0.53:53"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/resolvers", `{"protocol":"udp","udp":{"address":"10.0.0.53:53"}}`, resolver}}, nil},
		{"resolver URL implies HTTPS", "", []string{"resolver", "create", "--url", "https://dns.example/dns-query"}, ExitOK,
			[]expectedRequest{{"POST", "/v1/resolvers", `{"protocol":"https","https":{"url":"https://dns.example/dns-query"}}`, resolver}}, nil},
		{"resolver get", "\n", []string{"resolver", "get"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/resolvers", "", `{"data":[` + resolver + `]}`}, {"GET", "/v1/resolvers/rsv_test", "", resolver}},
			[]string{"Select a resolver:", "internal (rsv_test)", "udp · 10.0.0.53 · global-1"}},
		{"resolver rename", "\nnew resolver\n", []string{"resolver", "update"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/resolvers", "", `{"data":[` + resolver + `]}`}, {"PATCH", "/v1/resolvers/rsv_test", `{"name":"new resolver"}`, resolver}},
			[]string{"Select a resolver:", "New resolver name [internal]:"}},
		{"resolver delete cancelled", "\n\n", []string{"resolver", "delete"}, ExitError,
			[]expectedRequest{{"GET", "/v1/resolvers", "", `{"data":[` + resolver + `]}`}},
			[]string{"Select a resolver:", "Delete resolver internal (rsv_test)? [y/N]:", "operation cancelled"}},
		{"resolver delete confirmed", "\ny\n", []string{"resolver", "delete"}, ExitOK,
			[]expectedRequest{{"GET", "/v1/resolvers", "", `{"data":[` + resolver + `]}`}, {"DELETE", "/v1/resolvers/rsv_test", "", ""}},
			[]string{"Select a resolver:", "Delete resolver internal (rsv_test)? [y/N]:"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := wave3Harness(t, tc.requests...)
			_, errOut := guidedResult(t, h, tc.input, tc.status, tc.args...)
			requireText(t, errOut, tc.prompts...)
			rejectText(t, errOut, "Zone:", "Label:", "Password:", "Username:", "TLS server name:", "CA certificate:", "UDP enabled:")
			if tc.prompts == nil && errOut != "" {
				t.Errorf("explicit invocation prompted: %q", errOut)
			}
		})
	}
}

func TestWave3NetworkNameResolution(t *testing.T) {
	for _, kind := range []struct{ name, list, prefix string }{
		{"egress", "/v1/egresses", "egr_"},
		{"resolver", "/v1/resolvers", "rsv_"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			first := `{"id":"` + kind.prefix + `1","name":"Mixed"}`
			second := `{"id":"` + kind.prefix + `2","name":"mixed"}`
			listFirst := `{"data":[` + first + `],"next_page_token":"next"}`
			listSecond := `{"data":[` + second + `]}`
			nameLookup := []expectedRequest{{"GET", kind.list, "", listFirst}, {"GET", kind.list + "?page_token=next", "", listSecond}}
			h := wave3Harness(t,
				expectedRequest{"GET", kind.list + "/" + kind.prefix + "1", "", first},
				expectedRequest{"PATCH", kind.list + "/" + kind.prefix + "1", `{"name":"new"}`, first},
				expectedRequest{"DELETE", kind.list + "/" + kind.prefix + "1", "", ""},
			)
			if got := expectSuccess(t, h, kind.name, "get", kind.prefix+"1", "-q"); got != kind.prefix+"1\n" {
				t.Errorf("quiet ID = %q", got)
			}
			expectSuccess(t, h, kind.name, "update", kind.prefix+"1", "--name", "new", "--json")
			expectSuccess(t, h, kind.name, "delete", kind.prefix+"1", "--json")
			h = wave3Harness(t, append(append([]expectedRequest{}, nameLookup...), expectedRequest{"GET", kind.list + "/" + kind.prefix + "2", "", second})...)
			expectSuccess(t, h, kind.name, "get", "mixed", "--json")
			h = wave3Harness(t,
				expectedRequest{"GET", kind.list, "", `{"data":[` + first + `]}`},
				expectedRequest{"PATCH", kind.list + "/" + kind.prefix + "1", `{"name":"changed"}`, first},
				expectedRequest{"GET", kind.list, "", `{"data":[]}`},
				expectedRequest{"GET", kind.list + "/custom-id", "", first},
			)
			expectSuccess(t, h, kind.name, "update", "MIXED", "--name", "changed", "--json")
			expectSuccess(t, h, kind.name, "get", "custom-id", "--json")
			h = wave3Harness(t, nameLookup...)
			code, stdout, stderr := h.run(kind.name, "get", "MIXED", "--json")
			var detail jsonErrorEnvelope
			if err := json.Unmarshal([]byte(stderr), &detail); code != ExitUsage || stdout != "" || err != nil || detail.Error.Code != "ambiguous_name" || detail.Error.Hint == "" ||
				!reflect.DeepEqual(detail.Error.Details["candidates"], []any{kind.prefix + "1", kind.prefix + "2"}) {
				t.Fatalf("ambiguity = %d, %q, %q", code, stdout, stderr)
			}
			h = wave3Harness(t, append(append([]expectedRequest{}, nameLookup...), expectedRequest{"DELETE", kind.list + "/" + kind.prefix + "2", "", ""})...)
			_, stderr = guidedResult(t, h, "2\ny\n", ExitOK, kind.name, "delete", "MIXED")
			requireText(t, stderr, "named \"MIXED\"", "mixed ("+kind.prefix+"2)? [y/N]")
		})
	}
}

func TestWave3CatalogPagination(t *testing.T) {
	for _, tc := range []struct {
		command  string
		endpoint string
		collect  func(context.Context, *kibotos.Client) ([]string, error)
	}{
		{"pack", "/v1/packs", func(ctx context.Context, c *kibotos.Client) ([]string, error) {
			p, err := collectAllPacks(ctx, c, nil)
			if err != nil {
				return nil, err
			}
			return p.UnavailableZones, nil
		}},
		{"egress", "/v1/egresses", func(ctx context.Context, c *kibotos.Client) ([]string, error) {
			p, err := collectAllEgresses(ctx, c, nil)
			if err != nil {
				return nil, err
			}
			return p.UnavailableZones, nil
		}},
		{"resolver", "/v1/resolvers", func(ctx context.Context, c *kibotos.Client) ([]string, error) {
			p, err := collectAllResolvers(ctx, c, nil)
			if err != nil {
				return nil, err
			}
			return p.UnavailableZones, nil
		}},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			for _, repeat := range []bool{false, true} {
				second := `{"data":[],"unavailable_zones":["global-1","global-2"]}`
				if repeat {
					second = `{"data":[],"next_page_token":"next"}`
				}
				requests := []expectedRequest{
					{"GET", tc.endpoint, "", `{"data":[],"unavailable_zones":["global-1"],"next_page_token":"next"}`},
					{"GET", tc.endpoint + "?page_token=next", "", second},
				}
				h := wave3Harness(t, requests...)
				client, err := kibotos.NewClient("kb_wave3_cli", kibotos.WithHTTPClient(h.httpClient))
				if err != nil {
					t.Fatal(err)
				}
				zones, err := tc.collect(context.Background(), client)
				if repeat {
					if err == nil || !strings.Contains(err.Error(), "repeated page token") {
						t.Fatalf("repeated page token: %v", err)
					}
				} else if err != nil || !reflect.DeepEqual(zones, []string{"global-1", "global-2"}) {
					t.Fatalf("unavailable zones = %v, error = %v", zones, err)
				}
				if !repeat {
					out := expectSuccess(t, wave3Harness(t, requests...), tc.command, "list", "--json")
					var page kibotos.MultiZonePage[json.RawMessage]
					if err := json.Unmarshal([]byte(out), &page); err != nil ||
						!reflect.DeepEqual(page.UnavailableZones, []string{"global-1", "global-2"}) || page.HasMore() {
						t.Fatalf("list output = %q, error = %v", out, err)
					}
				}
			}
		})
	}
}

func TestWave3GuidedWorkspaceConfig(t *testing.T) {
	const stopped = `{"id":"wks_test","name":"dev","status":"stopped","zone":"global-1","resources":{"vcpus":2,"memory_mib":2048,"disk_mib":10240}}`
	const busy = `{"id":"wks_busy","name":"busy","status":"available"}`
	const list = `{"data":[` + busy + `,` + stopped + `]}`
	t.Run("resize changed fields", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", list},
			expectedRequest{"PATCH", wave3Workspace, `{"resources":{"memory_mib":4096,"disk_mib":20480}}`, stopped},
		)
		_, stderr := guidedResult(t, h, "2\n\n4096\n8192\n20480\n", ExitOK, "ws", "resize")
		requireText(t, stderr, "Select a workspace to resize:", "busy (wks_busy)", "vCPUs [2]:", "Memory (MiB) [2048]:", "Root disk (MiB, grow only) [10240]:", "enter an integer between 10240")
	})
	t.Run("resize with no changes", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", stopped})
		_, stderr := guidedResult(t, h, "\n\n\n", ExitUsage, "ws", "resize", "wks_test")
		requireText(t, stderr, "no resource changes specified")
	})
	t.Run("metrics", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", availableWorkspaceList},
			expectedRequest{"GET", wave3Workspace + "/metrics", "", `{"step_seconds":30,"host":[],"guest":[]}`},
		)
		stdout, stderr := guidedResult(t, h, "2\n", ExitOK, "ws", "metrics")
		requireText(t, stdout, "Step (seconds)")
		requireText(t, stderr, "Select a workspace:")
		rejectText(t, stderr, "Start:", "End:")
	})
	t.Run("set Packs", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", list},
			expectedRequest{"GET", "/v1/packs?zone=global-1", "", `{"data":[{"id":"node-22"},{"id":"kibotos-desktop-env"},{"id":"python-3.12"}]}`},
			expectedRequest{"PUT", wave3Workspace + "/packs", `{"packs":["node-22","kibotos-desktop-env","python-3.12"]}`, stopped},
		)
		_, stderr := guidedResult(t, h, "2\nnode-22, kibotos-desktop-env python-3.12\n", ExitOK, "ws", "packs", "set")
		requireText(t, stderr, "Select a workspace:", "Available packs: node-22, kibotos-desktop-env, python-3.12",
			"Pack IDs (comma-separated, blank to clear):")
	})
	t.Run("clear Packs after selecting workspace", func(t *testing.T) {
		const confirmation = "Clear all Packs on dev (wks_test)? [y/N]:"
		for _, tc := range []struct {
			name, input string
			yes         bool
			status      int
		}{
			{"confirmed", "2\n\ny\n", false, ExitOK},
			{"cancelled", "2\n\n\n", false, ExitError},
			{"assume yes", "2\n\n", true, ExitOK},
		} {
			t.Run(tc.name, func(t *testing.T) {
				requests := []expectedRequest{
					{"GET", workspaceListPath, "", list},
					{"GET", "/v1/packs?zone=global-1", "", `{"data":[]}`},
				}
				if tc.status == ExitOK {
					requests = append(requests, expectedRequest{"PUT", wave3Workspace + "/packs", `{"packs":[]}`, stopped})
				}
				args := []string{"ws", "packs", "set"}
				if tc.yes {
					args = append(args, "--yes")
				}
				_, stderr := guidedResult(t, wave3Harness(t, requests...), tc.input, tc.status, args...)
				requireText(t, stderr, "Pack IDs (comma-separated, blank to clear):")
				rejectText(t, stderr, "Available packs:")
				if tc.yes {
					rejectText(t, stderr, confirmation)
				} else {
					requireText(t, stderr, confirmation)
				}
			})
		}
	})
	t.Run("clear Packs on explicit workspace cancelled", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", stopped})
		_, stderr := guidedResult(t, h, "\n", ExitError, "ws", "packs", "set", "wks_test")
		requireText(t, stderr, "Clear all Packs on dev (wks_test)? [y/N]:", "operation cancelled")
	})
	t.Run("set volumes detach all", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", list},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[]}`, stopped},
		)
		_, stderr := guidedResult(t, h, "2\ny\n", ExitOK, "ws", "volumes", "set")
		requireText(t, stderr, "Select a running or stopped workspace:", "busy (wks_busy)", "Detach all volumes from dev (wks_test)? [y/N]:")
	})
}

func TestWave3GuidedWorkspaceVolume(t *testing.T) {
	const first = `{"id":"vol_first","mount_path":"/data","delete_with_workspace":true}`
	const second = `{"id":"vol_second","mount_path":"/cache"}`
	const stopped = `{"id":"wks_test","name":"dev","status":"stopped","zone":"global-1","volumes":[` + first + `]}`
	const both = `{"id":"wks_test","name":"dev","status":"stopped","volumes":[` + first + `,` + second + `]}`
	const empty = `{"id":"wks_empty","name":"empty","status":"stopped","volumes":[]}`
	const volumeList = `{"data":[
		{"id":"vol_zone","name":"remote","zone":"global-2","status":"available"},
		{"id":"vol_bound","name":"bound","zone":"global-1","status":"available","workspace_id":"wks_other"},
		{"id":"vol_pending","name":"pending","zone":"global-1","status":"creating"},
		{"id":"vol_second","name":"cache","zone":"global-1","status":"available"}
	]}`
	t.Run("positional IDs prompt for mount", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", stopped},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[` + first + `,` + second + `]}`, both},
		)
		_, stderr := guidedResult(t, h, "/cache\n", ExitOK, "ws", "volume", "attach", "wks_test", "vol_second")
		requireText(t, stderr, "Mount path (e.g. /data) [/data]:")
	})
	t.Run("legacy attach prompts for mount when already bound", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", stopped},
			expectedRequest{"GET", "/v1/volumes", "", volumeList},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[` + first + `,` + second + `]}`, both},
		)
		_, stderr := guidedResult(t, h, "\n/cache\n", ExitOK, "ws", "volume", "attach", "wks_test")
		requireText(t, stderr, "Select a volume to attach:", "cache (vol_second)", "Mount path (e.g. /data) [/data]:")
		rejectText(t, stderr, "vol_zone", "vol_bound", "vol_pending", "Delete with workspace")
	})
	t.Run("explicit attach and detach accept names", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", `{"data":[` + stopped + `]}`},
			expectedRequest{"GET", "/v1/volumes", "", volumeList},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[` + first + `,` + second + `]}`, both},
			expectedRequest{"GET", workspaceListPath, "", `{"data":[` + both + `]}`},
			expectedRequest{"GET", "/v1/volumes", "", volumeList},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[` + first + `]}`, stopped},
		)
		expectSuccess(t, h, "ws", "volume", "attach", "dev", "cache", "--mount", "/cache", "--json")
		expectSuccess(t, h, "ws", "volume", "detach", "dev", "cache", "--json")
	})
	t.Run("select one of several bindings", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", workspaceListPath, "", `{"data":[` + empty + `,` + both + `]}`},
			expectedRequest{"PUT", wave3Workspace + "/volumes", `{"volumes":[` + first + `]}`, stopped},
		)
		_, stderr := guidedResult(t, h, "\n2\n", ExitOK, "ws", "volume", "detach")
		requireText(t, stderr, "dev (wks_test)", "Select a volume to detach:", "vol_first at /data", "vol_second at /cache", "All volumes")
		rejectText(t, stderr, "wks_empty", "[y/N]")
	})
	t.Run("select all bindings and confirm", func(t *testing.T) {
		h := wave3Harness(t,
			expectedRequest{"GET", wave3Workspace, "", both},
			expectedRequest{"DELETE", wave3Workspace + "/volume", "", stopped},
		)
		_, stderr := guidedResult(t, h, "3\ny\n", ExitOK, "ws", "volume", "detach", "wks_test")
		requireText(t, stderr, "All volumes", "Detach all volumes from dev (wks_test)? [y/N]:")
	})
	t.Run("confirm single binding", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", wave3Workspace, "", stopped})
		_, stderr := guidedResult(t, h, "\n", ExitError, "ws", "volume", "detach", "wks_test")
		requireText(t, stderr, "Detach volume vol_first from dev (wks_test)? [y/N]:", "operation cancelled")
	})
	t.Run("explicit workspace without attachments", func(t *testing.T) {
		h := wave3Harness(t, expectedRequest{"GET", "/v1/workspaces/wks_empty", "", empty})
		_, stderr := guidedResult(t, h, "", ExitError, "ws", "volume", "detach", "wks_empty")
		requireText(t, stderr, "workspace has no attached volumes")
		rejectText(t, stderr, "[y/N]")
	})
}

func TestWave3NonInteractiveMissingInputs(t *testing.T) {
	cases := [][]string{
		{"exec", "--", "pwd"}, {"exec", "wks_test"}, {"exec", "-it"},
		{"files", "ls"}, {"files", "stat", "wks_test"}, {"files", "get", "wks_test"},
		{"files", "put", "wks_test"}, {"files", "put", "wks_test", "-"}, {"files", "mkdir", "wks_test"},
		{"files", "mv", "wks_test", "/source"}, {"files", "rm", "wks_test"}, {"files", "stat"},
		{"tunnel", "--remote", "8080"}, {"tunnel", "wks_test"},
		{"pack", "get"},
		{"egress", "create"}, {"egress", "get"}, {"egress", "update", "egr_test"}, {"egress", "update", "--name", "new"},
		{"egress", "update", "egr_test", "--tls"}, {"egress", "delete"},
		{"resolver", "create"}, {"resolver", "create", "--protocol", "https"}, {"resolver", "create", "--protocol", "tls"},
		{"resolver", "get"}, {"resolver", "update", "rsv_test"}, {"resolver", "update", "--name", "new"}, {"resolver", "delete"},
		{"ws", "get"}, {"ws", "update"}, {"ws", "update", "--name", "new"}, {"ws", "delete"},
		{"ws", "start"}, {"ws", "stop"}, {"ws", "timeout", "--seconds", "2h"}, {"ws", "wait", "--status", "available"},
		{"vol", "get"}, {"vol", "update"}, {"vol", "update", "--name", "new"}, {"vol", "resize", "--size", "20Gi"},
		{"vol", "delete"}, {"vol", "wait", "--status", "available"},
		{"exp", "list"}, {"exp", "get"}, {"exp", "get", "workspace-name"}, {"exp", "create", "--port", "8080"},
		{"exp", "revoke"}, {"exp", "revoke", "workspace-name"},
		{"ws", "resize", "--vcpus", "4"}, {"ws", "resize", "wks_test"}, {"ws", "metrics"},
		{"ws", "volumes", "set"}, {"ws", "packs", "set"}, {"ws", "volume", "attach"}, {"ws", "volume", "attach", "wks_test", "vol_first"},
		{"ws", "volume", "attach", "--volume", "vol_first"}, {"ws", "volume", "detach"},
		{"ws", "network", "--mode", "deny_all"},
	}
	for _, tc := range cases {
		for _, mode := range [][]string{{"--no-input"}, {"--json"}, {"-q"}, {"-o", "json"}} {
			t.Run(strings.Join(append(append([]string{}, mode...), tc...), " "), func(t *testing.T) {
				h := wave3Harness(t)
				for _, apiKey := range []string{"kb_wave3_cli", ""} {
					h.env["KIBOTOS_API_KEY"] = apiKey
					args := append(append([]string{}, mode...), tc...)
					stdout, stderr := guidedResult(t, h, "", ExitUsage, args...)
					if stdout != "" {
						t.Errorf("usage stdout = %q", stdout)
					}
					if mode[0] == "--json" || mode[0] == "-o" {
						var envelope jsonErrorEnvelope
						if err := json.Unmarshal([]byte(stderr), &envelope); err != nil || envelope.Error.ExitCode != ExitUsage || envelope.Error.Hint == "" {
							t.Fatalf("missing JSON usage hint: %q", stderr)
						}
					} else {
						requireText(t, stderr, "Hint:")
					}
				}
			})
		}
	}
}

func TestWave3ValidateExplicitFlagsBeforeAuthAndPrompts(t *testing.T) {
	h := wave3Harness(t)
	h.env["KIBOTOS_API_KEY"] = ""
	for _, args := range [][]string{
		{"exec", "wks_test", "pwd"}, {"exec", "--env", "invalid", "--", "pwd"}, {"exec", "-i"},
		{"exec", "-it", "--workdir", "/tmp", "--stream"}, {"exec", "--timeout", "0"},
		{"files", "ls", "--depth", "0"}, {"files", "get", "--range", "bytes=3-1"},
		{"files", "get", "wks_test", "file", "-", "--json"}, {"files", "mkdir", "--mode", "0899"},
		{"tunnel", "--remote", "65536"}, {"tunnel", "wks_test", "8080", "--remote", "8081"},
		{"tunnel", "wks_test", "8080", "--unix-path", "/tmp/sock"}, {"tunnel", "--family", "xyz"},
		{"tunnel", "--unix-path", "relative.sock"}, {"tunnel", "--listen", "missing-port"},
		{"egress", "create", "--address", "proxy:0"}, {"egress", "create", "--password", "a", "--password-env", "TEST"},
		{"egress", "update", "--address", "proxy:1080", "--name", "x"},
		{"resolver", "create", "--protocol", "unknown"}, {"resolver", "create", "--url", "http://dns.example"},
		{"resolver", "create", "--protocol", "https", "--address", "dns.example"},
		{"resolver", "update", "--protocol", "tcp", "--label", "team=dev"},
		{"ws", "resize", "--vcpus", "0"}, {"ws", "resize", "--disk-mib", "-1"},
		{"ws", "metrics", "--end", "invalid"}, {"ws", "metrics", "--start", "2026-11-28T00:00:00Z", "--end", "2026-11-27T00:00:00Z"},
		{"ws", "volumes", "set", "wks_test", "bad-binding"}, {"ws", "packs", "set", "wks_test", "node", "node"},
		{"ws", "volume", "attach", "wks_test", "vol_first", "--mount", "relative"},
		{"ws", "network", "wks_test", "--mode", "deny_all", "--allow", "192.0.2.1"},
		{"ws", "network", "wks_test", "--mode", "deny_all", "--deny", "192.0.2.1"},
		{"ws", "network", "wks_test", "--mode", "deny_all", "--egress", "egr_test=192.0.2.1"},
		{"ws", "network", "wks_test", "--mode", "deny_all", "--resolver", "rsv_test"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr := guidedResult(t, h, "", ExitUsage, args...)
			if stdout != "" {
				t.Errorf("usage stdout = %q", stdout)
			}
			rejectText(t, stderr, "Select a", "Name (optional):", "Command:", "SOCKS5 address (host:port):")
		})
	}
}
