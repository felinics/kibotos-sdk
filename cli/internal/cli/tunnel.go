package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newTunnelCmd(listenTCP func(context.Context, string, string) (net.Listener, error)) *cobra.Command {
	var target kibotos.TunnelOptions
	var listen string
	cmd := &cobra.Command{
		Use:   "tunnel [workspace] [port]",
		Short: "Forward a local TCP listener to a workspace TCP port or Unix socket",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote := cmd.Flags().Changed("remote")
			unixPath := cmd.Flags().Changed("unix-path")
			if remote && unixPath || remote && (target.Port < 1 || target.Port > 65535) ||
				unixPath && (!strings.HasPrefix(target.UnixPath, "/") || strings.ContainsRune(target.UnixPath, 0)) {
				return usageError("specify either --remote with a port from 1 to 65535 or --unix-path with an absolute guest path")
			}
			if len(args) == 2 {
				if err := validatePort(args[1]); err != nil {
					return usageError("invalid guest port %q: %v", args[1], err)
				}
				port, _ := strconv.Atoi(args[1])
				if unixPath || remote && target.Port != port {
					return usageError("positional port conflicts with --remote or --unix-path")
				}
				target.Port = port
			}
			if target.Family != "" && (unixPath || (target.Family != "ipv4" && target.Family != "ipv6")) {
				return usageError("--family requires a TCP target and must be ipv4 or ipv6")
			}
			_, localPort, err := net.SplitHostPort(listen)
			if err != nil {
				return usageError("invalid --listen address %q: expected host:port", listen)
			}
			if _, err := net.LookupPort("tcp", localPort); err != nil {
				return usageError("invalid --listen port %q", localPort)
			}
			if target.Port == 0 && !unixPath && !a.isInteractive() {
				return usageErrorWithHint("Pass a positional guest port, --remote <port>, or --unix-path </path>.", "tunnel target is required")
			}
			ctx := cmd.Context()
			client, id, err := a.resolveAvailableWorkspace(ctx, firstArg(args))
			if err != nil {
				return err
			}
			if target.Port == 0 && !unixPath {
				port, err := a.prompter().Input("Guest port to forward (1-65535)", "8080", validatePort)
				if err != nil {
					return err
				}
				target.Port, _ = strconv.Atoi(port)
			}
			listener, err := listenTCP(ctx, "tcp", listen)
			if err != nil {
				return err
			}
			defer listener.Close()
			p := a.printer()
			if p.IsJSON() {
				if err := p.JSON(map[string]any{"workspace_id": id, "listen": listener.Addr().String()}); err != nil {
					return err
				}
			} else if p.IsQuiet() {
				if err := p.ID(listener.Addr().String()); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintln(p.Out, "Listening on", listener.Addr()); err != nil {
				return err
			}
			return serveTunnel(ctx, listener, func(ctx context.Context) (*kibotos.BinarySocket, error) {
				return client.Workspaces.DialTunnel(ctx, id, target)
			}, func(err error) {
				_, detail := classifyError(err)
				if p.IsJSON() {
					p.ErrorJSON(jsonErrorEnvelope{Error: detail})
				} else {
					_, _ = fmt.Fprintln(p.Err, "Tunnel connection:", err)
				}
			})
		},
	}
	cmd.Flags().IntVar(&target.Port, "remote", 0, "TCP port on guest loopback")
	cmd.Flags().StringVar(&target.UnixPath, "unix-path", "", "Absolute path to a guest Unix stream socket")
	cmd.Flags().StringVar(&target.Family, "family", "", "Guest loopback family (ipv4 or ipv6)")
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:18080", "Local TCP address to listen on")
	return cmd
}

func serveTunnel(ctx context.Context, listener net.Listener, dial func(context.Context) (*kibotos.BinarySocket, error), report func(error)) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	var workers sync.WaitGroup
	var errorsMu sync.Mutex
	defer func() { cancel(); workers.Wait() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer conn.Close()
			socket, err := dial(ctx)
			if err == nil {
				err = bridgeTunnel(ctx, conn, socket)
			}
			if err != nil && ctx.Err() == nil {
				errorsMu.Lock()
				report(err)
				errorsMu.Unlock()
			}
		}()
	}
}

func bridgeTunnel(ctx context.Context, conn net.Conn, socket *kibotos.BinarySocket) error {
	defer socket.CloseNow()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	outgoing := make(chan error, 1)
	go func() {
		outgoing <- forwardTunnelInput(ctx, conn, socket)
		cancel()
	}()

	var remoteErr error
	for {
		data, err := socket.Read(ctx)
		if err != nil {
			remoteErr = err
			break
		}
		if _, err := conn.Write(data); err != nil {
			remoteErr = err
			break
		}
	}
	cancel()
	_ = conn.Close()
	localErr := <-outgoing
	if tunnelConnectionError(remoteErr) {
		return remoteErr
	}
	if tunnelConnectionError(localErr) {
		return localErr
	}
	return nil
}

func forwardTunnelInput(ctx context.Context, conn net.Conn, socket *kibotos.BinarySocket) error {
	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if writeErr := socket.Write(ctx, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			// A normal WebSocket close signals EOF to the guest.
			return socket.Close()
		}
		if err != nil {
			return err
		}
	}
}

func tunnelConnectionError(err error) bool {
	return err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) &&
		!errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe)
}
