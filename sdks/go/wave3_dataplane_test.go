package kibotos_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
)

func TestWave3Proxy(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, path, uri string
		status          int
	}{
		{"guest error", "/nested/a b?#%", "/v1/workspaces/wks_1/proxy/8080/nested/a%20b%3F%23%25?q=a%2Fb", http.StatusServiceUnavailable},
		{"root redirect", "", "/v1/workspaces/wks_1/proxy/8080?q=a%2Fb", http.StatusSeeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if r.Method != http.MethodPatch || r.URL.RequestURI() != tc.uri ||
					r.Header.Get("Authorization") != "Bearer kb_wave3" || r.Header.Get("Content-Type") != "text/plain" ||
					r.GetBody != nil || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-Idempotency-Key") != "" {
					t.Errorf("unexpected proxy request: %s %s %v", r.Method, r.URL, r.Header)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "guest input" {
					t.Errorf("request body = %q, error = %v", body, err)
				}
				w.Header().Set("Location", "/guest/redirect")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, "guest response")
			})
			resp, err := c.Workspaces.Proxy(ctx, "wks_1", 8080, http.MethodPatch, tc.path, url.Values{"q": {"a/b"}}, strings.NewReader("guest input"),
				kibotos.WithHeader("Content-Type", "text/plain"), kibotos.WithIdempotencyKey("unused"), kibotos.WithHeader("X-Idempotency-Key", "unused"))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != tc.status || resp.Header.Get("Location") != "/guest/redirect" ||
				string(body) != "guest response" || attempts != 1 {
				t.Fatalf("status = %d, body = %q, location = %q, attempts = %d, error = %v", resp.StatusCode, body, resp.Header.Get("Location"), attempts, err)
			}
		})
	}
}

func TestWave3Exec(t *testing.T) {
	ctx := context.Background()
	req := kibotos.ExecRequest{
		Argv: []string{"cat", "-n"}, WorkingDir: "code", User: "root", Env: map[string]string{"A": "b"},
		TimeoutSeconds: 60, MaxOutputBytes: 1024, Stdin: kibotos.Ptr("text"),
	}
	c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/workspaces/wks_1/exec" ||
			r.Header.Get("Accept") != "application/json" || r.Header.Get("Idempotency-Key") != "" ||
			r.Header.Get("X-Kibotos-Organization") != "org_1" {
			t.Errorf("unexpected exec request: %s %s %v", r.Method, r.URL, r.Header)
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < time.Minute {
			t.Errorf("exec deadline should allow guest timeout, got %v", deadline)
		}
		checkJSONBody(t, r, `{"argv":["cat","-n"],"working_dir":"code","env":{"A":"b"},"user":"root","timeout_seconds":60,"stdin":"text","max_output_bytes":1024}`)
		_, _ = io.WriteString(w, `{"exit_code":7,"signal":null,"termination_reason":"exited","stdout":"txt","stderr":"err","truncated":true,"duration_ms":42}`)
	})
	result, err := c.Workspaces.Exec(ctx, "wks_1", req, kibotos.WithIdempotencyKey("unused"), kibotos.WithHeader("X-Kibotos-Organization", "org_1"))
	if err != nil || result.ExitCode == nil || *result.ExitCode != 7 || result.Signal != nil ||
		result.Stderr != "err" || result.Stdout != "txt" || !result.Truncated || result.DurationMS != 42 {
		t.Fatalf("result = %+v, err = %v", result, err)
	}

	t.Run("does not replay", func(t *testing.T) {
		attempts := 0
		c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
			attempts++
			w.Header().Set("Request-Id", "req_exec")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"dataplane_unavailable","retryable":true}}`)
		})
		_, err := c.Workspaces.Exec(ctx, "wks_1", req)
		var api *kibotos.APIError
		if !errors.As(err, &api) || api.RequestID != "req_exec" || !api.Retryable || attempts != 1 {
			t.Fatalf("attempts = %d, error = %+v", attempts, err)
		}
	})
}

func TestWave3ExecStream(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       string
		check      func(error) bool
	}{
		{"all events", "\uFEFF: comment\r\nevent: started\r\ndata: {\"pid\":7,\r\ndata: \"incarnation\":2}\r\n\r\n" +
			"event: stdout\rdata: {\"data\":\"hello\"}\r\r" +
			"event: stderr\ndata: {\"data\":\"💡\"}\n\nevent: heartbeat\ndata: {}\n\n" +
			"event: exited\ndata: {\"exit_code\":0,\"duration_ms\":12}\n\n",
			"started stdout stderr heartbeat exited", func(err error) bool { return err == nil }},
		{"api error", "event: error\ndata: {\"status\":503,\"code\":\"unavailable\",\"request_id\":\"req_1\"}\n\n", "error",
			func(err error) bool {
				var api *kibotos.APIError
				return errors.As(err, &api) && api.Status == 503 && api.RequestID == "req_1"
			}},
		{"unexpected EOF", "event: heartbeat\ndata: {}\n\n", "heartbeat", func(err error) bool { return errors.Is(err, io.ErrUnexpectedEOF) }},
		{"bad event", "event: started\ndata: {xxx\n\n", "", func(err error) bool { return err != nil && strings.Contains(err.Error(), "decode exec event") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Idempotency-Key") != "" {
					t.Errorf("unexpected request: %s, %v", r.Method, r.Header)
				}
				if _, ok := r.Context().Deadline(); ok {
					t.Error("unexpected default deadline for SSE")
				}
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				_, _ = io.WriteString(w, tc.body)
			})
			var seen []string
			err := c.Workspaces.ExecStream(context.Background(), "wks_1", kibotos.ExecRequest{Argv: []string{"cmd"}}, func(e kibotos.ExecEvent) error {
				seen = append(seen, e.Type)
				switch e.Type {
				case "started":
					if e.PID != 7 || e.Incarnation != 2 {
						t.Errorf("started = %+v", e)
					}
				case "stdout":
					if e.Data != "hello" {
						t.Errorf("stdout = %+v", e)
					}
				case "stderr":
					if e.Data != "💡" {
						t.Errorf("stderr = %+v", e)
					}
				case "exited":
					if e.ExitCode == nil || *e.ExitCode != 0 || e.DurationMS != 12 {
						t.Errorf("exit = %+v", e)
					}
				}
				return nil
			})
			if strings.Join(seen, " ") != tc.want || !tc.check(err) {
				t.Fatalf("events = %v, error = %v", seen, err)
			}
		})
	}
	t.Run("handler error", func(t *testing.T) {
		c := clientForHandler(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: started\ndata: {}\n\nevent: exited\ndata: {}\n\n")
		})
		stop := errors.New("stop")
		calls := 0
		err := c.Workspaces.ExecStream(context.Background(), "wks_1", kibotos.ExecRequest{}, func(kibotos.ExecEvent) error {
			calls++
			return stop
		})
		if calls != 1 || !errors.Is(err, stop) {
			t.Fatalf("calls = %d, error = %v", calls, err)
		}
	})
}

func TestWave3FileOperations(t *testing.T) {
	const root = "/v1/workspaces/wks_1/files"
	const path = "/data/你好 & file"
	const entry = `{"path":"/data/你好 & file","name":"file","type":"file","size_bytes":5,"mode":"0600","modified_at":"2026-11-16T10:00:00Z"}`
	query := url.Values{"path": {path}, "user": {"agent"}}.Encode()
	runControlCases(t, []controlCase{
		{"list", "GET", root, "depth=3&" + query, "", `{"path":"/data","entries":[` + entry + `]}`,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				result, err := c.Workspaces.ListFiles(ctx, "wks_1", path, kibotos.ListFilesOptions{User: "agent", Depth: 3})
				if err == nil && (len(result.Entries) != 1 || result.Entries[0].Mode != "0600" || result.Entries[0].ModifiedAt == nil) {
					t.Errorf("list = %+v", result)
				}
				return result, err
			}},
		{"stat", "GET", root + "/stat", query, "", entry,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.StatFile(ctx, "wks_1", path, kibotos.FileOptions{User: "agent"})
			}},
		{"mkdir", "POST", root + "/mkdir", "", `{"path":"data","mode":"0755","user":"agent"}`, entry,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				// Data-plane mutations deliberately use no idempotency key.
				return c.Workspaces.MakeDir(ctx, "wks_1", kibotos.MakeDirectoryRequest{Path: "data", Mode: "0755", User: "agent"})
			}},
		{"move", "POST", root + "/move", "", `{"source":"old","destination":"new","user":"agent"}`, entry,
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return c.Workspaces.MoveFile(ctx, "wks_1", kibotos.MoveFileRequest{Source: "old", Destination: "new", User: "agent"})
			}},
		{"remove", "DELETE", root, query, "", "",
			func(ctx context.Context, c *kibotos.Client) (any, error) {
				return nil, c.Workspaces.DeleteFile(ctx, "wks_1", path, kibotos.FileOptions{User: "agent"})
			}},
	})
}

type trackedBody struct {
	io.Reader
	reads  int
	closed bool
}

func (b *trackedBody) Read(p []byte) (int, error) {
	b.reads++
	return b.Reader.Read(p)
}
func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestWave3ReadFileStreamingAndRange(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("abc")}
	c, err := kibotos.NewClient("kb_test", kibotos.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/v1/workspaces/wks_1/files/content" || r.URL.Query().Get("user") != "root" ||
			r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-None-Match") != `"old"` {
			t.Errorf("unexpected read request: %s %s %v", r.Method, r.URL, r.Header)
		}
		return &http.Response{StatusCode: 206, ContentLength: 3, Header: http.Header{"Content-Range": {"bytes 2-4/5"}, "Etag": {`"new"`}}, Body: body}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	content, err := c.Workspaces.ReadFile(context.Background(), "wks_1", "report", kibotos.ReadFileOptions{
		User: "root", Range: "bytes=2-4", IfNoneMatch: `"old"`,
	})
	if err != nil || body.reads != 0 || content.StatusCode != 206 || content.ContentLength != 3 ||
		content.Header.Get("Content-Range") != "bytes 2-4/5" || content.Header.Get("ETag") != `"new"` {
		t.Fatalf("content = %+v, err = %v, reads = %d", content, err, body.reads)
	}
	data, err := io.ReadAll(content)
	if err != nil || string(data) != "abc" {
		t.Fatalf("data = %q, err = %v", data, err)
	}
	if err := content.Close(); err != nil || !body.closed {
		t.Fatalf("closed = %v, err = %v", body.closed, err)
	}

	c = clientForHandler(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotModified) })
	content, err = c.Workspaces.ReadFile(context.Background(), "wks_1", "report", kibotos.ReadFileOptions{})
	if err != nil || content.StatusCode != http.StatusNotModified {
		t.Fatalf("not modified content = %+v, error = %v", content, err)
	}
	content.Close()
}

func TestWave3WriteFileStreamsOnce(t *testing.T) {
	input := &trackedBody{Reader: bytes.NewReader([]byte{0, 0xff, 2})}
	const path = "/data/a&b"
	attempts := 0
	c := clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if input.reads != 0 || r.Method != http.MethodPut || r.URL.Path != "/v1/workspaces/wks_1/files/content" ||
			r.URL.Query().Get("path") != path || r.URL.Query().Get("user") != "root" || r.URL.Query().Get("mode") != "0600" ||
			r.URL.Query().Get("create_only") != "true" || r.Header.Get("Content-Type") != "application/octet-stream" || r.GetBody != nil {
			t.Errorf("unexpected streaming upload: reads=%d, %s %s %v", input.reads, r.Method, r.URL, r.Header)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, []byte{0, 0xff, 2}) {
			t.Errorf("body = %v, error = %v", got, err)
		}
		_, _ = io.WriteString(w, `{"path":"/data/a&b","size_bytes":3,"sha256":"hash"}`)
	})
	result, err := c.Workspaces.WriteFile(context.Background(), "wks_1", path, input, kibotos.WriteFileOptions{User: "root", Mode: "0600", CreateOnly: true})
	if err != nil || attempts != 1 || result.SizeBytes != 3 || result.Path != path || result.SHA256 != "hash" {
		t.Fatalf("result = %+v, attempts = %d, error = %v", result, attempts, err)
	}
	attempts = 0
	c = clientForHandler(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"code":"outcome_unknown","retryable":true}}`)
	})
	_, err = c.Workspaces.WriteFile(context.Background(), "wks_1", path, strings.NewReader("data"), kibotos.WriteFileOptions{})
	if attempts != 1 || !kibotos.IsRetryable(err) {
		t.Fatalf("attempts = %d, error = %v", attempts, err)
	}
}

func TestWave3WebSocketBinaryFraming(t *testing.T) {
	for channel := kibotos.WSChannelStdin; channel <= kibotos.WSChannelTunnel; channel++ {
		original := kibotos.WSFrame{StreamID: 0x01020304, Channel: channel, Payload: []byte{0x80, 0xff}}
		data, err := original.MarshalBinary()
		if err != nil || !bytes.Equal(data, []byte{0x01, 1, 2, 3, 4, byte(channel), 0x80, 0xff}) {
			t.Fatalf("channel %d encoded %x, error %v", channel, data, err)
		}
		frame, err := kibotos.ParseWSFrame(data)
		if err != nil || frame.StreamID != original.StreamID || frame.Channel != original.Channel || !bytes.Equal(frame.Payload, original.Payload) {
			t.Fatalf("decoded = %+v, error = %v", frame, err)
		}
	}
	for _, data := range [][]byte{{}, {1, 0, 0, 0, 1}, {2, 0, 0, 0, 1, 0}, {1, 0, 0, 0, 0, 0}, {1, 0, 0, 0, 1, 5}} {
		if _, err := kibotos.ParseWSFrame(data); err == nil {
			t.Errorf("accepted invalid frame %x", data)
		}
	}
}

func TestWave3DialWS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	server := newPipeServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workspaces/wks_1/ws" || r.Header.Get("Authorization") != "Bearer kb_test" || r.Header.Get("X-Test") != "upgrade" {
			t.Errorf("unexpected upgrade request: %s %v", r.URL, r.Header)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		done <- func() error {
			types := []string{"process", "process", "fs.watch", "tunnel", "", "", "", "", ""}
			for i, kind := range types {
				var frame struct {
					Type     string         `json:"type"`
					StreamID uint32         `json:"stream_id"`
					Kind     string         `json:"kind"`
					Params   map[string]any `json:"params"`
					Rows     int            `json:"rows"`
					Cols     int            `json:"cols"`
					Signal   string         `json:"signal"`
				}
				if err := wsjson.Read(ctx, conn, &frame); err != nil {
					return err
				}
				if frame.Kind != kind || (i < 4 && (frame.Type != "open" || frame.StreamID != uint32(i+1))) {
					t.Errorf("unexpected control frame %d: %+v", i, frame)
				}
				switch i {
				case 0:
					if frame.Params["action"] != "start" || frame.Params["mode"] != "pty" || frame.Params["rows"] != float64(24) {
						t.Errorf("PTY params: %+v", frame.Params)
					}
				case 1:
					if frame.Params["action"] != "attach" || frame.Params["pid"] != float64(44) || frame.Params["incarnation"] != float64(8) {
						t.Errorf("attach params: %+v", frame.Params)
					}
				case 2:
					if frame.Params["path"] != "/app" || frame.Params["include_entry"] != true {
						t.Errorf("watch params: %+v", frame.Params)
					}
				case 3:
					target, _ := frame.Params["target"].(map[string]any)
					if target["type"] != "unix" || target["path"] != "/tmp/sock" {
						t.Errorf("tunnel params: %+v", frame.Params)
					}
				case 4:
					if frame.Type != "resize" || frame.Rows != 32 || frame.Cols != 120 {
						t.Errorf("resize frame: %+v", frame)
					}
				case 5:
					if frame.Type != "signal" || frame.Signal != "SIGINT" {
						t.Errorf("signal frame: %+v", frame)
					}
				case 6:
					if frame.Type != "close_stdin" {
						t.Errorf("close_stdin frame: %+v", frame)
					}
				case 7:
					if frame.Type != "close" {
						t.Errorf("close frame: %+v", frame)
					}
				case 8:
					if frame.Type != "ping" {
						t.Errorf("ping frame: %+v", frame)
					}
				}
			}
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if kind != websocket.MessageBinary || !bytes.Equal(data, []byte{1, 0, 0, 0, 1, 3, 0xff}) {
				t.Errorf("PTY binary frame = %x (%v)", data, kind)
			}
			if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"fs.event","stream_id":3,"event":"write","path":"/app/a","entry":{"name":"a"}}`)); err != nil {
				return err
			}
			return conn.Write(ctx, websocket.MessageBinary, []byte{1, 0, 0, 0, 4, 4, 0xfe})
		}()
	}))
	defer server.Close()
	c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(server.URL), kibotos.WithHTTPClient(server.Client()))
	ws, err := c.Workspaces.DialWS(ctx, "wks_1", kibotos.WithHeader("X-Test", "upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	for _, call := range []func() error{
		func() error {
			return ws.OpenProcess(ctx, 1, kibotos.WSProcessParams{Action: "start", Mode: "pty", Argv: []string{"sh"}, Rows: 24, Cols: 80})
		},
		func() error {
			return ws.OpenProcess(ctx, 2, kibotos.WSProcessParams{Action: "attach", PID: 44, Incarnation: kibotos.Ptr(uint64(8)), ResumeFrom: map[string]uint64{"pty": 10}})
		},
		func() error { return ws.OpenFSWatch(ctx, 3, kibotos.WSWatchParams{Path: "/app", IncludeEntry: true}) },
		func() error {
			return ws.OpenTunnel(ctx, 4, kibotos.WSTunnelParams{Target: kibotos.WSTunnelTarget{Type: "unix", Path: "/tmp/sock"}})
		},
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	for _, control := range []kibotos.WSControlRequest{
		{Type: "resize", StreamID: 1, Rows: 32, Cols: 120}, {Type: "signal", StreamID: 1, Signal: "SIGINT"},
		{Type: "close_stdin", StreamID: 1}, {Type: "close", StreamID: 2}, {Type: "ping", Timestamp: 123},
	} {
		if err := ws.WriteControl(ctx, control); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.WriteBinary(ctx, kibotos.WSFrame{StreamID: 1, Channel: kibotos.WSChannelPTY, Payload: []byte{0xff}}); err != nil {
		t.Fatal(err)
	}
	msg, err := ws.Read(ctx)
	if err != nil || msg.Control == nil || msg.Control.Type != "fs.event" || msg.Control.StreamID != 3 ||
		msg.Control.Event != "write" || msg.Control.Path != "/app/a" || msg.Control.Entry == nil || msg.Control.Entry.Name != "a" {
		t.Fatalf("event = %+v, error = %v", msg.Control, err)
	}
	msg, err = ws.Read(ctx)
	if err != nil || msg.Binary == nil || msg.Binary.StreamID != 4 || msg.Binary.Channel != kibotos.WSChannelTunnel || !bytes.Equal(msg.Binary.Payload, []byte{0xfe}) {
		t.Fatalf("frame = %+v, error = %v", msg.Binary, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWave3RawWebSocketHelpers(t *testing.T) {
	for _, tc := range []struct {
		name, path, query, protocol string
		dial                        func(context.Context, *kibotos.Client) (*kibotos.BinarySocket, error)
	}{
		{"TCP", "/v1/workspaces/wks_1/tunnels/ws", "family=ipv6&port=8080", "",
			func(ctx context.Context, c *kibotos.Client) (*kibotos.BinarySocket, error) {
				return c.Workspaces.DialTunnel(ctx, "wks_1", kibotos.TunnelOptions{Port: 8080, Family: "ipv6"})
			}},
		{"Unix", "/v1/workspaces/wks_1/tunnels/ws", "unix_path=%2Frun%2Fa+b.sock", "",
			func(ctx context.Context, c *kibotos.Client) (*kibotos.BinarySocket, error) {
				return c.Workspaces.DialTunnel(ctx, "wks_1", kibotos.TunnelOptions{UnixPath: "/run/a b.sock"})
			}},
		{"VNC", "/v1/workspaces/wks_1/desktop/vnc", "", "binary",
			func(ctx context.Context, c *kibotos.Client) (*kibotos.BinarySocket, error) {
				return c.Workspaces.DialDesktopVNC(ctx, "wks_1")
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			server := newPipeServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path || r.URL.RawQuery != tc.query || r.Header.Get("Sec-Websocket-Protocol") != tc.protocol {
					t.Errorf("upgrade = %s %v", r.URL, r.Header)
				}
				conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				kind, data, err := conn.Read(ctx)
				if err != nil || kind != websocket.MessageBinary || !bytes.Equal(data, []byte{0, 1, 2, 0xff}) {
					t.Errorf("raw payload = %x (%v), error = %v", data, kind, err)
					return
				}
				if err := conn.Write(ctx, websocket.MessageBinary, []byte("RFB/raw")); err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close(websocket.StatusNormalClosure, "")
			}))
			defer server.Close()
			c, _ := kibotos.NewClient("kb_test", kibotos.WithBaseURL(server.URL), kibotos.WithHTTPClient(server.Client()))
			s, err := tc.dial(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			defer s.CloseNow()
			if err := s.Write(ctx, []byte{0, 1, 2, 0xff}); err != nil {
				t.Fatal(err)
			}
			data, err := s.Read(ctx)
			if err != nil || string(data) != "RFB/raw" {
				t.Fatalf("raw = %q, error = %v", data, err)
			}
			if _, err := s.Read(ctx); !errors.Is(err, io.EOF) {
				t.Fatalf("socket closure = %v", err)
			}
		})
	}
	t.Run("upgrade error", func(t *testing.T) {
		c := clientForHandler(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Request-Id", "req_vnc")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"desktop_not_enabled","message":"missing pack"}}`)
		})
		_, err := c.Workspaces.DialDesktopVNC(context.Background(), "wks_1")
		var api *kibotos.APIError
		if !errors.As(err, &api) || api.Status != http.StatusConflict || api.Code != "desktop_not_enabled" || api.RequestID != "req_vnc" {
			t.Fatalf("error = %v", err)
		}
	})
}
