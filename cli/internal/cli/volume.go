package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
)

func (a *App) newVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"vol", "volumes"},
		Short:   "Create and manage persistent Kibotos storage volumes",
	}

	cmd.AddCommand(
		a.newVolumeListCmd(),
		a.newVolumeGetCmd(),
		a.newVolumeCreateCmd(),
		a.newVolumeUpdateCmd(),
		a.newVolumeResizeCmd(),
		a.newVolumeDeleteCmd(),
		a.newVolumeWaitCmd(),
	)
	return cmd
}

func (a *App) newVolumeListCmd() *cobra.Command {
	var (
		zone      string
		attached  string
		labels    []string
		limit     int
		pageToken string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List persistent volumes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			attachedPtr, err := parseOptionalBool(attached, "attached")
			if err != nil {
				return err
			}
			parsedLabels, err := parseLabels(labels)
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

			params := &kibotos.VolumeListParams{
				Zone:      strings.TrimSpace(zone),
				Attached:  attachedPtr,
				Labels:    parsedLabels,
				Limit:     limit,
				PageToken: pageToken,
			}

			var page *kibotos.MultiZonePage[kibotos.Volume]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Volumes.List(ctx, params)
				if err != nil {
					return err
				}
			} else {
				page, err = collectAllVolumes(ctx, client, params)
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
				for _, v := range page.Data {
					ids = append(ids, v.ID)
				}
				return p.IDs(ids)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No volumes found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, v := range page.Data {
				wsID := "-"
				if v.WorkspaceID != nil && *v.WorkspaceID != "" {
					wsID = *v.WorkspaceID
				}
				rows = append(rows, []string{
					v.ID,
					output.EmptyDash(v.Name),
					v.Zone,
					strconv.Itoa(v.SizeGiB),
					p.FormatStatus(string(v.Status)),
					wsID,
				})
			}
			return p.Table([]string{"ID", "NAME", "ZONE", "SIZE (GiB)", "STATUS", "WORKSPACE"}, rows)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&zone, "zone", "z", "", "Filter by zone ID")
	f.StringVar(&attached, "attached", "", "Filter by attachment state: true or false")
	f.StringArrayVarP(&labels, "label", "l", nil, "Filter by label (key=value, repeatable)")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) resolveCommandVolume(ctx context.Context, ref string, filter func(kibotos.Volume) bool, emptyMessage, title string) (*kibotos.Client, string, *kibotos.Volume, error) {
	if strings.TrimSpace(ref) == "" && !a.isInteractive() {
		return nil, "", nil, missingVolumeRef()
	}
	client, _, err := a.requireClient()
	if err != nil {
		return nil, "", nil, err
	}
	id, vol, err := a.resolveVolume(ctx, client, ref, filter, emptyMessage, title)
	return client, id, vol, err
}

func (a *App) newVolumeGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [id-or-name]",
		Short: "Get details of a persistent volume",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, volID, _, err := a.resolveCommandVolume(ctx, firstArg(args), nil, "", "Select a volume to inspect:")
			if err != nil {
				return err
			}
			vol, err := client.Volumes.Get(ctx, volID)
			if err != nil {
				return err
			}
			return a.renderVolume(vol)
		},
	}
}

func (a *App) newVolumeCreateCmd() *cobra.Command {
	var (
		sizeRaw     string
		name        string
		zone        string
		labels      []string
		wait        bool
		noWait      bool
		waitTimeout string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new persistent volume",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				sizeGiB int
				err     error
			)
			if cmd.Flags().Changed("size") {
				sizeGiB, err = parseSizeGiB(sizeRaw, "size")
				if err != nil {
					return err
				}
			} else if !a.isInteractive() {
				return missingFlagError("--size", "Pass --size <gib> (e.g. --size 50 or --size 50Gi).")
			}

			if cmd.Flags().Changed("name") {
				name, err = validateResourceName(name, true)
				if err != nil {
					return err
				}
			}
			if err := validateNonEmptyFlag(cmd, "zone", zone); err != nil {
				return err
			}
			parsedLabels, err := parseLabels(labels)
			if err != nil {
				return err
			}
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()

			if sizeGiB == 0 {
				sizeInput, err := a.prompter().Input("Volume size (e.g. 20Gi, 50Gi)", "20Gi", func(v string) error {
					_, err := parseSizeGiB(v, "size")
					return err
				})
				if err != nil {
					return err
				}
				sizeGiB, _ = parseSizeGiB(sizeInput, "size")

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

			vol, err := client.Volumes.Create(ctx, kibotos.CreateVolumeParams{
				Name:    strings.TrimSpace(name),
				Zone:    strings.TrimSpace(zone),
				SizeGiB: sizeGiB,
				Labels:  parsedLabels,
			})
			if err != nil {
				return err
			}

			if shouldWait && vol.Status != kibotos.VolumeStatusAvailable {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for volume %s to reach available...", vol.ID))
				vol, err = client.Volumes.Wait(ctx, vol.ID, kibotos.WaitVolumeParams{
					Statuses: []kibotos.VolumeStatus{kibotos.VolumeStatusAvailable},
					Timeout:  waitDur,
				})
				if err != nil {
					stopSpin(fmt.Sprintf("Failed creating volume %s", vol.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Volume %s is available", vol.ID), true)
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&sizeRaw, "size", "s", "", "Volume size (e.g. 50, 50Gi, 1Ti; bare integer is GiB)")
	f.StringVarP(&name, "name", "n", "", "Human-readable volume name")
	f.StringVarP(&zone, "zone", "z", "", "Target zone ID")
	f.StringArrayVarP(&labels, "label", "l", nil, "Volume label (key=value, repeatable)")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the volume reaches available")
	return cmd
}

func (a *App) newVolumeUpdateCmd() *cobra.Command {
	var (
		name        string
		clearName   bool
		labels      []string
		unsetLabels []string
	)

	cmd := &cobra.Command{
		Use:   "update [id-or-name]",
		Short: "Update a volume's name or labels",
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
					return missingVolumeRef()
				}
				if !nameChanged && !clearName && len(parsedLabels) == 0 {
					return usageError("specify at least one of --name, --clear-name, --label, or --unset-label")
				}
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			volID, existing, err := a.resolveVolume(cmd.Context(), client, ref, nil, "no volumes found", "Select a volume to update:")
			if err != nil {
				return err
			}

			if !nameChanged && !clearName && len(parsedLabels) == 0 {
				curName := ""
				if existing != nil {
					curName = existing.Name
				}
				newName, err := a.prompter().Input("New volume name", curName, func(v string) error {
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

			params := kibotos.UpdateVolumeParams{
				Labels: parsedLabels,
			}
			if nameChanged {
				params.Name = kibotos.Some(name)
			} else if clearName {
				params.Name = kibotos.Null[string]()
			}

			vol, err := client.Volumes.Update(cmd.Context(), volID, params)
			if err != nil {
				return err
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&name, "name", "n", "", "Set volume name")
	f.BoolVar(&clearName, "clear-name", false, "Clear the volume name (set to null)")
	f.StringArrayVarP(&labels, "label", "l", nil, "Set a label (key=value, repeatable)")
	f.StringArrayVar(&unsetLabels, "unset-label", nil, "Remove a label by key (repeatable)")
	return cmd
}

func (a *App) newVolumeResizeCmd() *cobra.Command {
	var (
		sizeRaw     string
		wait        bool
		noWait      bool
		waitTimeout string
	)

	cmd := &cobra.Command{
		Use:   "resize [id-or-name]",
		Short: "Grow a persistent volume to a larger size",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				sizeGiB int
				err     error
			)
			if cmd.Flags().Changed("size") {
				sizeGiB, err = parseSizeGiB(sizeRaw, "size")
				if err != nil {
					return err
				}
			} else if !a.isInteractive() {
				return missingFlagError("--size", "Pass --size <gib> (e.g. --size 100 or --size 100Gi).")
			}

			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, volID, picked, err := a.resolveCommandVolume(
				ctx, firstArg(args),
				func(v kibotos.Volume) bool { return v.Status == kibotos.VolumeStatusAvailable },
				"no available volumes found to resize",
				"Select an available volume to resize:",
			)
			if err != nil {
				return err
			}

			if sizeGiB == 0 {
				promptLabel := "New volume size (e.g. 100Gi)"
				if picked != nil && picked.SizeGiB > 0 {
					promptLabel = fmt.Sprintf("New volume size (current: %d GiB)", picked.SizeGiB)
				}
				sizeInput, err := a.prompter().Input(promptLabel, "", func(v string) error {
					n, err := parseSizeGiB(v, "size")
					if err != nil {
						return err
					}
					if picked != nil && n <= picked.SizeGiB {
						return fmt.Errorf("new size (%d GiB) must be larger than current size (%d GiB)", n, picked.SizeGiB)
					}
					return nil
				})
				if err != nil {
					return err
				}
				sizeGiB, _ = parseSizeGiB(sizeInput, "size")
			} else if picked != nil && picked.SizeGiB > 0 && sizeGiB <= picked.SizeGiB {
				return usageError("new size (%d GiB) must be larger than current volume size (%d GiB)", sizeGiB, picked.SizeGiB)
			}

			vol, err := client.Volumes.Resize(ctx, volID, kibotos.ResizeVolumeParams{
				SizeGiB: sizeGiB,
			})
			if err != nil {
				return err
			}

			if shouldWait && vol.Status != kibotos.VolumeStatusAvailable {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for volume %s resize to complete...", vol.ID))
				vol, err = client.Volumes.Wait(ctx, vol.ID, kibotos.WaitVolumeParams{
					Statuses: []kibotos.VolumeStatus{kibotos.VolumeStatusAvailable},
					Timeout:  waitDur,
				})
				if err != nil {
					stopSpin(fmt.Sprintf("Failed resizing volume %s", vol.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Volume %s resized to %d GiB", vol.ID, vol.SizeGiB), true)
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&sizeRaw, "size", "s", "", "New volume size (e.g. 100, 100Gi; bare integer is GiB)")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the resize finishes")
	return cmd
}

func (a *App) newVolumeDeleteCmd() *cobra.Command {
	var (
		wait        bool
		noWait      bool
		waitTimeout string
	)

	cmd := &cobra.Command{
		Use:   "delete [id-or-name]",
		Short: "Delete a persistent volume",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, volID, picked, err := a.resolveCommandVolume(ctx, firstArg(args), nil, "", "Select a volume to delete:")
			if err != nil {
				return err
			}

			targetLabel := volID
			if picked != nil && picked.Name != "" {
				targetLabel = fmt.Sprintf("%q (%s)", picked.Name, picked.ID)
			}
			ok, err := a.confirmDestructive(fmt.Sprintf("Delete volume %s?", targetLabel))
			if err != nil {
				return err
			}
			if !ok {
				return prompt.ErrCancelled
			}

			vol, err := client.Volumes.Delete(ctx, volID)
			if err != nil {
				return err
			}

			if shouldWait {
				p := a.printer()
				stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for volume %s to be deleted...", vol.ID))
				vol, err = a.waitVolumeDeleted(ctx, client, vol, waitDur)
				if err != nil {
					stopSpin(fmt.Sprintf("Failed deleting volume %s", vol.ID), false)
					return err
				}
				stopSpin(fmt.Sprintf("Volume %s deleted", vol.ID), true)
			}
			return a.renderVolume(vol)
		},
	}

	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "volume deletion completes")
	return cmd
}

func (a *App) newVolumeWaitCmd() *cobra.Command {
	var (
		statuses   []string
		timeoutRaw string
	)

	cmd := &cobra.Command{
		Use:   "wait [id-or-name]",
		Short: "Wait until a volume reaches one of the target statuses",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			timeoutSec, err := parseDurationSeconds(timeoutRaw, "timeout")
			if err != nil {
				return err
			}
			for _, raw := range statuses {
				if s := strings.TrimSpace(raw); s != "" {
					if _, err := validateVolumeStatus(s); err != nil {
						return err
					}
				}
			}
			if len(statuses) == 0 && !a.isInteractive() {
				return missingFlagError("--status", "Pass --status <s1,s2> (e.g. --status available,failed).")
			}

			ctx := cmd.Context()
			client, volID, _, err := a.resolveCommandVolume(ctx, firstArg(args), nil, "", "Select a volume to wait on:")
			if err != nil {
				return err
			}

			if len(statuses) == 0 {
				statuses = []string{"available", "failed"}
			}

			parsed := make([]kibotos.VolumeStatus, 0, len(statuses))
			for _, raw := range statuses {
				s := strings.TrimSpace(raw)
				if s == "" {
					continue
				}
				volStatus, err := validateVolumeStatus(s)
				if err != nil {
					return err
				}
				parsed = append(parsed, volStatus)
			}
			if len(parsed) == 0 {
				return usageError("--status must include at least one non-empty status")
			}

			p := a.printer()
			stopSpin := p.StartSpinner(fmt.Sprintf("Waiting for volume %s (%s)...", volID, strings.Join(statuses, ",")))
			vol, err := client.Volumes.Wait(ctx, volID, kibotos.WaitVolumeParams{
				Statuses: parsed,
				Timeout:  time.Duration(timeoutSec) * time.Second,
			})
			if err != nil {
				stopSpin(fmt.Sprintf("Wait failed for volume %s", volID), false)
				return err
			}
			stopSpin(fmt.Sprintf("Volume %s reached %s", vol.ID, vol.Status), true)
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringSliceVarP(&statuses, "status", "s", nil, "Target status(es), comma-separated or repeatable")
	f.StringVar(&timeoutRaw, "timeout", fmt.Sprintf("%ds", defaultWaitCmdTimeoutSeconds), "Maximum duration to wait (e.g. 30s, 3m, 30)")
	return cmd
}

func (a *App) renderVolume(vol *kibotos.Volume) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(vol)
	}
	if p.IsQuiet() {
		return p.ID(vol.ID)
	}
	wsID := "-"
	if vol.WorkspaceID != nil && *vol.WorkspaceID != "" {
		wsID = *vol.WorkspaceID
	}
	items := []output.KV{
		{Key: "ID", Value: vol.ID},
		{Key: "Name", Value: output.EmptyDash(vol.Name)},
		{Key: "Zone", Value: vol.Zone},
		{Key: "Size", Value: fmt.Sprintf("%d GiB", vol.SizeGiB)},
		{Key: "Status", Value: p.FormatStatus(string(vol.Status))},
		{Key: "Workspace", Value: wsID},
		{Key: "Labels", Value: output.FormatLabels(vol.Labels)},
		{Key: "Created At", Value: output.FormatTime(vol.CreatedAt)},
		{Key: "Updated At", Value: output.FormatTime(vol.UpdatedAt)},
	}
	if vol.Failure != nil {
		items = append(items, output.KV{
			Key:   "Failure",
			Value: fmt.Sprintf("%s: %s (retryable=%t)", vol.Failure.Code, vol.Failure.Message, vol.Failure.Retryable),
		})
	}
	return p.Details(items)
}

func (a *App) waitVolumeDeleted(ctx context.Context, client *kibotos.Client, initial *kibotos.Volume, timeout time.Duration) (*kibotos.Volume, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	last := initial
	for {
		vol, err := client.Volumes.Get(waitCtx, initial.ID)
		if err != nil {
			if kibotos.IsNotFound(err) {
				deleted := *last
				deleted.Status = kibotos.VolumeStatus("deleted")
				return &deleted, nil
			}
			return nil, err
		}
		last = vol
		if vol.Status == kibotos.VolumeStatusFailed {
			msg := "volume deletion failed"
			if vol.Failure != nil && vol.Failure.Message != "" {
				msg = fmt.Sprintf("volume deletion failed (%s): %s", vol.Failure.Code, vol.Failure.Message)
			}
			return nil, &CLIError{ExitCode: ExitError, Code: "delete_failed", Message: msg}
		}
		if err := sleepWithContext(waitCtx, a.pollDelay()); err != nil {
			return nil, err
		}
	}
}

func collectAllVolumes(ctx context.Context, client *kibotos.Client, base *kibotos.VolumeListParams) (*kibotos.MultiZonePage[kibotos.Volume], error) {
	var p kibotos.VolumeListParams
	if base != nil {
		p = *base
	}
	all := []kibotos.Volume{}
	unavailSeen := make(map[string]struct{})
	unavail := []string{}
	for {
		page, err := client.Volumes.List(ctx, &p)
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
	return &kibotos.MultiZonePage[kibotos.Volume]{
		Data:             all,
		UnavailableZones: unavail,
	}, nil
}
