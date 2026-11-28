package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type processStatus int

func (s processStatus) Error() string { return fmt.Sprintf("process exited with status %d", s) }

func processResult(result kibotos.ProcessExit) error {
	if result.TerminationReason == "timed_out" {
		return processStatus(124)
	}
	if result.Signal != nil && *result.Signal > 0 && *result.Signal < 128 {
		return processStatus(128 + *result.Signal)
	}
	if result.ExitCode != nil && *result.ExitCode > 0 && *result.ExitCode <= 255 {
		return processStatus(*result.ExitCode)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 ||
		(result.TerminationReason != "" && result.TerminationReason != "exited") {
		return processStatus(1)
	}
	return nil
}

func (a *App) newExecCmd() *cobra.Command {
	var req kibotos.ExecRequest
	var env []string
	var stdin string
	var stream, interactive, tty bool
	cmd := &cobra.Command{
		Use:   "exec <workspace> [flags] -- <command> [args...]",
		Short: "Run a command inside a workspace",
		Args: func(cmd *cobra.Command, args []string) error {
			if cmd.ArgsLenAtDash() != 1 || len(args) < 2 || args[1] == "" {
				return usageError("expected a workspace and a command after --")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Argv = args[1:]
			var err error
			req.Env, err = parseExecEnv(env)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("timeout") && req.TimeoutSeconds <= 0 {
				return usageError("--timeout must be positive")
			}
			if !stream && !tty && req.TimeoutSeconds > 600 {
				return usageError("timeouts over 600 seconds require --stream or -it")
			}
			if interactive != tty {
				return usageError("use -it to open an interactive terminal")
			}
			var terminal *os.File
			if tty {
				if stream || cmd.Flags().Changed("stdin") || a.printer().IsJSON() {
					return usageError("-it requires terminal output and cannot be combined with --stream, --stdin, or --json")
				}
				file, ok := a.stdin.(*os.File)
				if !ok || !term.IsTerminal(int(file.Fd())) {
					return usageError("-it requires a terminal on stdin")
				}
				terminal = file
			}
			if cmd.Flags().Changed("stdin") {
				if stdin == "-" {
					// Leave room for JSON encoding and the other request fields.
					const maxStdin = 3 << 20
					data, err := io.ReadAll(io.LimitReader(a.stdin, maxStdin+1))
					if err != nil {
						return err
					}
					if len(data) > maxStdin {
						return usageError("--stdin - exceeds the 3 MiB input limit")
					}
					stdin = string(data)
				}
				req.Stdin = &stdin
				payload, err := json.Marshal(req)
				if err != nil {
					return err
				}
				if len(payload) > 4<<20 {
					return usageError("exec JSON request with --stdin exceeds the 4 MiB API limit")
				}
			}
			client, id, err := a.workspaceDataClient(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if tty {
				return a.runPTY(ctx, client, id, req, terminal)
			}
			if stream {
				return a.streamExec(ctx, client, id, req)
			}
			result, err := client.Workspaces.Exec(ctx, id, req)
			if err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				if err := p.JSON(result); err != nil {
					return err
				}
			} else {
				if _, err := io.WriteString(p.Out, result.Stdout); err != nil {
					return err
				}
				if _, err := io.WriteString(p.Err, result.Stderr); err != nil {
					return err
				}
				if result.Truncated {
					p.Warnf("command output was truncated")
				}
			}
			return processResult(result.ProcessExit)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&req.WorkingDir, "workdir", "", "Working directory inside the workspace")
	flags.StringArrayVar(&env, "env", nil, "Environment variable (KEY=VALUE, repeatable)")
	flags.IntVar(&req.TimeoutSeconds, "timeout", 0, "Process timeout in seconds (API default for noninteractive exec: 30)")
	flags.StringVar(&req.User, "user", "", "Run as this workspace user")
	flags.StringVar(&stdin, "stdin", "", "Input text, or - to read local stdin")
	flags.BoolVar(&stream, "stream", false, "Stream output as it arrives (newline-delimited events with --json)")
	flags.BoolVarP(&interactive, "interactive", "i", false, "Forward terminal input (use with -t)")
	flags.BoolVarP(&tty, "tty", "t", false, "Allocate a remote PTY (use with -i)")
	return cmd
}

func parseExecEnv(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" || strings.ContainsAny(name, " \t\r\n\x00") || strings.ContainsRune(value, 0) {
			return nil, usageError("invalid --env entry: expected KEY=VALUE")
		}
		env[name] = value
	}
	return env, nil
}

func (a *App) streamExec(ctx context.Context, client *kibotos.Client, id string, req kibotos.ExecRequest) error {
	jsonOutput := a.printer().IsJSON()
	encoder := json.NewEncoder(a.stdout)
	var result kibotos.ProcessExit
	err := client.Workspaces.ExecStream(ctx, id, req, func(event kibotos.ExecEvent) error {
		if event.Type == "exited" {
			result = event.ProcessExit
		}
		if jsonOutput {
			// The root command prints the structured API error once.
			if event.Type == "error" {
				return nil
			}
			return encoder.Encode(struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}{event.Type, event.Raw})
		}
		var err error
		switch event.Type {
		case "stdout":
			_, err = io.WriteString(a.stdout, event.Data)
		case "stderr":
			_, err = io.WriteString(a.stderr, event.Data)
		}
		return err
	})
	if err != nil {
		return err
	}
	return processResult(result)
}
