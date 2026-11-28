package kibotos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Proxy forwards an HTTP request to a loopback port in an available workspace.
// Pass an unescaped path and separate query parameters. An empty path targets
// the service root. Use WithHeader to set request headers such as Content-Type.
// Proxy returns the raw response for every HTTP status without SDK retries or
// following redirects. The caller must close the response body.
func (s *WorkspacesService) Proxy(ctx context.Context, workspaceID string, port int, method, path string, query url.Values, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	if port < 1 || port > 65535 {
		return nil, errors.New("kibotos: proxy port must be between 1 and 65535")
	}
	suffix := "/proxy/" + strconv.Itoa(port)
	if path != "" {
		suffix += (&url.URL{Path: "/" + strings.TrimPrefix(path, "/")}).EscapedPath()
	}
	endpoint, err := workspaceEndpoint(workspaceID, suffix)
	if err != nil {
		return nil, err
	}
	req, err := s.client.newDataRequest(ctx, method, endpoint, query, body, "", "", nil, opts)
	if err != nil {
		return nil, err
	}
	return s.client.noRedirectClient().Do(req)
}

// ExecRequest runs Argv directly, with no implicit shell. Stdin, if present,
// is written and then closed.
type ExecRequest struct {
	Argv           []string          `json:"argv"`
	Env            map[string]string `json:"env,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	User           string            `json:"user,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Stdin          *string           `json:"stdin,omitempty"`
	MaxOutputBytes int               `json:"max_output_bytes,omitempty"`
}

type ProcessExit struct {
	ExitCode          *int   `json:"exit_code"`
	Signal            *int   `json:"signal"`
	TerminationReason string `json:"termination_reason"`
}

type ExecResponse struct {
	ProcessExit
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms"`
}

// Exec runs a command once and returns its output and exit status. A nonzero
// process exit code is part of the result. Exec does not replay requests.
func (s *WorkspacesService) Exec(ctx context.Context, workspaceID string, body ExecRequest, opts ...RequestOption) (*ExecResponse, error) {
	if timeout := s.client.timeout; timeout > 0 {
		seconds := body.TimeoutSeconds
		if seconds <= 0 {
			seconds = 30
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout+time.Duration(min(seconds, 600))*time.Second)
		defer cancel()
	}
	path, payload, err := workspaceJSONBody(workspaceID, "/exec", body)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodPost, path, nil, payload, "application/json", "application/json", nil, opts)
	if err != nil {
		return nil, err
	}
	return decodeDataResponse[ExecResponse](resp)
}

// ExecEvent holds a started, stdout, stderr, heartbeat, exited, or error event.
type ExecEvent struct {
	Type string `json:"type"`
	ProcessExit
	PID         int             `json:"pid,omitempty"`
	Incarnation uint64          `json:"incarnation,omitempty"`
	Data        string          `json:"data,omitempty"`
	DurationMS  int64           `json:"duration_ms,omitempty"`
	Error       *APIError       `json:"error,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// ExecStream calls handler for each SSE event, including the final exited or error
// event. A handler error stops the stream. If the server sends an error event,
// ExecStream returns its APIError. The context controls the lifetime of the stream.
func (s *WorkspacesService) ExecStream(ctx context.Context, workspaceID string, body ExecRequest, handler func(ExecEvent) error, opts ...RequestOption) error {
	if handler == nil {
		return errors.New("kibotos: exec event handler is required")
	}
	path, payload, err := workspaceJSONBody(workspaceID, "/exec", body)
	if err != nil {
		return err
	}
	resp, err := s.client.openDataRequest(ctx, http.MethodPost, path, nil, payload, "application/json", "text/event-stream", nil, opts)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "text/event-stream" {
		return fmt.Errorf("kibotos: unexpected exec stream content type %q", resp.Header.Get("Content-Type"))
	}
	return consumeExecEvents(resp.Body, handler)
}

func consumeExecEvents(body io.Reader, handler func(ExecEvent) error) error {
	scanner := bufio.NewScanner(body)
	// A single event is bounded. The total stream has no size limit.
	const maxEvent = 1 << 20
	scanner.Buffer(make([]byte, 4096), maxEvent)
	scanner.Split(splitSSELine)
	var eventType string
	var data strings.Builder
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			firstLine = false
		}
		if line == "" {
			if data.Len() != 0 {
				raw := strings.TrimSuffix(data.String(), "\n")
				event := ExecEvent{Raw: json.RawMessage(raw)}
				if err := json.Unmarshal(event.Raw, &event); err != nil {
					return fmt.Errorf("kibotos: decode exec event %q: %w", eventType, err)
				}
				event.Type = eventType
				if eventType == "error" {
					event.Error = new(APIError)
					if err := json.Unmarshal(event.Raw, event.Error); err != nil {
						return fmt.Errorf("kibotos: decode exec error: %w", err)
					}
				}
				if err := handler(event); err != nil {
					return err
				}
				if event.Error != nil {
					return event.Error
				}
				if eventType == "exited" {
					return nil
				}
			}
			eventType = ""
			data = strings.Builder{}
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventType = value
		case "data":
			if data.Len()+len(value)+1 > maxEvent {
				return errors.New("kibotos: exec event exceeds 1 MiB")
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("kibotos: exec stream ended before a terminal event: %w", io.ErrUnexpectedEOF)
}

func splitSSELine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		end := i + 1
		if data[i] == '\r' {
			if end == len(data) && !atEOF {
				return 0, nil, nil
			}
			if end < len(data) && data[end] == '\n' {
				end++
			}
		}
		return end, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func workspaceJSONBody(id, suffix string, body any) (string, io.Reader, error) {
	path, err := workspaceEndpoint(id, suffix)
	if err != nil {
		return "", nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", nil, fmt.Errorf("kibotos: marshal request body: %w", err)
	}
	return path, bytes.NewReader(payload), nil
}

// openDataRequest opens a single response. The caller owns a successful body.
func (c *Client) openDataRequest(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType, accept string, headers http.Header, opts []RequestOption) (*http.Response, error) {
	req, err := c.newDataRequest(ctx, method, path, query, body, contentType, accept, headers, opts)
	if err != nil {
		return nil, err
	}
	resp, err := c.noRedirectClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 ||
		method == http.MethodGet && resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return nil, parseAPIError(resp.StatusCode, resp.Header, data)
}

func (c *Client) newDataRequest(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType, accept string, headers http.Header, opts []RequestOption) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.requestURL(path, query), body)
	if err != nil {
		return nil, fmt.Errorf("kibotos: build request: %w", err)
	}
	c.setDefaultHeaders(req.Header, accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		req.Header[key] = values
	}
	for key, values := range collectRequestOptions(opts).headers {
		req.Header[key] = values
	}
	// These requests can have side effects or carry a nonrewindable body.
	req.Header.Del("Idempotency-Key")
	req.Header.Del("X-Idempotency-Key")
	req.GetBody = nil
	return req, nil
}

func (c *Client) noRedirectClient() *http.Client {
	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func decodeDataResponse[T any](resp *http.Response) (*T, error) {
	defer resp.Body.Close()
	result := new(T)
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, fmt.Errorf("kibotos: decode response: %w", err)
	}
	return result, nil
}
