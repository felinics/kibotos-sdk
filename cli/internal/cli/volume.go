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
			if cmd.Flags().Changed("limit") && limit <= 0 {
				return usageError("--limit must be greater than zero")
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
					string(v.Status),
					wsID,
				})
			}
			return p.Table([]string{"ID", "NAME", "ZONE", "SIZE (GiB)", "STATUS", "WORKSPACE"}, rows)
		},
	}

	f := cmd.Flags()
	f.StringVar(&zone, "zone", "", "Filter by zone ID")
	f.StringVar(&attached, "attached", "", "Filter by attachment state: true or false")
	f.StringArrayVar(&labels, "label", nil, "Filter by label (key=value, repeatable)")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newVolumeGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get details of a persistent volume",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			vol, err := client.Volumes.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.renderVolume(vol)
		},
	}
}

func (a *App) newVolumeCreateCmd() *cobra.Command {
	var (
		sizeGiB     int
		name        string
		zone        string
		labels      []string
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new persistent volume",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("size") || sizeGiB <= 0 {
				return usageError("--size is required and must be greater than zero (GiB)")
			}
			parsedLabels, err := parseLabels(labels)
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
			vol, err := client.Volumes.Create(ctx, kibotos.CreateVolumeParams{
				Name:    strings.TrimSpace(name),
				Zone:    strings.TrimSpace(zone),
				SizeGiB: sizeGiB,
				Labels:  parsedLabels,
			})
			if err != nil {
				return err
			}

			if wait && vol.Status != kibotos.VolumeStatusAvailable {
				a.printer().Statusf("Waiting for volume %s to reach available...", vol.ID)
				vol, err = client.Volumes.Wait(ctx, vol.ID, kibotos.WaitVolumeParams{
					Statuses: []kibotos.VolumeStatus{kibotos.VolumeStatusAvailable},
					Timeout:  time.Duration(waitTimeout) * time.Second,
				})
				if err != nil {
					return err
				}
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.IntVar(&sizeGiB, "size", 0, "Volume size in GiB (required)")
	f.StringVar(&name, "name", "", "Human-readable volume name")
	f.StringVar(&zone, "zone", "", "Target zone ID")
	f.StringArrayVar(&labels, "label", nil, "Volume label (key=value, repeatable)")
	f.BoolVar(&wait, "wait", false, "Wait until the volume reaches available")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
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
		Use:   "update <id>",
		Short: "Update a volume's name or labels",
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

			params := kibotos.UpdateVolumeParams{
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
			vol, err := client.Volumes.Update(cmd.Context(), args[0], params)
			if err != nil {
				return err
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringVar(&name, "name", "", "Set volume name")
	f.BoolVar(&clearName, "clear-name", false, "Clear the volume name (set to null)")
	f.StringArrayVar(&labels, "label", nil, "Set a label (key=value, repeatable)")
	f.StringArrayVar(&unsetLabels, "unset-label", nil, "Remove a label by key (repeatable)")
	return cmd
}

func (a *App) newVolumeResizeCmd() *cobra.Command {
	var (
		sizeGiB     int
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "resize <id>",
		Short: "Grow a persistent volume to a larger size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("size") || sizeGiB <= 0 {
				return usageError("--size is required and must be greater than zero (GiB)")
			}
			if wait && waitTimeout <= 0 {
				return usageError("--wait-timeout must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			vol, err := client.Volumes.Resize(ctx, args[0], kibotos.ResizeVolumeParams{
				SizeGiB: sizeGiB,
			})
			if err != nil {
				return err
			}

			if wait && vol.Status != kibotos.VolumeStatusAvailable && vol.Status != kibotos.VolumeStatusAttached {
				a.printer().Statusf("Waiting for volume %s resize to complete...", vol.ID)
				vol, err = client.Volumes.Wait(ctx, vol.ID, kibotos.WaitVolumeParams{
					Statuses: []kibotos.VolumeStatus{kibotos.VolumeStatusAvailable, kibotos.VolumeStatusAttached},
					Timeout:  time.Duration(waitTimeout) * time.Second,
				})
				if err != nil {
					return err
				}
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.IntVar(&sizeGiB, "size", 0, "New volume size in GiB (required)")
	f.BoolVar(&wait, "wait", false, "Wait until the resize finishes")
	f.IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newVolumeDeleteCmd() *cobra.Command {
	var (
		wait        bool
		waitTimeout int
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a persistent volume",
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
			vol, err := client.Volumes.Delete(ctx, args[0])
			if err != nil {
				return err
			}

			if wait {
				a.printer().Statusf("Waiting for volume %s to be deleted...", vol.ID)
				vol, err = a.waitVolumeDeleted(ctx, client, vol, time.Duration(waitTimeout)*time.Second)
				if err != nil {
					return err
				}
			}
			return a.renderVolume(vol)
		},
	}

	cmd.Flags().BoolVar(&wait, "wait", false, "Wait until volume deletion completes")
	cmd.Flags().IntVar(&waitTimeout, "wait-timeout", defaultWaitTimeoutSeconds, "Maximum seconds to wait when --wait is set")
	return cmd
}

func (a *App) newVolumeWaitCmd() *cobra.Command {
	var (
		statuses []string
		timeout  int
	)

	cmd := &cobra.Command{
		Use:   "wait <id>",
		Short: "Wait until a volume reaches one of the target statuses",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(statuses) == 0 {
				return usageError("--status is required (e.g. --status available,failed)")
			}
			if timeout <= 0 {
				return usageError("--timeout must be greater than zero")
			}
			parsed := make([]kibotos.VolumeStatus, 0, len(statuses))
			for _, raw := range statuses {
				s := strings.TrimSpace(raw)
				if s == "" {
					continue
				}
				parsed = append(parsed, kibotos.VolumeStatus(s))
			}
			if len(parsed) == 0 {
				return usageError("--status must include at least one non-empty status")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			vol, err := client.Volumes.Wait(cmd.Context(), args[0], kibotos.WaitVolumeParams{
				Statuses: parsed,
				Timeout:  time.Duration(timeout) * time.Second,
			})
			if err != nil {
				return err
			}
			return a.renderVolume(vol)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&statuses, "status", nil, "Target status(es), comma-separated or repeatable (required)")
	f.IntVar(&timeout, "timeout", defaultWaitCmdTimeoutSeconds, "Maximum seconds to wait")
	return cmd
}

func (a *App) renderVolume(vol *kibotos.Volume) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(vol)
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
		{Key: "Status", Value: string(vol.Status)},
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
