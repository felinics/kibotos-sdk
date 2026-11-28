package cli

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
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
		a.newDesktopCmd(),
		a.newWorkspaceUpdateCmd(),
		a.newWorkspaceDeleteCmd(),
		a.newWorkspaceStartCmd(),
		a.newWorkspaceStopCmd(),
		a.newWorkspaceRestartCmd(),
		a.newWorkspaceTimeoutCmd(),
		a.newWorkspaceNetworkCmd(),
		a.newWorkspaceVolumeCmd(),
		a.newWorkspaceVolumesCmd(),
		a.newWorkspacePacksCmd(),
		a.newWorkspaceResizeCmd(),
		a.newWorkspaceMetricsCmd(),
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
			parsedStatus, err := validateWorkspaceStatus(status)
			if err != nil {
				return err
			}
			if err := validateNonEmptyFlag(cmd, "zone", zone); err != nil {
				return err
			}
			if err := validatePageLimit(cmd, limit); err != nil {
				return err
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			p := a.printer()

			params := &kibotos.WorkspaceListParams{
				Status:    parsedStatus,
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
			if p.IsQuiet() {
				ids := make([]string, 0, len(page.Data))
				for _, ws := range page.Data {
					ids = append(ids, ws.ID)
				}
				return p.IDs(ids)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No workspaces found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, ws := range page.Data {
				volStr := "-"
				if volumes := workspaceVolumes(&ws); len(volumes) > 0 {
					ids := make([]string, 0, len(volumes))
					for _, vol := range volumes {
						ids = append(ids, vol.ID)
					}
					volStr = strings.Join(ids, ", ")
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
					p.FormatStatus(string(ws.Status)),
					string(ws.Network.Mode),
					volStr,
					timeoutStr,
				})
			}
			return p.Table([]string{"ID", "NAME", "ZONE", "TEMPLATE", "STATUS", "NETWORK", "VOLUME", "TIMEOUT"}, rows)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&status, "status", "s", "", "Filter by workspace status")
	f.StringVarP(&zone, "zone", "z", "", "Filter by zone ID")
	f.StringArrayVarP(&labels, "label", "l", nil, "Filter by label (key=value, repeatable)")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newWorkspaceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [id-or-name]",
		Short: "Get details of a workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "Select a workspace to inspect:")
			if err != nil {
				return err
			}
			ws, err := client.Workspaces.Get(ctx, wsID)
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
		memoryRaw     string
		diskRaw       string
		networkMode   string
		volumeRefs    []string
		packIDs       []string
		enableDesktop bool
		openDesktop   bool
		consoleURL    string
		mountPath     string
		deleteVolume  bool
		timeoutRaw    string
		timeoutAction string
		labels        []string
		wait          bool
		noWait        bool
		waitTimeout   string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new workspace",
		Example: `  kibo ws create
  kibo ws create -t debian --desktop --no-input --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if openDesktop && !enableDesktop {
				return usageError("--open requires --desktop")
			}
			if err := validateDesktopConsoleURL(cmd, consoleURL); err != nil {
				return err
			}
			if err := validateNonEmptyFlag(cmd, "template", templateID); err != nil {
				return err
			}
			if err := validateNonEmptyFlag(cmd, "revision", revision); err != nil {
				return err
			}
			if err := validateNonEmptyFlag(cmd, "zone", zone); err != nil {
				return err
			}
			tplID, tplRev, err := parseTemplateSpec(templateID, revision)
			if err != nil {
				return err
			}

			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			// Validate all flags up front before making any network calls.
			var (
				memMiB     int
				diskMiB    int
				timeoutSec int
			)
			if cmd.Flags().Changed("name") {
				name, err = validateResourceName(name, true)
				if err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("vcpus") {
				if err := validateVCPUs(vcpus); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("memory") {
				memMiB, err = parseWorkspaceMemoryMiB(memoryRaw)
				if err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("disk") {
				diskMiB, err = parseWorkspaceDiskMiB(diskRaw)
				if err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("network") {
				if _, err := parseNetworkMode(networkMode); err != nil {
					return err
				}
			}
			volumeInputs, legacyVolume, err := parseCreateVolumes(volumeRefs)
			if err != nil {
				return err
			}
			if err := validatePacks(packIDs); err != nil {
				return err
			}
			if legacyVolume == "" && (cmd.Flags().Changed("mount-path") || cmd.Flags().Changed("delete-volume")) {
				return usageError("--mount-path and --delete-volume require a single --volume without an embedded mount path")
			}
			if cmd.Flags().Changed("mount-path") {
				mountPath, err = validateMountPath(mountPath)
				if err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("timeout") || cmd.Flags().Changed("timeout-action") {
				if !cmd.Flags().Changed("timeout") {
					return usageError("--timeout is required when --timeout-action is set")
				}
				timeoutSec, err = parseDurationSeconds(timeoutRaw, "timeout")
				if err != nil {
					return err
				}
				if _, err := parseTimeoutAction(timeoutAction); err != nil {
					return err
				}
			}
			parsedLabels, err := parseLabels(labels)
			if err != nil {
				return err
			}

			// Fast-fail missing required --template in non-interactive mode BEFORE auth/API calls.
			if tplID == "" && !a.isInteractive() {
				return missingFlagError("--template", "Pass --template <id>. Run `kibo template list --json` to see available templates.")
			}

			client, res, err := a.requireClient()
			if err != nil {
				return err
			}

			// Minimal interactive flow when --template was omitted in a TTY:
			// ask for template, compute preset, and optional name; advanced options stay on flags.
			if tplID == "" {
				tpl, err := a.resolveTemplate(ctx, client, strings.TrimSpace(zone))
				if err != nil {
					return err
				}
				tplID = tpl.ID

				if !cmd.Flags().Changed("vcpus") && !cmd.Flags().Changed("memory") && !cmd.Flags().Changed("disk") {
					pickedVCPUs, pickedMem, pickedDisk, err := a.promptWorkspaceResources()
					if err != nil {
						return err
					}
					vcpus, memMiB, diskMiB = pickedVCPUs, pickedMem, pickedDisk
				}

				if !cmd.Flags().Changed("name") {
					enteredName, err := a.prompter().Input("Name (optional)", "", func(v string) error {
						_, err := validateResourceName(v, true)
						return err
					})
					if err != nil {
						return err
					}
					name = enteredName
				}
			}

			if enableDesktop {
				packID, err := resolveDesktopPackID(ctx, client, strings.TrimSpace(zone))
				if err != nil {
					return err
				}
				if !slices.Contains(packIDs, packID) {
					packIDs = append(packIDs, packID)
				}
				if err := validatePacks(packIDs); err != nil {
					return err
				}
			}

			params := kibotos.CreateWorkspaceParams{
				Volumes: volumeInputs,
				Packs:   packIDs,
				Name:    strings.TrimSpace(name),
				Zone:    strings.TrimSpace(zone),
				Template: kibotos.WorkspaceTemplateSpec{
					ID:       tplID,
					Revision: tplRev,
				},
			}

			if vcpus > 0 || memMiB > 0 || diskMiB > 0 {
				params.Resources = &kibotos.WorkspaceResourcesSpec{
					VCPUs:     vcpus,
					MemoryMiB: memMiB,
					DiskMiB:   diskMiB,
				}
			}

			if strings.TrimSpace(networkMode) != "" {
				mode, err := parseNetworkMode(networkMode)
				if err != nil {
					return err
				}
				params.Network = &kibotos.WorkspaceNetworkSpec{Mode: mode}
			}

			if legacyVolume != "" {
				volID, _, err := a.resolveVolume(ctx, client, legacyVolume, nil, "", "")
				if err != nil {
					return err
				}
				params.Volume = &kibotos.WorkspaceVolumeSpec{
					ID:                  volID,
					MountPath:           strings.TrimSpace(mountPath),
					DeleteWithWorkspace: deleteVolume,
				}
			}

			if timeoutSec > 0 {
				act, err := parseTimeoutAction(timeoutAction)
				if err != nil {
					return err
				}
				params.Timeout = &kibotos.TimeoutSpec{
					Seconds: timeoutSec,
					Action:  act,
				}
			}

			if len(parsedLabels) > 0 {
				params.Labels = parsedLabels
			}

			ws, err := client.Workspaces.Create(ctx, params)
			if err != nil {
				return err
			}

			p := a.printer()
			if shouldWait && ws.Status != kibotos.WorkspaceStatusAvailable {
				wsID := ws.ID
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s to reach available...", wsID))
				ws, err = client.Workspaces.Wait(ctx, wsID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusAvailable},
					Timeout:  waitDur,
				})
				if err != nil {
					stopSpin(fmt.Sprintf("Workspace %s did not reach available", wsID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Workspace %s is available", wsID), true)
			}

			if err := a.renderWorkspace(ws); err != nil {
				return err
			}
			if enableDesktop || hasDesktopCapability(ws) {
				desktopURL := buildDesktopURL(resolveConsoleURL(consoleURL, res), ws.ID, res.OrganizationID)
				if !p.IsJSON() && !p.IsQuiet() {
					if _, err := fmt.Fprintf(p.Out, "Desktop URL: %s\n", p.Link(desktopURL)); err != nil {
						return err
					}
				}
				if openDesktop && ws.Status == kibotos.WorkspaceStatusAvailable {
					a.openDesktopURL(p, desktopURL)
				}
			}
			if ws.Status == kibotos.WorkspaceStatusAvailable {
				p.Hintf("Expose a port with: kibo exp create %s -p 8080", ws.ID)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&templateID, "template", "t", "", "Template ID (supports template@revision; prompted interactively if omitted)")
	f.StringVar(&revision, "revision", "", "Specific template revision ID")
	f.StringVarP(&name, "name", "n", "", "Human-readable workspace name")
	f.StringVarP(&zone, "zone", "z", "", "Target zone ID (defaults to organization default zone)")
	f.IntVar(&vcpus, "vcpus", 0, "vCPU count override")
	f.StringVar(&memoryRaw, "memory", "", "Memory allocation (e.g. 4Gi, 4096; bare integer is MiB)")
	f.StringVar(&diskRaw, "disk", "", "Root disk size (e.g. 20Gi, 20480; bare integer is MiB)")
	f.StringVar(&networkMode, "network", "", "Outbound network mode: public_internet or deny_all")
	f.StringArrayVar(&volumeRefs, "volume", nil, "Volume ID and mount as vol_id:/path (repeatable), or one volume ID or name")
	f.StringArrayVar(&packIDs, "pack", nil, "Pack ID (repeatable, at most four)")
	f.BoolVar(&enableDesktop, "desktop", false, "Enable remote desktop (attaches the desktop.v1 Pack) and show the browser desktop URL")
	f.BoolVar(&openDesktop, "open", false, "Open the browser desktop URL after creation (requires --desktop)")
	f.StringVar(&consoleURL, "console-url", "", "Override Kibotos console URL used for desktop links")
	f.StringVar(&mountPath, "mount-path", "", "Mount path for the attached volume (e.g. /data)")
	f.BoolVar(&deleteVolume, "delete-volume", false, "Delete the attached volume when this workspace is deleted")
	f.StringVar(&timeoutRaw, "timeout", "", "Auto-timeout duration (e.g. 1h, 30m, 3600; bare integer is seconds)")
	f.StringVar(&timeoutAction, "timeout-action", "", "Action on timeout expiration: stop or delete")
	f.StringArrayVarP(&labels, "label", "l", nil, "Workspace label (key=value, repeatable)")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the workspace reaches available")
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
		Use:   "update [id-or-name]",
		Short: "Update a workspace's name or labels",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nameChanged := cmd.Flags().Changed("name")
			if nameChanged && clearName {
				return usageError("--name and --clear-name cannot be used together")
			}
			if nameChanged {
				var err error
				name, err = validateResourceName(name, false)
				if err != nil {
					return err
				}
			}
			parsedLabels, err := parseUpdateLabels(labels, unsetLabels)
			if err != nil {
				return err
			}
			ref := firstArg(args)
			if !a.isInteractive() {
				if strings.TrimSpace(ref) == "" {
					return missingWorkspaceRef()
				}
				if !nameChanged && !clearName && len(parsedLabels) == 0 {
					return usageError("specify at least one of --name, --clear-name, --label, or --unset-label")
				}
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			wsID, existing, err := a.resolveWorkspace(cmd.Context(), client, ref, nil, "no workspaces found", "Select a workspace to update:")
			if err != nil {
				return err
			}

			if !nameChanged && !clearName && len(parsedLabels) == 0 {
				curName := ""
				if existing != nil {
					curName = existing.Name
				}
				newName, err := a.prompter().Input("New workspace name", curName, func(v string) error {
					_, err := validateResourceName(v, true)
					return err
				})
				if err != nil {
					return err
				}
				if newName == "" || newName == curName {
					return usageError("no changes specified; pass --name, --clear-name, --label, or --unset-label")
				}
				name = newName
				nameChanged = true
			}

			params := kibotos.UpdateWorkspaceParams{
				Labels: parsedLabels,
			}
			if nameChanged {
				params.Name = kibotos.Some(name)
			} else if clearName {
				params.Name = kibotos.Null[string]()
			}

			ws, err := client.Workspaces.Update(cmd.Context(), wsID, params)
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&name, "name", "n", "", "Set workspace name")
	f.BoolVar(&clearName, "clear-name", false, "Clear the workspace name (set to null)")
	f.StringArrayVarP(&labels, "label", "l", nil, "Set a label (key=value, repeatable)")
	f.StringArrayVar(&unsetLabels, "unset-label", nil, "Remove a label by key (repeatable)")
	return cmd
}

func (a *App) newWorkspaceDeleteCmd() *cobra.Command {
	var (
		wait        bool
		noWait      bool
		waitTimeout string
	)

	cmd := &cobra.Command{
		Use:   "delete [id-or-name]",
		Short: "Delete a workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, wsID, picked, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "Select a workspace to delete:")
			if err != nil {
				return err
			}

			targetLabel := wsID
			if picked != nil && picked.Name != "" {
				targetLabel = fmt.Sprintf("%q (%s)", picked.Name, picked.ID)
			}
			ok, err := a.confirmDestructive(fmt.Sprintf("Delete workspace %s?", targetLabel))
			if err != nil {
				return err
			}
			if !ok {
				return prompt.ErrCancelled
			}

			ws, err := client.Workspaces.Delete(ctx, wsID)
			if err != nil {
				return err
			}

			if shouldWait {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s to be deleted...", ws.ID))
				ws, err = a.waitWorkspaceDeleted(ctx, client, ws, waitDur)
				if err != nil {
					stopSpin(fmt.Sprintf("Failed deleting workspace %s", ws.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Workspace %s deleted", ws.ID), true)
			}
			return a.renderWorkspace(ws)
		},
	}

	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "workspace deletion completes")
	return cmd
}

func (a *App) newWorkspaceStartCmd() *cobra.Command {
	var (
		timeoutRaw    string
		timeoutAction string
		wait          bool
		noWait        bool
		waitTimeout   string
	)

	cmd := &cobra.Command{
		Use:   "start [id-or-name]",
		Short: "Start a stopped workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := parseStartWorkspaceParams(cmd, timeoutRaw, timeoutAction)
			if err != nil {
				return err
			}
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(
				ctx, firstArg(args),
				func(w kibotos.Workspace) bool { return w.Status == kibotos.WorkspaceStatusStopped },
				"no stopped workspaces found to start",
				"Select a stopped workspace to start:",
			)
			if err != nil {
				return err
			}

			ws, err := client.Workspaces.Start(ctx, wsID, params)
			if err != nil {
				return err
			}

			if shouldWait && ws.Status != kibotos.WorkspaceStatusAvailable {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s to reach available...", ws.ID))
				ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusAvailable},
					Timeout:  waitDur,
				})
				if err != nil {
					stopSpin(fmt.Sprintf("Failed starting workspace %s", ws.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Workspace %s is available", ws.ID), true)
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringVar(&timeoutRaw, "timeout", "", "Auto-timeout duration (e.g. 1h, 30m, 3600; bare integer is seconds)")
	f.StringVar(&timeoutAction, "timeout-action", "", "Action on timeout expiration: stop or delete")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the workspace reaches available")
	return cmd
}

func parseStartWorkspaceParams(cmd *cobra.Command, timeoutRaw, timeoutAction string) (*kibotos.StartWorkspaceParams, error) {
	if !cmd.Flags().Changed("timeout") {
		if cmd.Flags().Changed("timeout-action") {
			return nil, missingFlagError("--timeout", "Pass --timeout with --timeout-action, for example --timeout 1h.")
		}
		return nil, nil
	}
	seconds, err := parseDurationSeconds(timeoutRaw, "timeout")
	if err != nil {
		return nil, err
	}
	action, err := parseTimeoutAction(timeoutAction)
	if err != nil {
		return nil, err
	}
	return &kibotos.StartWorkspaceParams{Timeout: &kibotos.TimeoutSpec{Seconds: seconds, Action: action}}, nil
}

func (a *App) newWorkspaceRestartCmd() *cobra.Command {
	var force, wait, noWait bool
	var timeoutRaw, timeoutAction, waitTimeout string
	cmd := &cobra.Command{
		Use:     "restart [id-or-name]",
		Short:   "Restart a workspace (stop and start)",
		Example: "  kibo ws restart\n  kibo ws restart wks_123 --timeout 1h --no-input --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := parseStartWorkspaceParams(cmd, timeoutRaw, timeoutAction)
			if err != nil {
				return err
			}
			shouldWait, timeout, err := resolveRestartWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, ws, err := a.resolveCommandWorkspace(ctx, firstArg(args), isAvailableWorkspace,
				"no available workspaces found to restart", "Select an available workspace to restart:")
			if err != nil {
				return err
			}
			if ws == nil {
				ws = &kibotos.Workspace{ID: id}
			}
			if force {
				ok, err := a.confirmDestructive(fmt.Sprintf("Force power-off workspace %s before starting it again?", resourceLabel(ws.ID, ws.Name)))
				if err != nil {
					return err
				}
				if !ok {
					return prompt.ErrCancelled
				}
			}
			stopped, err := a.stopWorkspaceAndWait(ctx, client, ws, force, timeout)
			if err != nil {
				return err
			}
			ws, err = a.startWorkspace(ctx, client, stopped, params, shouldWait, timeout)
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "Force immediate power-off before starting again")
	f.StringVar(&timeoutRaw, "timeout", "", "Auto-timeout after starting (e.g. 1h, 30m, 3600)")
	f.StringVar(&timeoutAction, "timeout-action", "", "Action on timeout expiration: stop or delete")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the workspace reaches available again")
	return cmd
}

func (a *App) newWorkspaceStopCmd() *cobra.Command {
	var (
		force       bool
		wait        bool
		noWait      bool
		waitTimeout string
	)

	cmd := &cobra.Command{
		Use:   "stop [id-or-name]",
		Short: "Stop an available workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, wsID, picked, err := a.resolveCommandWorkspace(
				ctx, firstArg(args),
				func(w kibotos.Workspace) bool { return w.Status == kibotos.WorkspaceStatusAvailable },
				"no available workspaces found to stop",
				"Select a workspace to stop:",
			)
			if err != nil {
				return err
			}

			if force {
				targetLabel := wsID
				if picked != nil && picked.Name != "" {
					targetLabel = fmt.Sprintf("%q (%s)", picked.Name, picked.ID)
				}
				ok, err := a.confirmDestructive(fmt.Sprintf("Force power-off workspace %s immediately?", targetLabel))
				if err != nil {
					return err
				}
				if !ok {
					return prompt.ErrCancelled
				}
			}

			var params *kibotos.StopWorkspaceParams
			if force {
				params = &kibotos.StopWorkspaceParams{Force: true}
			}

			ws, err := client.Workspaces.Stop(ctx, wsID, params)
			if err != nil {
				return err
			}

			if shouldWait && ws.Status != kibotos.WorkspaceStatusStopped {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s to reach stopped...", ws.ID))
				ws, err = client.Workspaces.Wait(ctx, ws.ID, kibotos.WaitWorkspaceParams{
					Statuses: []kibotos.WorkspaceStatus{kibotos.WorkspaceStatusStopped},
					Timeout:  waitDur,
				})
				if err != nil {
					stopSpin(fmt.Sprintf("Failed stopping workspace %s", ws.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Workspace %s is stopped", ws.ID), true)
			}
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&force, "force", false, "Force immediate power-off instead of graceful shutdown")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the workspace reaches stopped")
	return cmd
}

func (a *App) newWorkspaceTimeoutCmd() *cobra.Command {
	var (
		secondsRaw string
		action     string
	)

	cmd := &cobra.Command{
		Use:   "timeout [id-or-name]",
		Short: "Set or extend a workspace's auto-timeout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				seconds int
				err     error
			)
			if cmd.Flags().Changed("seconds") {
				seconds, err = parseDurationSeconds(secondsRaw, "seconds")
				if err != nil {
					return err
				}
			} else if !a.isInteractive() {
				return missingFlagError("--seconds", "Pass --seconds <duration> (e.g. --seconds 2h or --seconds 7200).")
			}

			act, err := parseTimeoutAction(action)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(
				ctx, firstArg(args),
				func(w kibotos.Workspace) bool { return w.Status == kibotos.WorkspaceStatusAvailable },
				"no available workspaces found",
				"Select a workspace to update timeout:",
			)
			if err != nil {
				return err
			}

			if seconds == 0 {
				durInput, err := a.prompter().Input("Timeout duration (e.g. 1h, 30m)", "1h", func(v string) error {
					_, err := parseDurationSeconds(v, "seconds")
					return err
				})
				if err != nil {
					return err
				}
				seconds, _ = parseDurationSeconds(durInput, "seconds")
			}

			ws, err := client.Workspaces.SetTimeout(ctx, wsID, kibotos.SetWorkspaceTimeoutParams{
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
	f.StringVar(&secondsRaw, "seconds", "", "New timeout duration from now (e.g. 2h, 30m, 7200; bare integer is seconds)")
	f.StringVar(&action, "action", "", "Timeout action: stop or delete")
	return cmd
}

func (a *App) newWorkspaceVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Attach or detach a persistent volume on a workspace",
	}

	var (
		volumeRef                            string
		mountPath                            string
		newMount                             string
		deleteWithWorkspace                  bool
		attachRestart, detachRestart         bool
		attachWaitTimeout, detachWaitTimeout string
	)

	attachCmd := &cobra.Command{
		Use:     "attach [workspace] [volume]",
		Short:   "Attach a persistent volume to a workspace",
		Example: "  kibo ws volume attach\n  kibo ws volume attach wks_123 vol_123 --mount /data --restart --no-input --json",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			positional := len(args) == 2
			volume, mount := volumeRef, mountPath
			if positional {
				if cmd.Flags().Changed("volume") || cmd.Flags().Changed("mount-path") {
					return usageError("use the volume argument and --mount together")
				}
				volume, mount = args[1], newMount
				if strings.TrimSpace(volume) == "" {
					return missingVolumeRef()
				}
				if !cmd.Flags().Changed("mount") && !a.isInteractive() {
					return missingFlagError("--mount", "Pass the absolute mount path, for example --mount /data.")
				}
			} else {
				if cmd.Flags().Changed("mount") {
					return usageError("--mount requires a positional volume ID or name")
				}
				if err := validateNonEmptyFlag(cmd, "volume", volume); err != nil {
					return err
				}
				if strings.TrimSpace(volume) == "" && !a.isInteractive() {
					return missingFlagError("--volume", "Pass --volume <vol_id>. Run `kibo volume list --attached false --json` to see available volumes.")
				}
			}
			if cmd.Flags().Changed("mount") || cmd.Flags().Changed("mount-path") {
				var err error
				mount, err = validateMountPath(mount)
				if err != nil {
					return err
				}
			}

			timeout, err := transitionTimeout(attachWaitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, ws, err := a.resolveCommandWorkspace(ctx, firstArg(args), stoppedOrAvailableWorkspace,
				"no running or stopped workspaces found", "Select a running or stopped workspace:")
			if err != nil {
				return err
			}
			if ws == nil {
				ws, err = client.Workspaces.Get(ctx, id)
				if err != nil {
					return err
				}
			}
			needsMount := positional || len(workspaceVolumes(ws)) > 0
			if needsMount && mount == "" && !a.isInteractive() {
				return missingFlagError("--mount-path", "Pass an absolute mount path when the workspace already has an attached volume.")
			}
			volID, _, err := a.resolveVolume(ctx, client, volume, func(v kibotos.Volume) bool {
				return v.Status == kibotos.VolumeStatusAvailable && v.Zone == ws.Zone &&
					(v.WorkspaceID == nil || *v.WorkspaceID == "")
			}, fmt.Sprintf("no unattached available volumes found in zone %s", output.EmptyDash(ws.Zone)), "Select a volume to attach:")
			if err != nil {
				return err
			}

			if needsMount {
				if mount == "" {
					mount, err = a.prompter().Input("Mount path (e.g. /data)", "/data", validateMountPathInput)
					if err != nil {
						return err
					}
					mount, _ = validateMountPath(mount)
				}
				return a.updateWorkspaceVolumeBindings(ctx, client, ws, volID, mount, deleteWithWorkspace, attachRestart, timeout)
			}
			ws, err = a.withStoppedWorkspace(ctx, client, ws, "update volumes", attachRestart, timeout, func() (*kibotos.Workspace, error) {
				return client.Workspaces.AttachVolume(ctx, id, kibotos.AttachVolumeParams{
					ID: volID, MountPath: mount, DeleteWithWorkspace: deleteWithWorkspace,
				})
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
	addVolumeRestartFlags(attachCmd, &attachRestart, &attachWaitTimeout)
	af := attachCmd.Flags()
	af.StringVar(&newMount, "mount", "", "Absolute path for the volume mount")
	af.StringVar(&volumeRef, "volume", "", "Persistent volume ID or name to attach")
	af.StringVar(&mountPath, "mount-path", "", "Mount path inside the workspace (e.g. /data)")
	af.BoolVar(&deleteWithWorkspace, "delete-with-workspace", false, "Delete the volume when the workspace is deleted")

	detachCmd := &cobra.Command{
		Use:     "detach [workspace] [volume]",
		Short:   "Detach a volume or detach all volumes",
		Example: "  kibo ws volume detach\n  kibo ws volume detach wks_123 vol_123 --restart --no-input --json",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 && strings.TrimSpace(args[1]) == "" {
				return missingVolumeRef()
			}
			timeout, err := transitionTimeout(detachWaitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, ws, err := a.resolveCommandWorkspace(ctx, firstArg(args), func(w kibotos.Workspace) bool {
				return len(workspaceVolumes(&w)) > 0
			}, "no workspaces with attached volumes found", "Select a workspace to detach volumes from:")
			if err != nil {
				return err
			}
			ws, err = workspaceState(ctx, client, id, ws, len(args) == 2 || detachRestart || a.isInteractive())
			if err != nil {
				return err
			}
			if len(args) == 2 {
				volID, _, err := a.resolveVolume(ctx, client, args[1], func(v kibotos.Volume) bool {
					return slices.ContainsFunc(workspaceVolumes(ws), func(binding kibotos.WorkspaceVolume) bool {
						return binding.ID == v.ID
					})
				}, "no matching volumes are attached to this workspace", "Select a volume to detach:")
				if err != nil {
					return err
				}
				return a.updateWorkspaceVolumeBindings(ctx, client, ws, volID, "", false, detachRestart, timeout)
			}
			action := "update volumes"
			if a.isInteractive() {
				vols := workspaceVolumes(ws)
				if len(vols) == 0 {
					return &CLIError{ExitCode: ExitError, Code: "no_matching_volume", Message: "workspace has no attached volumes"}
				}
				var selected string
				if len(vols) > 1 {
					options := make([]prompt.Option, 0, len(vols)+1)
					for _, vol := range vols {
						options = append(options, prompt.Option{Value: vol.ID, Label: fmt.Sprintf("%s at %s", vol.ID, vol.MountPath)})
					}
					options = append(options, prompt.Option{Label: "All volumes"})
					choice, err := a.prompter().Select("Select a volume to detach:", "Volume", options, 0)
					if err != nil {
						return err
					}
					selected = choice.Value
				}
				label := resourceLabel(id, ws.Name)
				message := fmt.Sprintf("Detach all volumes from %s?", label)
				if len(vols) == 1 {
					message = fmt.Sprintf("Detach volume %s from %s?", vols[0].ID, label)
				}
				if selected != "" {
					return a.updateWorkspaceVolumeBindings(ctx, client, ws, selected, "", false, detachRestart, timeout)
				}
				action = "detach all volumes"
				if len(vols) == 1 {
					action = fmt.Sprintf("detach volume %s", vols[0].ID)
				}
				if err := a.confirmVolumeRemoval(ws, detachRestart, message); err != nil {
					return err
				}
			}
			ws, err = a.withStoppedWorkspace(ctx, client, ws, action, detachRestart, timeout, func() (*kibotos.Workspace, error) {
				return client.Workspaces.DetachVolume(ctx, id)
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}

	addVolumeRestartFlags(detachCmd, &detachRestart, &detachWaitTimeout)
	cmd.AddCommand(attachCmd, detachCmd)
	return cmd
}

func (a *App) newWorkspaceWaitCmd() *cobra.Command {
	var (
		statuses   []string
		timeoutRaw string
	)

	cmd := &cobra.Command{
		Use:   "wait [id-or-name]",
		Short: "Wait until a workspace reaches one of the target statuses",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			timeoutSec, err := parseDurationSeconds(timeoutRaw, "timeout")
			if err != nil {
				return err
			}
			for _, raw := range statuses {
				if s := strings.TrimSpace(raw); s != "" {
					if _, err := validateWorkspaceStatus(s); err != nil {
						return err
					}
				}
			}
			if len(statuses) == 0 && !a.isInteractive() {
				return missingFlagError("--status", "Pass --status <s1,s2> (e.g. --status available,failed).")
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "Select a workspace to wait on:")
			if err != nil {
				return err
			}

			if len(statuses) == 0 {
				statuses = []string{"available", "failed"}
			}

			parsed := make([]kibotos.WorkspaceStatus, 0, len(statuses))
			for _, raw := range statuses {
				s := strings.TrimSpace(raw)
				if s == "" {
					continue
				}
				wsStatus, err := validateWorkspaceStatus(s)
				if err != nil {
					return err
				}
				parsed = append(parsed, wsStatus)
			}
			if len(parsed) == 0 {
				return usageError("--status must include at least one non-empty status")
			}

			p := a.printer()
			stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for workspace %s (%s)...", wsID, strings.Join(statuses, ",")))
			ws, err := client.Workspaces.Wait(ctx, wsID, kibotos.WaitWorkspaceParams{
				Statuses: parsed,
				Timeout:  time.Duration(timeoutSec) * time.Second,
			})
			if err != nil {
				stopSpin(fmt.Sprintf("Wait failed for workspace %s", wsID), false)
				return err
			}
			stopSpin(fmt.Sprintf("Workspace %s reached %s", ws.ID, ws.Status), true)
			return a.renderWorkspace(ws)
		},
	}

	f := cmd.Flags()
	f.StringSliceVarP(&statuses, "status", "s", nil, "Target status(es), comma-separated or repeatable")
	f.StringVar(&timeoutRaw, "timeout", fmt.Sprintf("%ds", defaultWaitCmdTimeoutSeconds), "Maximum duration to wait (e.g. 30s, 3m, 30)")
	return cmd
}

func (a *App) renderWorkspace(ws *kibotos.Workspace) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(ws)
	}
	if p.IsQuiet() {
		return p.ID(ws.ID)
	}
	tpl := ws.Template.ID
	if ws.Template.Revision != "" {
		tpl += "@" + ws.Template.Revision
	}
	resStr := fmt.Sprintf("%d vCPU / %d MiB RAM / %d MiB disk",
		ws.Resources.VCPUs, ws.Resources.MemoryMiB, ws.Resources.DiskMiB)
	applied := "pending start"
	if ws.Network.Applied != nil {
		applied = fmt.Sprint(*ws.Network.Applied)
	}
	netStr := fmt.Sprintf("%s (applied=%s)", ws.Network.Mode, applied)
	volumes := []string{}
	for _, vol := range workspaceVolumes(ws) {
		volumes = append(volumes, fmt.Sprintf("%s at %s (delete_with_workspace=%t)",
			vol.ID, output.EmptyDash(vol.MountPath), vol.DeleteWithWorkspace))
	}
	packs := []string{}
	for _, pack := range ws.Packs {
		packs = append(packs, pack.ID+"@"+pack.Revision)
	}
	timeoutStr := "-"
	if ws.Timeout != nil {
		timeoutStr = fmt.Sprintf("%s (action=%s)", output.FormatTime(ws.Timeout.ExpiresAt), ws.Timeout.Action)
	}

	items := []output.KV{
		{Key: "ID", Value: ws.ID},
		{Key: "Name", Value: output.EmptyDash(ws.Name)},
		{Key: "Zone", Value: ws.Zone},
		{Key: "Status", Value: p.FormatStatus(string(ws.Status))},
		{Key: "Template", Value: tpl},
		{Key: "Resources", Value: resStr},
		{Key: "Network", Value: netStr},
		{Key: "Volumes", Value: output.EmptyDash(strings.Join(volumes, ", "))},
		{Key: "Packs", Value: output.EmptyDash(strings.Join(packs, ", "))},
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

func (a *App) waitWorkspaceNetworkApplied(ctx context.Context, client *kibotos.Client, id string, policy kibotos.WorkspaceNetwork, timeout time.Duration) (*kibotos.Workspace, error) {
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
		if networkApplied(ws) && sameNetworkPolicy(ws.Network, policy) {
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

func (a *App) promptWorkspaceResources() (int, int, int, error) {
	options := []prompt.Option{
		{Value: "2c2g", Label: "2c2g", Description: "2 vCPU, 2 GiB RAM"},
		{Value: "2c4g", Label: "2c4g", Description: "2 vCPU, 4 GiB RAM"},
		{Value: "4c4g", Label: "4c4g", Description: "4 vCPU, 4 GiB RAM"},
		{Value: "4c8g", Label: "4c8g", Description: "4 vCPU, 8 GiB RAM"},
		{Value: "8c16g", Label: "8c16g", Description: "8 vCPU, 16 GiB RAM"},
		{Value: "custom", Label: "custom", Description: "Enter vCPUs, memory, and disk"},
	}
	choice, err := a.prompter().Select("Select resources:", "Resources", options, 0)
	if err != nil {
		return 0, 0, 0, err
	}
	switch choice.Value {
	case "2c2g":
		return 2, 2048, 0, nil
	case "2c4g":
		return 2, 4096, 0, nil
	case "4c4g":
		return 4, 4096, 0, nil
	case "4c8g":
		return 4, 8192, 0, nil
	case "8c16g":
		return 8, 16384, 0, nil
	default:
		vcpuStr, err := a.prompter().Input("vCPUs", "2", func(v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return fmt.Errorf("vCPUs must be a positive integer")
			}
			return validateVCPUs(n)
		})
		if err != nil {
			return 0, 0, 0, err
		}
		vcpus, _ := strconv.Atoi(strings.TrimSpace(vcpuStr))

		memStr, err := a.prompter().Input("Memory (e.g. 2G, 4096M)", "2G", func(v string) error {
			_, err := parseWorkspaceMemoryMiB(v)
			return err
		})
		if err != nil {
			return 0, 0, 0, err
		}
		memMiB, _ := parseWorkspaceMemoryMiB(memStr)

		diskStr, err := a.prompter().Input("Disk (optional, e.g. 10G)", "", func(v string) error {
			if strings.TrimSpace(v) == "" {
				return nil
			}
			_, err := parseWorkspaceDiskMiB(v)
			return err
		})
		if err != nil {
			return 0, 0, 0, err
		}
		var diskMiB int
		if strings.TrimSpace(diskStr) != "" {
			diskMiB, _ = parseWorkspaceDiskMiB(diskStr)
		}
		return vcpus, memMiB, diskMiB, nil
	}
}
