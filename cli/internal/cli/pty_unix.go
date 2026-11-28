//go:build unix

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const supportsPTY = true

func (a *App) runPTY(ctx context.Context, client *kibotos.Client, id string, req kibotos.ExecRequest, input *os.File) error {
	const streamID = 1
	fd := int(input.Fd())
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)

	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return fmt.Errorf("read terminal size: %w", err)
	}
	socket, err := client.Workspaces.DialWS(ctx, id)
	if err != nil {
		return err
	}
	defer socket.CloseNow()
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set terminal raw mode: %w", err)
	}
	defer term.Restore(fd, state)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	err = socket.OpenProcess(ctx, streamID, kibotos.WSProcessParams{
		Action: "start", Mode: "pty", Argv: req.Argv, Env: req.Env, WorkingDir: req.WorkingDir,
		User: req.User, TimeoutSeconds: req.TimeoutSeconds, Rows: uint32(rows), Cols: uint32(cols),
	})
	if err != nil {
		return err
	}

	var started bool
	for {
		message, err := socket.Read(ctx)
		if err != nil {
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("terminal disconnected before process exit: %w", io.ErrUnexpectedEOF)
			}
			return err
		}
		if frame := message.Binary; frame != nil {
			if frame.StreamID == streamID && frame.Channel == kibotos.WSChannelPTY {
				if _, err := a.stdout.Write(frame.Payload); err != nil {
					return err
				}
			}
			continue
		}
		event := message.Control
		if event.StreamID != streamID && event.StreamID != 0 {
			continue
		}
		switch event.Type {
		case "opened":
			if !started && event.StreamID == streamID {
				started = true
				done := make(chan struct{})
				go func() {
					defer close(done)
					if err := copyPTYInput(ctx, socket, streamID, fd); err != nil {
						cancel(err)
					}
				}()
				resizeDone := make(chan struct{})
				go func() {
					defer close(resizeDone)
					if err := resizePTY(ctx, socket, streamID, fd, resize); err != nil {
						cancel(err)
					}
				}()
				defer func() {
					cancel(nil)
					<-done
					<-resizeDone
				}()
			}
		case "error":
			err := &kibotos.APIError{Code: event.Code, Message: event.Message, Retryable: event.Retryable, Details: event.Details}
			if !started {
				return err
			}
			a.printer().Warnf("%v", err)
		case "exited":
			return processResult(kibotos.ProcessExit{ExitCode: event.ExitCode, Signal: event.Signal, TerminationReason: event.TerminationReason})
		case "closed":
			return fmt.Errorf("terminal stream closed before process exit (%s)", event.Reason)
		}
	}
}

func copyPTYInput(ctx context.Context, socket *kibotos.WorkspaceWS, id uint32, fd int) error {
	buffer := make([]byte, 4096)
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		// Poll keeps the terminal read interruptible without closing stdin.
		ready, err := unix.Poll(poll, 100)
		if errors.Is(err, unix.EINTR) || (err == nil && ready == 0) {
			continue
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			break
		}
		count, err := unix.Read(fd, buffer)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return err
		}
		if count == 0 {
			return socket.WriteControl(ctx, kibotos.WSControlRequest{Type: "close_stdin", StreamID: id})
		}
		if err := socket.WriteBinary(ctx, kibotos.WSFrame{StreamID: id, Channel: kibotos.WSChannelPTY, Payload: buffer[:count]}); err != nil {
			return err
		}
	}
	return nil
}

func resizePTY(ctx context.Context, socket *kibotos.WorkspaceWS, id uint32, fd int, resize <-chan os.Signal) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-resize:
			cols, rows, err := term.GetSize(fd)
			if err != nil {
				return err
			}
			if rows > 0 && cols > 0 {
				if err := socket.WriteControl(ctx, kibotos.WSControlRequest{
					Type: "resize", StreamID: id, Rows: uint32(rows), Cols: uint32(cols),
				}); err != nil {
					return err
				}
			}
		}
	}
}
