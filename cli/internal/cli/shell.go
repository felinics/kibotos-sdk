package cli

import (
	"fmt"
	"os"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *App) newShellCmd() *cobra.Command {
	var flags kibotos.ExecRequest
	var env []string
	var start bool
	var waitTimeout string
	cmd := &cobra.Command{
		Use:     "shell [workspace]",
		Short:   "Open an interactive shell inside a workspace",
		Example: "  kibo shell\n  kibo shell wks_123 --start --no-input",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := flags
			req.Argv = loginShellArgv()
			var err error
			req.Env, err = parseExecEnv(env)
			if err != nil {
				return err
			}
			for _, flag := range []struct{ name, value string }{{"workdir", req.WorkingDir}, {"user", req.User}} {
				if err := validateNonEmptyFlag(cmd, flag.name, flag.value); err != nil {
					return err
				}
				if strings.ContainsRune(flag.value, 0) {
					return usageError("--%s cannot contain a NUL byte", flag.name)
				}
			}
			timeout, err := transitionTimeout(waitTimeout)
			if err != nil {
				return err
			}
			ref := strings.TrimSpace(firstArg(args))
			if ref == "" && !a.isInteractive() {
				return missingWorkspaceRef()
			}
			if a.printer().IsJSON() {
				return usageErrorWithHint("Use terminal output for shell, or use `kibo exec --json` to run a command.", "shell cannot be combined with JSON output")
			}
			terminal, ok := a.stdin.(*os.File)
			if !ok || !term.IsTerminal(int(terminal.Fd())) {
				return usageErrorWithHint("Run shell with terminal stdin, or use `kibo exec` to run a command.", "shell requires a terminal on stdin")
			}
			if !supportsPTY {
				return usageErrorWithHint("Run shell from a Unix terminal.", "shell requires Unix PTY support")
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			const empty = "no running or stopped workspaces found"
			const title = "Select a workspace for the shell:"
			var id string
			var ws *kibotos.Workspace
			if ref == "" {
				id, ws, err = a.pickWorkspace(ctx, client, stoppedOrAvailableWorkspace, empty, title, compareShellWorkspaces)
			} else {
				id, ws, err = a.resolveWorkspace(ctx, client, ref, stoppedOrAvailableWorkspace, empty, title)
			}
			if err != nil {
				return err
			}
			ws, err = workspaceState(ctx, client, id, ws, true)
			if err != nil {
				return err
			}
			switch ws.Status {
			case kibotos.WorkspaceStatusStopped:
				if !start {
					if !a.isInteractive() {
						return &CLIError{ExitCode: ExitError, Code: "invalid_state", Message: "workspace is stopped", Hint: "Pass --start to start the workspace and open a shell."}
					}
					message := fmt.Sprintf("Workspace %s is stopped. Start it and open shell?", resourceLabel(id, ws.Name))
					if err := a.confirmTransition(message); err != nil {
						return err
					}
				}
				if _, err := a.startWorkspaceAndWait(ctx, client, ws, timeout); err != nil {
					return err
				}
			case kibotos.WorkspaceStatusAvailable:
			default:
				return &CLIError{ExitCode: ExitError, Code: "invalid_state", Message: fmt.Sprintf("workspace %s must be available to open a shell (status: %s)", id, ws.Status)}
			}
			return a.runPTY(ctx, client, id, req, terminal)
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.WorkingDir, "workdir", "", "Working directory inside the workspace")
	f.StringVar(&flags.User, "user", "", "Run as this workspace user")
	f.StringArrayVar(&env, "env", nil, "Environment variable (KEY=VALUE, repeatable)")
	f.BoolVar(&start, "start", false, "Start the workspace if it is stopped")
	addTransitionTimeout(cmd, &waitTimeout)
	return cmd
}

func compareShellWorkspaces(left, right kibotos.Workspace) int {
	if left.Status == right.Status {
		return 0
	}
	if isAvailableWorkspace(left) {
		return -1
	}
	return 1
}
