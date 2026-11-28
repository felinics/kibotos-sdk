package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) stopWorkspaceAndWait(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, force bool, timeout time.Duration) (*kibotos.Workspace, error) {
	p := a.printer()
	p.Statusf("Stopping workspace %s...", ws.ID)
	var params *kibotos.StopWorkspaceParams
	if force {
		params = &kibotos.StopWorkspaceParams{Force: true}
	}
	stopping, err := client.Workspaces.Stop(ctx, ws.ID, params)
	if err != nil {
		return nil, err
	}
	return waitForWorkspaceStatus(ctx, client, p, stopping, kibotos.WorkspaceStatusStopped, timeout)
}

func (a *App) startWorkspaceAndWait(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, timeout time.Duration) (*kibotos.Workspace, error) {
	return a.startWorkspace(ctx, client, ws, nil, true, timeout)
}

func (a *App) startWorkspace(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, params *kibotos.StartWorkspaceParams, shouldWait bool, timeout time.Duration) (*kibotos.Workspace, error) {
	p := a.printer()
	p.Statusf("Starting workspace %s...", ws.ID)
	starting, err := client.Workspaces.Start(ctx, ws.ID, params)
	if err != nil || !shouldWait {
		return starting, err
	}
	return waitForWorkspaceStatus(ctx, client, p, starting, kibotos.WorkspaceStatusAvailable, timeout)
}

func (a *App) restartWorkspaceAndWait(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, force, shouldWait bool, timeout time.Duration) (*kibotos.Workspace, error) {
	stopped, err := a.stopWorkspaceAndWait(ctx, client, ws, force, timeout)
	if err != nil {
		return nil, err
	}
	return a.startWorkspace(ctx, client, stopped, nil, shouldWait, timeout)
}

func (a *App) withStoppedWorkspace(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, action string, restart bool, timeout time.Duration, mutate func() (*kibotos.Workspace, error)) (*kibotos.Workspace, error) {
	if ws.Status != kibotos.WorkspaceStatusAvailable || (!restart && !a.isInteractive()) {
		return mutate()
	}
	if !restart {
		message := fmt.Sprintf("Workspace %s is running. Stop, %s, and start it again?", resourceLabel(ws.ID, ws.Name), action)
		if err := a.confirmTransition(message); err != nil {
			return nil, err
		}
	}
	if _, err := a.stopWorkspaceAndWait(ctx, client, ws, false, timeout); err != nil {
		return nil, err
	}
	a.printer().Statusf("Applying changes to workspace %s...", ws.ID)
	updated, err := mutate()
	if err != nil {
		return nil, err
	}
	return a.startWorkspaceAndWait(ctx, client, updated, timeout)
}

func (a *App) confirmTransition(message string) error {
	ok, err := a.prompter().Confirm(message, true)
	if err != nil {
		return err
	}
	if !ok {
		return prompt.ErrCancelled
	}
	return nil
}

// workspaceState loads the workspace only when an operation needs more than its ID.
func workspaceState(ctx context.Context, client *kibotos.Client, id string, picked *kibotos.Workspace, needed bool) (*kibotos.Workspace, error) {
	if picked != nil {
		return picked, nil
	}
	if needed {
		return client.Workspaces.Get(ctx, id)
	}
	return &kibotos.Workspace{ID: id}, nil
}

func addTransitionTimeout(cmd *cobra.Command, raw *string) {
	cmd.Flags().StringVar(raw, "wait-timeout", fmt.Sprintf("%ds", defaultWaitTimeoutSeconds), "Maximum duration for each workspace transition (e.g. 5m, 300)")
}

func transitionTimeout(raw string) (time.Duration, error) {
	seconds, err := parseDurationSeconds(raw, "wait-timeout")
	return time.Duration(seconds) * time.Second, err
}

// Restart always waits for the stop even with --no-wait.
func resolveRestartWait(cmd *cobra.Command, wait, noWait bool, raw string) (bool, time.Duration, error) {
	shouldWait, timeout, err := resolveWait(cmd, wait, noWait, raw)
	if shouldWait || err != nil {
		return shouldWait, timeout, err
	}
	timeout, err = transitionTimeout(raw)
	return false, timeout, err
}

func waitForWorkspaceStatus(ctx context.Context, client *kibotos.Client, p *output.Printer, ws *kibotos.Workspace, status kibotos.WorkspaceStatus, timeout time.Duration) (*kibotos.Workspace, error) {
	if ws.Status == status {
		return ws, nil
	}
	id := ws.ID
	stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s to reach %s...", id, status))
	ready, err := client.Workspaces.Wait(ctx, id, kibotos.WaitWorkspaceParams{
		Statuses: []kibotos.WorkspaceStatus{status},
		Timeout:  timeout,
	})
	if err != nil {
		stopSpin(fmt.Sprintf("Workspace %s did not reach %s", id, status), false)
		return nil, err
	}
	stopSpin(fmt.Sprintf("Workspace %s is %s", id, status), true)
	return ready, nil
}
