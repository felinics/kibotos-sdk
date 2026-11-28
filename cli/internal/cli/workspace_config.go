package cli

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) resolveCommandWorkspace(ctx context.Context, ref string, filter func(kibotos.Workspace) bool, emptyMessage, title string) (*kibotos.Client, string, *kibotos.Workspace, error) {
	if strings.TrimSpace(ref) == "" && !a.isInteractive() {
		return nil, "", nil, missingWorkspaceRef()
	}
	client, _, err := a.requireClient()
	if err != nil {
		return nil, "", nil, err
	}
	id, ws, err := a.resolveWorkspace(ctx, client, ref, filter, emptyMessage, title)
	return client, id, ws, err
}

func (a *App) resolveAvailableWorkspace(ctx context.Context, ref string) (*kibotos.Client, string, error) {
	client, id, _, err := a.resolveCommandWorkspace(ctx, ref, func(w kibotos.Workspace) bool {
		return w.Status == kibotos.WorkspaceStatusAvailable
	}, "no available workspaces found", "Select an available workspace:")
	return client, id, err
}

func isAvailableWorkspace(w kibotos.Workspace) bool {
	return w.Status == kibotos.WorkspaceStatusAvailable
}

func stoppedOrAvailableWorkspace(w kibotos.Workspace) bool {
	return stoppedWorkspace(w) || isAvailableWorkspace(w)
}

func stoppedWorkspace(w kibotos.Workspace) bool {
	return w.Status == kibotos.WorkspaceStatusStopped
}

func optionalIntFlag(cmd *cobra.Command, name string, value, low, high int) (*int, error) {
	if !cmd.Flags().Changed(name) {
		return nil, nil
	}
	if value < low || value > high {
		return nil, usageError("--%s must be between %d and %d", name, low, high)
	}
	return &value, nil
}

func workspaceVolumes(ws *kibotos.Workspace) []kibotos.WorkspaceVolume {
	if ws.Volumes == nil && ws.Volume != nil {
		return []kibotos.WorkspaceVolume{*ws.Volume}
	}
	return ws.Volumes
}

func parseCreateVolumes(values []string) ([]kibotos.WorkspaceVolumeInput, string, error) {
	if len(values) == 1 && !strings.Contains(values[0], ":") {
		volume := strings.TrimSpace(values[0])
		if volume == "" {
			return nil, "", usageError("--volume cannot be empty")
		}
		return nil, volume, nil
	}
	volumes, err := parseVolumeBindings(values)
	return volumes, "", err
}

func parseVolumeBindings(values []string) ([]kibotos.WorkspaceVolumeInput, error) {
	volumes := make([]kibotos.WorkspaceVolumeInput, 0, len(values))
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, value := range values {
		id, rawMount, ok := strings.Cut(value, ":")
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			return nil, usageError("invalid volume binding %q: expected vol_id:/path", value)
		}
		mount, err := validateMountPath(rawMount)
		if err != nil {
			return nil, err
		}
		if ids[id] || paths[mount] {
			return nil, usageError("duplicate volume ID or mount path in %q", value)
		}
		ids[id], paths[mount] = true, true
		volumes = append(volumes, kibotos.WorkspaceVolumeInput{ID: id, MountPath: mount})
	}
	return volumes, nil
}

func (a *App) updateWorkspaceVolumeBindings(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, volumeID, mount string, deleteWithWorkspace, restart bool, timeout time.Duration) error {
	volumes := workspaceVolumes(ws)
	bindings := make([]kibotos.WorkspaceVolumeInput, 0, len(volumes)+1)
	found := false
	for _, volume := range volumes {
		if volume.ID == volumeID {
			found = true
			if mount == "" {
				continue
			}
			if mount == volume.MountPath && deleteWithWorkspace == volume.DeleteWithWorkspace {
				return a.renderWorkspace(ws)
			}
			return usageError("volume %s is already attached at %s", volumeID, volume.MountPath)
		}
		if mount != "" && volume.MountPath == mount {
			return usageError("mount path %s is already in use", mount)
		}
		bindings = append(bindings, kibotos.WorkspaceVolumeInput(volume))
	}
	if mount != "" {
		bindings = append(bindings, kibotos.WorkspaceVolumeInput{ID: volumeID, MountPath: mount, DeleteWithWorkspace: deleteWithWorkspace})
	} else if !found {
		return a.renderWorkspace(ws)
	}
	ws, err := a.withStoppedWorkspace(ctx, client, ws, "update volumes", restart, timeout, func() (*kibotos.Workspace, error) {
		return client.Workspaces.SetVolumes(ctx, ws.ID, bindings)
	})
	if err != nil {
		return err
	}
	return a.renderWorkspace(ws)
}

func (a *App) newWorkspaceVolumesCmd() *cobra.Command {
	root := &cobra.Command{Use: "volumes", Short: "Manage a workspace's full volume binding list"}
	var restart bool
	var waitTimeout string
	set := &cobra.Command{
		Use:     "set [workspace] [vol_id:/path...]",
		Short:   "Replace all volume bindings (omit bindings to detach all)",
		Example: "  kibo ws volumes set\n  kibo ws volumes set wks_123 vol_123:/data --restart --no-input --json",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			volumes, err := parseVolumeBindings(args[min(len(args), 1):])
			if err != nil {
				return err
			}
			timeout, err := transitionTimeout(waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, ws, err := a.resolveCommandWorkspace(ctx, firstArg(args), stoppedOrAvailableWorkspace,
				"no running or stopped workspaces found", "Select a running or stopped workspace:")
			if err != nil {
				return err
			}
			ws, err = workspaceState(ctx, client, id, ws, restart || a.isInteractive())
			if err != nil {
				return err
			}
			action := "update volumes"
			if len(volumes) == 0 {
				action = "detach all volumes"
				question := fmt.Sprintf("Detach all volumes from %s?", resourceLabel(id, ws.Name))
				if err := a.confirmVolumeRemoval(ws, restart, question); err != nil {
					return err
				}
			}
			ws, err = a.withStoppedWorkspace(ctx, client, ws, action, restart, timeout, func() (*kibotos.Workspace, error) {
				return client.Workspaces.SetVolumes(ctx, id, volumes)
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
	addVolumeRestartFlags(set, &restart, &waitTimeout)
	root.AddCommand(set)
	return root
}

func addVolumeRestartFlags(cmd *cobra.Command, restart *bool, waitTimeout *string) {
	cmd.Flags().BoolVar(restart, "restart", false, "If the workspace is running, stop it, update volumes, and start it again")
	addTransitionTimeout(cmd, waitTimeout)
}

func (a *App) confirmVolumeRemoval(ws *kibotos.Workspace, restart bool, question string) error {
	// The transition confirmation includes the removal when both need consent.
	if isAvailableWorkspace(*ws) && !restart && a.isInteractive() {
		return nil
	}
	ok, err := a.confirmDestructive(question)
	if err != nil {
		return err
	}
	if !ok {
		return prompt.ErrCancelled
	}
	return nil
}

func validatePacks(packs []string) error {
	if len(packs) > 4 {
		return usageError("a workspace can bind at most four Packs")
	}
	seen := make(map[string]bool, len(packs))
	for _, pack := range packs {
		if strings.TrimSpace(pack) == "" || seen[pack] {
			return usageError("Pack IDs must be nonempty and unique")
		}
		seen[pack] = true
	}
	return nil
}

func splitPackCSV(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

func validatePackCSV(value string) error {
	return validatePacks(splitPackCSV(value))
}

func (a *App) newWorkspaceResizeCmd() *cobra.Command {
	var resources kibotos.WorkspaceResourcesInput
	var restart bool
	var waitTimeout string
	cmd := &cobra.Command{
		Use:     "resize [id-or-name]",
		Short:   "Change CPU or memory or grow the root disk of a workspace",
		Example: "  kibo ws resize\n  kibo ws resize wks_123 --vcpus 4 --restart --no-input --json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hasResources := cmd.Flags().Changed("vcpus") || cmd.Flags().Changed("memory-mib") || cmd.Flags().Changed("disk-mib")
			if !hasResources && !a.isInteractive() {
				return usageErrorWithHint("Pass at least one of --vcpus, --memory-mib or --disk-mib.", "no resources specified")
			}
			if cmd.Flags().Changed("vcpus") {
				if err := validateVCPUs(resources.VCPUs); err != nil {
					return err
				}
			}
			if _, err := optionalIntFlag(cmd, "memory-mib", resources.MemoryMiB, minMemoryMiB, maxMemoryMiB); err != nil {
				return err
			}
			if _, err := optionalIntFlag(cmd, "disk-mib", resources.DiskMiB, minDiskMiB, maxDiskMiB); err != nil {
				return err
			}
			timeout, err := transitionTimeout(waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, picked, err := a.resolveCommandWorkspace(ctx, firstArg(args), func(w kibotos.Workspace) bool {
				return stoppedOrAvailableWorkspace(w) || w.Status == kibotos.WorkspaceStatusFailed
			}, "no running, stopped, or failed workspaces found to resize", "Select a workspace to resize:")
			if err != nil {
				return err
			}
			picked, err = workspaceState(ctx, client, id, picked, restart || a.isInteractive())
			if err != nil {
				return err
			}
			request := resources
			if !hasResources {
				request, err = a.promptResize(picked.Resources)
				if err != nil {
					return err
				}
			}
			ws, err := a.withStoppedWorkspace(ctx, client, picked, "resize", restart, timeout, func() (*kibotos.Workspace, error) {
				return client.Workspaces.Update(ctx, id, kibotos.UpdateWorkspaceRequest{Resources: &request})
			})
			if err != nil {
				return err
			}
			return a.renderWorkspace(ws)
		},
	}
	cmd.Flags().IntVar(&resources.VCPUs, "vcpus", 0, "New vCPU count")
	cmd.Flags().IntVar(&resources.MemoryMiB, "memory-mib", 0, "New memory size in MiB")
	cmd.Flags().IntVar(&resources.DiskMiB, "disk-mib", 0, "New root disk size in MiB")
	cmd.Flags().BoolVar(&restart, "restart", false, "If the workspace is running, stop it, apply the resize, and start it again")
	addTransitionTimeout(cmd, &waitTimeout)
	return cmd
}

func (a *App) promptResize(current kibotos.WorkspaceResources) (kibotos.WorkspaceResourcesInput, error) {
	var changes kibotos.WorkspaceResourcesInput
	var err error
	changes.VCPUs, err = a.promptResourceChange("vCPUs", current.VCPUs, minVCPUs, maxVCPUs)
	if err != nil {
		return changes, err
	}
	changes.MemoryMiB, err = a.promptResourceChange("Memory (MiB)", current.MemoryMiB, minMemoryMiB, maxMemoryMiB)
	if err != nil {
		return changes, err
	}
	changes.DiskMiB, err = a.promptResourceChange("Root disk (MiB, grow only)", current.DiskMiB, max(minDiskMiB, current.DiskMiB), maxDiskMiB)
	if err != nil {
		return changes, err
	}
	if changes == (kibotos.WorkspaceResourcesInput{}) {
		return changes, usageError("no resource changes specified")
	}
	return changes, nil
}

func (a *App) promptResourceChange(label string, current, minimum, maximum int) (int, error) {
	input, err := a.prompter().Input(label, strconv.Itoa(current), func(raw string) error {
		value, err := strconv.Atoi(raw)
		if err == nil && value == current {
			return nil
		}
		if err != nil || value < minimum || value > maximum {
			return fmt.Errorf("enter an integer between %d and %d", minimum, maximum)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	value, _ := strconv.Atoi(input)
	if value == current {
		return 0, nil
	}
	return value, nil
}

func (a *App) newWorkspaceMetricsCmd() *cobra.Command {
	var start, end string
	cmd := &cobra.Command{
		Use:   "metrics [id-or-name]",
		Short: "Read host and guest resource metrics",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var options kibotos.WorkspaceMetricsOptions
			var err error
			if cmd.Flags().Changed("start") {
				options.Start, err = time.Parse(time.RFC3339, start)
				if err != nil {
					return usageError("--start must be an RFC3339 timestamp")
				}
			}
			if cmd.Flags().Changed("end") {
				options.End, err = time.Parse(time.RFC3339, end)
				if err != nil {
					return usageError("--end must be an RFC3339 timestamp")
				}
			}
			if !options.Start.IsZero() && !options.End.IsZero() && !options.Start.Before(options.End) {
				return usageError("--start must be before --end")
			}
			ctx := cmd.Context()
			client, id, _, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "")
			if err != nil {
				return err
			}
			metrics, err := client.Workspaces.GetMetrics(ctx, id, &options)
			if err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(metrics)
			}
			if p.IsQuiet() {
				return p.ID(id)
			}
			if err := p.Details([]output.KV{{Key: "Step (seconds)", Value: fmt.Sprint(metrics.StepSeconds)}, {Key: "Earliest available", Value: optionalFileTime(metrics.EarliestTime)}}); err != nil {
				return err
			}
			rows := make([][]string, 0, len(metrics.Host)+len(metrics.Guest))
			for _, m := range metrics.Host {
				rows = append(rows, []string{m.Time.Format(time.RFC3339), "host", optionalMetric(m.CPUCores), optionalMetric(m.MemoryBytes), "-"})
			}
			for _, m := range metrics.Guest {
				rows = append(rows, []string{m.Time.Format(time.RFC3339), "guest", fmt.Sprint(m.CPUUsedPercent), fmt.Sprint(m.MemoryUsedBytes), fmt.Sprint(m.DiskUsedBytes)})
			}
			return p.Table([]string{"TIME", "SOURCE", "CPU (HOST CORES/GUEST %)", "MEMORY BYTES", "DISK USED BYTES"}, rows)
		},
	}
	cmd.Flags().StringVar(&start, "start", "", "Interval start in RFC3339 format")
	cmd.Flags().StringVar(&end, "end", "", "Interval end in RFC3339 format")
	return cmd
}

func optionalMetric[T int64 | float64](value *T) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprint(*value)
}

func networkApplied(ws *kibotos.Workspace) bool {
	return ws.Network.Applied != nil && *ws.Network.Applied
}

func sameNetworkPolicy(left, right kibotos.WorkspaceNetwork) bool {
	return left.Mode == right.Mode &&
		slices.Equal(left.Allow, right.Allow) &&
		slices.Equal(left.Deny, right.Deny) &&
		slices.Equal(left.Resolvers, right.Resolvers) &&
		slices.EqualFunc(left.Egresses, right.Egresses, func(a, b kibotos.NetworkEgressRule) bool {
			return a.EgressID == b.EgressID && slices.Equal(a.Destinations, b.Destinations)
		})
}

func parseWorkspaceNetwork(allow, deny, egresses, resolvers []string) (kibotos.WorkspaceNetworkInput, error) {
	params := kibotos.WorkspaceNetworkInput{Allow: allow, Deny: deny}
	if err := validateNetworkAddresses(allow, "--allow"); err != nil {
		return params, err
	}
	if err := validateNetworkAddresses(deny, "--deny"); err != nil {
		return params, err
	}
	if len(egresses) > 1 || len(resolvers) > 1 {
		return params, usageError("a policy supports at most one --egress and one --resolver")
	}
	if len(egresses) == 1 {
		id, rawDestinations, ok := strings.Cut(egresses[0], "=")
		if !ok || id == "" || rawDestinations == "" {
			return params, usageError("--egress must be egress_id=IP_OR_CIDR[,IP_OR_CIDR...]")
		}
		destinations := strings.Split(rawDestinations, ",")
		if len(destinations) > 256 {
			return params, usageError("--egress accepts up to 256 destinations")
		}
		if err := validateNetworkAddresses(destinations, "--egress"); err != nil {
			return params, err
		}
		params.Egresses = []kibotos.NetworkEgressRule{{EgressID: id, Destinations: destinations}}
	}
	if len(resolvers) == 1 {
		id, egress, useEgress := strings.Cut(resolvers[0], "=")
		if id == "" {
			return params, usageError("--resolver requires a resolver ID")
		}
		path := kibotos.NetworkResolverPath{Type: "direct"}
		if useEgress {
			if len(params.Egresses) == 0 || params.Egresses[0].EgressID != egress {
				return params, usageError("--resolver must use the egress selected by --egress")
			}
			path = kibotos.NetworkResolverPath{Type: "egress", EgressID: egress}
		}
		params.Resolvers = []kibotos.NetworkResolverRule{{ResolverID: id, Path: path}}
	}
	return params, nil
}

func validateNetworkAddresses(values []string, flag string) error {
	for _, value := range values {
		if ip, err := netip.ParseAddr(value); err == nil && ip.Zone() == "" {
			continue
		}
		if _, err := netip.ParsePrefix(value); err != nil {
			return usageError("%s requires IP addresses or CIDRs, got %q", flag, value)
		}
	}
	return nil
}
