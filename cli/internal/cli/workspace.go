package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

func (a *App) newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws", "workspaces"},
		Short:   "Create and manage Kibotos workspaces",
	}

	cmd.AddCommand(
		a.newWorkspaceListCmd(),
		a.newWorkspaceGetCmd(),
		a.newWorkspaceCreateCmd(),
		a.newWorkspaceUpdateCmd(),
		a.newWorkspaceDeleteCmd(),
		a.newWorkspaceStartCmd(),
		a.newWorkspaceStopCmd(),
		a.newWorkspaceTimeoutCmd(),
		a.newWorkspaceNetworkCmd(),
		a.newWorkspaceVolumeCmd(),
		a.newWorkspaceWaitCmd(),
	)
	return cmd
}

func (a *App) newWorkspaceListCmd() *cobra.Command {
	var (
		status    string
		zone      string
		labels    []string
		limit     int
		pageToken string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsedLabels, err := parseLabels(labels)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("limit") && limit <= 0 {
				return usageError("--limit must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			p := a.printer()

			params := &kibotos.WorkspaceListParams{
				Status:    kibotos.WorkspaceStatus(strings.TrimSpace(status)),
				Zone:      strings.TrimSpace(zone),
				Labels:    parsedLabels,
				Limit:     limit,
				PageToken: pageToken,
			}

			var page *kibotos.MultiZonePage[kibotos.Workspace]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Workspaces.List(ctx, params)
				if err != nil {
					return err
				}
			} else {
				page, err = collectAllWorkspaces(ctx, client, params)
				if err != nil {
					return err
				}
			}

			if p.IsJSON() {
				return p.JSON(page)
			}
			if len(page.UnavailableZones) > 0 {
				p.Warnf("unavailable zones: %s", strings.Join(page.UnavailableZones, ", "))
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No workspaces found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, ws := range page.Data {
				volStr := "-"
				if ws.Volume != nil {
					volStr = ws.Volume.ID
				}
				timeoutStr := "-"
				if ws.Timeout != nil {
					timeoutStr = fmt.Sprintf("%s (%s)", output.FormatTime(ws.Timeout.ExpiresAt), ws.Timeout.Action)
				}
				tplStr := ws.Template.ID
				if ws.Template.Revision != "" {
					tplStr += "@" + ws.Template.Revision
				}
				rows = append(rows, []string{
					ws.ID,
					output.EmptyDash(ws.Name),
					ws.Zone,
					tplStr,
					string(ws.Status),
					string(ws.Network.Mode),
					volStr,
					timeoutStr,
				})
			}
			return p.Table([]string{"ID", "NAME", "ZONE", "TEMPLATE", "STATUS", "NETWORK", "VOLUME", "TIMEOUT"}, rows)
		},
	}

	f := cmd.Flags()
	f.StringVar(&status, "status", "", "Filter by workspace status")
	f.StringVar(&zone, "zone", "", "Filter by zone ID")
	f.StringArrayVar(&labels, "label", nil, "Filter by label (key=value, repeatable)")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newWorkspaceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details of a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
}

func (a *App) newWorkspaceCreateCmd() *cobra.Command {
	var (
		templateID    string
		revision      string
		name          string
		zone          string
		vcpus         int
		memoryMiB     int
		diskMiB       int
		networkMode   string
		volumeID      string
		mountPath     string
		deleteVolume  bool
		timeoutSec    int
		timeoutAction string
		labels        []string
		wait          bool
		waitTimeout   int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(templateID) == "" {
				return usageError("--template is required")
			}

			params := kibotos.CreateWorkspaceParams{
				Name: strings.TrimSpace(name),
				Zone: strings.TrimSpace(zone),
				Template: kibotos.WorkspaceTemplateSpec{
					ID:       strings.TrimSpace(templateID),
					Revision: strings.TrimSpace(revision),
				},
			}

			if cmd.Flags().Changed("vcpus") || cmd.Flags().Changed("memory") || cmd.Flags().Changed("disk") {
				if cmd.Flags().Changed("vcpus") && vcpus <= 0 {
					return usageError("--vcpus must be greater than zero")
				}
				if cmd.Flags().Changed("memory") && memoryMiB <= 0 {
					return usageError("--memory must be greater than zero")
				}
				if cmd.Flags().Changed("disk") && diskMiB <= 0 {
					return usageError("--disk must be greater than zero")
				}
				params.Resources = &kibotos.WorkspaceResourcesSpec{
					VCPUs:     vcpus,
					MemoryMiB: memoryMiB,
					DiskMiB:   diskMiB,
				}
			}

			if cmd.Flags().Changed("network") {
				mode, err := parseNetworkMode(networkMode)
				if err != nil {
					return err
				}
				params.Network = &kibotos.WorkspaceNetworkSpec{Mode: mode}
			}

			if strings.TrimSpace(volumeID) != "" {
				params.Volume = &kibotos.WorkspaceVolumeSpec{
					ID:                  strings.TrimSpace(volumeID),
					MountPath:           strings.TrimSpace(mountPath),
					DeleteWithWorkspace: deleteVolume,
				}
			} else if cmd.Flags().Changed("mount-path") || cmd.Flags().Changed("delete-volume") {
				return usageError("--mount-path and --delete-volume require --volume")
			}

			if cmd.Flags().Changed("timeout") || cmd.Flags().Changed("timeout-action") {
				if !cmd.Flags().Changed("timeout") || timeoutSec <= 0 {
					return usageError("--timeout must be a positive number of seconds")
				}
				act, err := parseTimeoutAction(timeoutAction)
				if err != nil {
					return err
				}
				params.Timeout = &kibotos.TimeoutSpec{
					Seconds: timeoutSec,
					Action:  act,
				}
			}

			parsedLabels, err := parseLabels(labels)
			if err != nil {
				return err
			}
			if len(parsedLabels) > 0 {
				params.Labels = parsedLabels
			}

			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			ws, err := client.Workspaces.Create(ctx, params)
			if err != nil {
				return err
			}

			if wait && ws.Status != kibotos.WorkspaceStatusAvailable {
				a.printer().Statusf("Waiting for workspace %s to reach available...", ws.ID)
				ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusAvailable},
					Timeout:  time.Duration(waitTimeout) * time.Second,
				})
				if err != nil {
					return err
				}
			}

			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringVar(&templateID, "template", "", "Template ID (required)")
	f.StringVar(&revision, "revision", "", "Specific template revision ID")
	f.StringVar(&name, "name", "", "Human-readable workspace name")
	f.StringVar(&zone, "zone", "", "Target zone ID (defaults to organization default zone)")
	f.IntVar(&vcpus, "vcpus", 0, "vCPU count override")
	f.IntVar(&memoryMiB, "memory", 0, "Memory allocation in MiB")
	f.IntVar(&diskMiB, "disk", 0, "Root disk size in MiB")
	f.StringVar(&networkMode, "network", "", "Outbound network mode: public_internet or deny_all")
	f.StringVar(&volumeID, "volume", "", "Persistent volume ID to attach at creation")
	f.StringVar(&mountPath, "mount-path", "", "Mount path for the attached volume (e.g. /data)")
	f.BoolVar(&deleteVolume, "delete-volume", false, "Delete the attached volume when this workspace is deleted")
	f.IntVar(&timeoutSec, "timeout", 0, "Auto-timeout duration in seconds")
	f.StringVar(&timeoutAction, "timeout-action", "", "Action on timeout expiration: stop or delete")
	f.StringArrayVar(&labels, "label", nil, "Workspace label (key=value, repeatable)")
	f.BoolVar(&wait, "wait", false, "Wait until the workspace reaches available")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newWorkspaceUpdateCmd() *cobra.Command {
	var (
		name        string
		clearName   bool
		labels      []string
		unsetLabels []string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a workspace's name or labels",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nameChanged := cmd.Flags().Changed("name")
			if nameChanged && clearName {
				return usageError("--name and --clear-name cannot be used together")
			}
			parsedLabels, err := parseUpdateLabels(labels, unsetLabels)
			if err != nil {
				return err
			}
			if !nameChanged && !clearName && len(parsedLabels) == 0 {
				return usageError("specify at least one of --name, --clear-name, --label, or --unset-label")
			}

			params := kibotos.UpdateWorkspaceParams{
				Labels: parsedLabels,
			}
			if nameChanged {
				params.Name = kibotos.Some(name)
			} else if clearName {
				params.Name = kibotos.Null[string]()
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.Update(cmd.Context(), args[0], params)
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringVar(&name, "name", "", "Set workspace name")
	f.BoolVar(&clearName, "clear-name", false, "Clear the workspace name (set to null)")
	f.StringArrayVar(&labels, "label", nil, "Set a label (key=value, repeatable)")
	f.StringArrayVar(&unsetLabels, "unset-label", nil, "Remove a label by key (repeatable)")
	return cmd
}

func (a *App) newWorkspaceDeleteCmd() *cobra.Command {
	var (
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			ws, err := client.Workspaces.Delete(ctx, args[0])
			if err != nil {
				return err
			}

			if wait {
				a.printer().Statusf("Waiting for workspace %s to be deleted...", ws.ID)
				ws, err = a.waitWorkspaceDeleted(ctx, client, ws, time.Duration(waitTimeout)*time.Second)
				if err != nil {
					return err
				}
			}
			return a.renderWorkspace(ws)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", false, "Wait until workspace deletion completes")
	cmd.Flags().IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newWorkspaceStartCmd() *cobra.Command {
	var (
		timeoutSec    int
		timeoutAction string
		wait          bool
		waitTimeout   int
	)

	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Start a stopped workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var params *kibotos.StartWorkspaceParams
			if cmd.Flags().Changed("timeout") || cmd.Flags().Changed("timeout-action") {
				if !cmd.Flags().Changed("timeout") || timeoutSec <= 0 {
					return usageError("--timeout must be a positive number of seconds")
				}
				act, err := parseTimeoutAction(timeoutAction)
				if err != nil {
					return err
				}
				params = &kibotos.StartWorkspaceParams{
					Timeout: &kibotos.TimeoutSpec{
						Seconds: timeoutSec,
						Action:  act,
					},
				}
			}
			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			ws, err := client.Workspaces.Start(ctx, args[0], params)
			if err != nil {
				return err
			}

			if wait && ws.Status != kibotos.WorkspaceStatusAvailable {
				a.printer().Statusf("Waiting for workspace %s to reach available...", ws.ID)
				ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusAvailable},
					Timeout:  time.Duration(waitTimeout) * time.Second,
				})
				if err != nil {
					return err
				}
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.IntVar(&timeoutSec, "timeout", 0, "Auto-timeout duration in seconds")
	f.StringVar(&timeoutAction, "timeout-action", "", "Action on timeout expiration: stop or delete")
	f.BoolVar(&wait, "wait", false, "Wait until the workspace reaches available")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newWorkspaceStopCmd() *cobra.Command {
	var (
		force       bool
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop an available workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			var params *kibotos.StopWorkspaceParams
			if force {
				params = &kibotos.StopWorkspaceParams{Force: true}
			}

			ws, err := client.Workspaces.Stop(ctx, args[0], params)
			if err != nil {
				return err
			}

			if wait && ws.Status != kibotos.WorkspaceStatusStopped {
				a.printer().Statusf("Waiting for workspace %s to reach stopped...", ws.ID)
				ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusStopped},
					Timeout:  time.Duration(waitTimeout) * time.Second,
				})
				if err != nil {
					return err
				}
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "Force immediate power-off instead of graceful shutdown")
	f.BoolVar(&wait, "wait", false, "Wait until the workspace reaches stopped")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newWorkspaceTimeoutCmd() *cobra.Command {
	var (
		seconds int
		action  string
	)

	cmd := &cobra.Command{
		Use:   "timeout <id>",
		Short: "Set or extend a workspace's auto-timeout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("seconds") || seconds <= 0 {
				return usageError("--seconds is required and must be greater than zero")
			}
			act, err := parseTimeoutAction(action)
			if err != nil {
				return err
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.SetTimeout(cmd.Context(), args[0], kibotos.SetWorkspaceTimeoutParams{
				Seconds: seconds,
				Action:  act,
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.IntVar(&seconds, "seconds", 0, "New timeout duration from now in seconds (required)")
	f.StringVar(&action, "action", "", "Timeout action: stop or delete")
	return cmd
}

func (a *App) newWorkspaceNetworkCmd() *cobra.Command {
	var (
		mode        string
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "network <id>",
		Short: "Update a workspace's outbound network policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("mode") {
				return usageError("--mode is required (public_internet or deny_all)")
			}
			parsedMode, err := parseNetworkMode(mode)
			if err != nil {
				return err
			}
			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			ws, err := client.Workspaces.SetNetwork(ctx, args[0], kibotos.SetWorkspaceNetworkParams{
				Mode: parsedMode,
			})
			if err != nil {
				return err
			}

			if wait && !ws.Network.Applied {
				a.printer().Statusf("Waiting for network policy %q to apply on workspace %s...", parsedMode, ws.ID)
				ws, err = a.waitWorkspaceNetworkApplied(ctx, client, ws.ID, parsedMode, time.Duration(waitTimeout)*time.Second)
				if err != nil {
					return err
				}
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringVar(&mode, "mode", "", "Outbound network mode: public_internet or deny_all (required)")
	f.BoolVar(&wait, "wait", false, "Wait until the network policy is applied")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newWorkspaceVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Attach or detach a persistent volume on a workspace",
	}

	var (
		volumeID            string
		mountPath           string
		deleteWithWorkspace bool
	)

	attachCmd := &cobra.Command{
		Use:   "attach <workspace_id>",
		Short: "Attach a persistent volume to a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(volumeID) == "" {
				return usageError("--volume is required")
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.AttachVolume(cmd.Context(), args[0], kibotos.AttachVolumeParams{
				ID:                  strings.TrimSpace(volumeID),
				MountPath:           strings.TrimSpace(mountPath),
				DeleteWithWorkspace: deleteWithWorkspace,
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
	af := attachCmd.Flags()
	af.StringVar(&volumeID, "volume", "", "Persistent volume ID to attach (required)")
	af.StringVar(&mountPath, "mount-path", "", "Mount path inside the workspace (e.g. /data)")
	af.BoolVar(&deleteWithWorkspace, "delete-with-workspace", false, "Delete the volume when the workspace is deleted")

	detachCmd := &cobra.Command{
		Use:   "detach <workspace_id>",
		Short: "Detach the persistent volume from a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.DetachVolume(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	cmd.AddCommand(attachCmd, detachCmd)
	return cmd
}

func (a *App) newWorkspaceWaitCmd() *cobra.Command {
	var (
		statuses []string
		timeout  int
	)

	cmd := &cobra.Command{
		Use:   "wait <id>",
		Short: "Wait until a workspace reaches one of the target statuses",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(statuses) == 0 {
				return usageError("--status is required (e.g. --status available,failed)")
			}
			if timeout <= 0 {
				return usageError("--timeout must be greater than zero")
			}
			parsed := make([]kibotos.WorkspaceStatus, 0, len(statuses))
			for _, raw := range statuses {
				s := strings.TrimSpace(raw)
				if s == "" {
					continue
				}
				parsed = append(parsed, kibotos.WorkspaceStatus(s))
			}
			if len(parsed) == 0 {
				return usageError("--status must include at least one non-empty status")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.Wait(cmd.Context(), args[0], kibotos.WaitWorkspaceParams{
				Statuses: parsed,
				Timeout:  time.Duration(timeout) * time.Second,
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&statuses, "status", nil, "Target status(es), comma-separated or repeatable (required)")
	f.IntVar(&timeout, "timeout", defaultWaitCmdTimeoutSeconds, "Maximum seconds to wait")
	return cmd
}

func (a *App) renderWorkspace(ws *kibotos.Workspace) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(ws)
	}
	tpl := ws.Template.ID
	if ws.Template.Revision != "" {
		tpl += "@" + ws.Template.Revision
	}
	resStr := fmt.Sprintf("%d vCPU / %d MiB RAM / %d MiB disk",
		ws.Resources.VCPUs, ws.Resources.MemoryMiB, ws.Resources.DiskMiB)
	netStr := fmt.Sprintf("%s (applied=%t)", ws.Network.Mode, ws.Network.Applied)
	volStr := "-"
	if ws.Volume != nil {
		volStr = fmt.Sprintf("%s at %s (delete_with_workspace=%t)",
			ws.Volume.ID, output.EmptyDash(ws.Volume.MountPath), ws.Volume.DeleteWithWorkspace)
	}
	timeoutStr := "-"
	if ws.Timeout != nil {
		timeoutStr = fmt.Sprintf("%s (action=%s)", output.FormatTime(ws.Timeout.ExpiresAt), ws.Timeout.Action)
	}

	items := []output.KV{
		{Key: "ID", Value: ws.ID},
		{Key: "Name", Value: output.EmptyDash(ws.Name)},
		{Key: "Zone", Value: ws.Zone},
		{Key: "Status", Value: string(ws.Status)},
		{Key: "Template", Value: tpl},
		{Key: "Resources", Value: resStr},
		{Key: "Network", Value: netStr},
		{Key: "Volume", Value: volStr},
		{Key: "Timeout", Value: timeoutStr},
		{Key: "Labels", Value: output.FormatLabels(ws.Labels)},
		{Key: "Created At", Value: output.FormatTime(ws.CreatedAt)},
		{Key: "Updated At", Value: output.FormatTime(ws.UpdatedAt)},
	}
	if ws.Failure != nil {
		items = append(items, output.KV{
			Key:   "Failure",
			Value: fmt.Sprintf("%s: %s (retryable=%t)", ws.Failure.Code, ws.Failure.Message, ws.Failure.Retryable),
		})
	}
	return p.Details(items)
}

func (a *App) waitWorkspaceDeleted(ctx context.Context, client *kibotos.Client, initial *kibotos.Workspace, timeout time.Duration) (*kibotos.Workspace, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	last := initial
	for {
		ws, err := client.Workspaces.Get(waitCtx, initial.ID)
		if err != nil {
			if kibotos.IsNotFound(err) {
				deleted := *last
				deleted.Status = kibotos.WorkspaceStatus("deleted")
				return &deleted, nil
			}
			return nil, err
		}
		last = ws
		if ws.Status == kibotos.WorkspaceStatusFailed {
			msg := "workspace deletion failed"
			if ws.Failure != nil && ws.Failure.Message != "" {
				msg = fmt.Sprintf("workspace deletion failed (%s): %s", ws.Failure.Code, ws.Failure.Message)
			}
			return nil, &CLIError{ExitCode: ExitError, Code: "delete_failed", Message: msg}
		}
		if err := sleepWithContext(waitCtx, a.pollDelay()); err != nil {
			return nil, err
		}
	}
}

func (a *App) waitWorkspaceNetworkApplied(ctx context.Context, client *kibotos.Client, id string, mode kibotos.NetworkMode, timeout time.Duration) (*kibotos.Workspace, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if err := sleepWithContext(waitCtx, a.pollDelay()); err != nil {
			return nil, err
		}
		ws, err := client.Workspaces.Get(waitCtx, id)
		if err != nil {
			return nil, err
		}
		if ws.Network.Mode == mode && ws.Network.Applied {
			return ws, nil
		}
		if ws.Status == kibotos.WorkspaceStatusFailed {
			msg := "workspace entered failed state while applying network policy"
			if ws.Failure != nil && ws.Failure.Message != "" {
				msg = fmt.Sprintf("workspace failed (%s): %s", ws.Failure.Code, ws.Failure.Message)
			}
			return nil, &CLIError{ExitCode: ExitError, Code: "network_apply_failed", Message: msg}
		}
	}
}

func collectAllWorkspaces(ctx context.Context, client *kibotos.Client, base *kibotos.WorkspaceListParams) (*kibotos.MultiZonePage[kibotos.Workspace], error) {
	var p kibotos.WorkspaceListParams
	if base != nil {
		p = *base
	}
	all := []kibotos.Workspace{}
	unavailSeen := make(map[string]struct{})
	unavail := []string{}
	for {
		page, err := client.Workspaces.List(ctx, &p)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		for _, z := range page.UnavailableZones {
			if _, ok := unavailSeen[z]; !ok {
				unavailSeen[z] = struct{}{}
				unavail = append(unavail, z)
			}
		}
		next := page.Token()
		if next == "" || next == p.PageToken {
			break
		}
		p.PageToken = next
	}
	return &kibotos.MultiZonePage[kibotos.Workspace]{
		Data:             all,
		UnavailableZones: unavail,
	}, nil
}
