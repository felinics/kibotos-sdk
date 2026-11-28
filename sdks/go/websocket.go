package kibotos

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/coder/websocket"
)

const (
	WSFrameVersion    = 0x01
	WSFrameHeaderSize = 6
)

// WSChannel identifies the payload type of a binary message on the unified channel.
type WSChannel uint8

const (
	WSChannelStdin WSChannel = iota
	WSChannelStdout
	WSChannelStderr
	WSChannelPTY
	WSChannelTunnel
)

// WSFrame holds the decoded stream ID, channel, and raw payload of one binary message.
type WSFrame struct {
	StreamID uint32
	Channel  WSChannel
	Payload  []byte
}

func (f WSFrame) MarshalBinary() ([]byte, error) {
	if f.StreamID == 0 || f.Channel > WSChannelTunnel {
		return nil, errors.New("kibotos: invalid workspace WebSocket stream ID or channel")
	}
	data := make([]byte, WSFrameHeaderSize+len(f.Payload))
	data[0] = WSFrameVersion
	binary.BigEndian.PutUint32(data[1:5], f.StreamID)
	data[5] = byte(f.Channel)
	copy(data[WSFrameHeaderSize:], f.Payload)
	return data, nil
}

// ParseWSFrame decodes a binary message. The payload shares storage with data.
func ParseWSFrame(data []byte) (WSFrame, error) {
	if len(data) < WSFrameHeaderSize || data[0] != WSFrameVersion {
		return WSFrame{}, errors.New("kibotos: invalid workspace WebSocket frame header")
	}
	frame := WSFrame{binary.BigEndian.Uint32(data[1:5]), WSChannel(data[5]), data[WSFrameHeaderSize:]}
	if frame.StreamID == 0 || frame.Channel > WSChannelTunnel {
		return WSFrame{}, errors.New("kibotos: invalid workspace WebSocket stream ID or channel")
	}
	return frame, nil
}

const (
	WSKindProcess = "process"
	WSKindFSWatch = "fs.watch"
	WSKindTunnel  = "tunnel"
)

// WSProcessParams starts a pipe or PTY process, or reattaches to a running
// process with PID, Incarnation and optional per-channel ResumeFrom offsets.
type WSProcessParams struct {
	Action         string            `json:"action"` // start or attach
	Mode           string            `json:"mode,omitempty"`
	Argv           []string          `json:"argv,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	User           string            `json:"user,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Rows           uint32            `json:"rows,omitempty"`
	Cols           uint32            `json:"cols,omitempty"`
	PID            int               `json:"pid,omitempty"`
	Incarnation    *uint64           `json:"incarnation,omitempty"`
	ResumeFrom     map[string]uint64 `json:"resume_from,omitempty"`
}

type WSWatchParams struct {
	Path         string `json:"path"`
	User         string `json:"user,omitempty"`
	IncludeEntry bool   `json:"include_entry,omitempty"`
}

type WSTunnelTarget struct {
	Type   string `json:"type"` // tcp or unix
	Port   int    `json:"port,omitempty"`
	Family string `json:"family,omitempty"`
	Path   string `json:"path,omitempty"`
}

type WSTunnelParams struct {
	Target WSTunnelTarget `json:"target"`
}

// WSControlRequest is a client text message. Open sends a typed Params value.
// Resize, signal, close_stdin, and close address a stream. Ping uses Timestamp.
type WSControlRequest struct {
	Type      string `json:"type"`
	StreamID  uint32 `json:"stream_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Params    any    `json:"params,omitempty"`
	Rows      uint32 `json:"rows,omitempty"`
	Cols      uint32 `json:"cols,omitempty"`
	Signal    string `json:"signal,omitempty"`
	Timestamp int64  `json:"ts,omitempty"`
}

// WSControlEvent is a server text message such as opened, exited, fs.event,
// closed, error, heartbeat or pong.
type WSControlEvent struct {
	Type              string          `json:"type"`
	StreamID          uint32          `json:"stream_id"`
	Kind              string          `json:"kind,omitempty"`
	Meta              json.RawMessage `json:"meta,omitempty"`
	ExitCode          *int            `json:"exit_code,omitempty"`
	Signal            *int            `json:"signal,omitempty"`
	TerminationReason string          `json:"termination_reason,omitempty"`
	Event             string          `json:"event,omitempty"`
	Path              string          `json:"path,omitempty"`
	Entry             *FileEntry      `json:"entry,omitempty"`
	Reason            string          `json:"reason,omitempty"`
	Code              string          `json:"code,omitempty"`
	Message           string          `json:"message,omitempty"`
	Retryable         bool            `json:"retryable,omitempty"`
	Details           map[string]any  `json:"details,omitempty"`
	Timestamp         int64           `json:"ts,omitempty"`
}

// WSMessage contains either one JSON control event or one binary stream frame.
type WSMessage struct {
	Control *WSControlEvent
	Binary  *WSFrame
}

// WorkspaceWS is a unified workspace connection. Use one reader and keep it
// running to service WebSocket pings. Writes may run concurrently.
type WorkspaceWS struct{ conn *websocket.Conn }

// DialWS opens a unified workspace connection. ctx bounds the handshake;
// pass a lifetime context to Read and Write methods separately.
func (s *WorkspacesService) DialWS(ctx context.Context, workspaceID string, opts ...RequestOption) (*WorkspaceWS, error) {
	conn, err := s.dialSocket(ctx, workspaceID, "/ws", nil, nil, opts)
	if err != nil {
		return nil, err
	}
	return &WorkspaceWS{conn}, nil
}

func (ws *WorkspaceWS) WriteControl(ctx context.Context, control WSControlRequest) error {
	data, err := json.Marshal(control)
	if err != nil {
		return err
	}
	return ws.conn.Write(ctx, websocket.MessageText, data)
}

func (ws *WorkspaceWS) OpenProcess(ctx context.Context, streamID uint32, params WSProcessParams) error {
	return ws.open(ctx, streamID, WSKindProcess, params)
}

func (ws *WorkspaceWS) OpenFSWatch(ctx context.Context, streamID uint32, params WSWatchParams) error {
	return ws.open(ctx, streamID, WSKindFSWatch, params)
}

func (ws *WorkspaceWS) OpenTunnel(ctx context.Context, streamID uint32, params WSTunnelParams) error {
	return ws.open(ctx, streamID, WSKindTunnel, params)
}

func (ws *WorkspaceWS) open(ctx context.Context, streamID uint32, kind string, params any) error {
	if streamID == 0 {
		return errors.New("kibotos: workspace WebSocket stream ID must be positive")
	}
	return ws.WriteControl(ctx, WSControlRequest{Type: "open", Kind: kind, StreamID: streamID, Params: params})
}

func (ws *WorkspaceWS) WriteBinary(ctx context.Context, frame WSFrame) error {
	data, err := frame.MarshalBinary()
	if err != nil {
		return err
	}
	return ws.conn.Write(ctx, websocket.MessageBinary, data)
}

func (ws *WorkspaceWS) Read(ctx context.Context) (WSMessage, error) {
	kind, data, err := ws.conn.Read(ctx)
	if err != nil {
		return WSMessage{}, normalSocketEOF(err)
	}
	if kind == websocket.MessageBinary {
		frame, err := ParseWSFrame(data)
		if err != nil {
			return WSMessage{}, err
		}
		return WSMessage{Binary: &frame}, nil
	}
	var event WSControlEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return WSMessage{}, fmt.Errorf("kibotos: decode workspace WebSocket control: %w", err)
	}
	return WSMessage{Control: &event}, nil
}

func (ws *WorkspaceWS) Close() error {
	return ws.conn.Close(websocket.StatusNormalClosure, "")
}

func (ws *WorkspaceWS) CloseNow() error { return ws.conn.CloseNow() }

// TunnelOptions selects either a guest loopback TCP port or a guest Unix socket.
type TunnelOptions struct {
	Port     int
	Family   string
	UnixPath string
}

// BinarySocket carries unframed tunnel or RFB bytes in standard binary
// WebSocket messages. Use one reader to service incoming messages and pings.
type BinarySocket struct{ conn *websocket.Conn }

func (s *WorkspacesService) DialTunnel(ctx context.Context, workspaceID string, options TunnelOptions, opts ...RequestOption) (*BinarySocket, error) {
	query := url.Values{}
	switch {
	case options.UnixPath != "" && strings.HasPrefix(options.UnixPath, "/") && options.Port == 0 && options.Family == "":
		query.Set("unix_path", options.UnixPath)
	case options.UnixPath == "" && options.Port >= 1 && options.Port <= 65535:
		query.Set("port", strconv.Itoa(options.Port))
		switch options.Family {
		case "":
		case "ipv4", "ipv6":
			query.Set("family", options.Family)
		default:
			return nil, errors.New("kibotos: tunnel family must be ipv4 or ipv6")
		}
	default:
		return nil, errors.New("kibotos: tunnel requires a TCP port (1-65535) or an absolute Unix socket path")
	}
	conn, err := s.dialSocket(ctx, workspaceID, "/tunnels/ws", query, nil, opts)
	if err != nil {
		return nil, err
	}
	return &BinarySocket{conn}, nil
}

func (s *WorkspacesService) DialDesktopVNC(ctx context.Context, workspaceID string, opts ...RequestOption) (*BinarySocket, error) {
	conn, err := s.dialSocket(ctx, workspaceID, "/desktop/vnc", nil, []string{"binary"}, opts)
	if err != nil {
		return nil, err
	}
	return &BinarySocket{conn}, nil
}

func (ws *BinarySocket) Read(ctx context.Context) ([]byte, error) {
	kind, data, err := ws.conn.Read(ctx)
	if err != nil {
		return nil, normalSocketEOF(err)
	}
	if kind != websocket.MessageBinary {
		return nil, errors.New("kibotos: expected a binary WebSocket message")
	}
	return data, nil
}

func (ws *BinarySocket) Write(ctx context.Context, data []byte) error {
	return ws.conn.Write(ctx, websocket.MessageBinary, data)
}

func (ws *BinarySocket) Close() error {
	return ws.conn.Close(websocket.StatusNormalClosure, "")
}

func (ws *BinarySocket) CloseNow() error { return ws.conn.CloseNow() }

func normalSocketEOF(err error) error {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return io.EOF
	default:
		return err
	}
}

func (s *WorkspacesService) dialSocket(ctx context.Context, id, suffix string, query url.Values, protocols []string, opts []RequestOption) (*websocket.Conn, error) {
	path, err := workspaceEndpoint(id, suffix)
	if err != nil {
		return nil, err
	}
	if s.client.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.client.timeout)
		defer cancel()
	}
	headers := make(http.Header)
	s.client.setDefaultHeaders(headers, "")
	for key, values := range collectRequestOptions(opts).headers {
		headers[key] = values
	}
	conn, response, err := websocket.Dial(ctx, s.client.requestURL(path, query), &websocket.DialOptions{
		HTTPClient: s.client.noRedirectClient(), HTTPHeader: headers, Subprotocols: protocols,
	})
	if err != nil {
		if response != nil && response.StatusCode != http.StatusSwitchingProtocols {
			var data []byte
			if response.Body != nil {
				data, _ = io.ReadAll(response.Body)
				_ = response.Body.Close()
			}
			return nil, parseAPIError(response.StatusCode, response.Header, data)
		}
		return nil, err
	}
	conn.SetReadLimit(1 << 20)
	return conn, nil
}
