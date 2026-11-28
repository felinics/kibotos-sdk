package cli

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newWorkspaceNetworkCmd() *cobra.Command {
	var (
		mode                             string
		allow, deny, egresses, resolvers []string
		drain                            int
		wait                             bool
		noWait                           bool
		waitTimeout                      string
	)

	cmd := &cobra.Command{
		Use:   "network [id-or-name]",
		Short: "Update a workspace's outbound network policy",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				parsedMode kibotos.NetworkMode
				err        error
			)
			if cmd.Flags().Changed("mode") {
				parsedMode, err = parseNetworkMode(mode)
				if err != nil {
					return err
				}
			} else if !a.isInteractive() {
				return missingFlagError("--mode", "Pass --mode public_internet or --mode deny_all.")
			}

			params, err := parseWorkspaceNetwork(allow, deny, egresses, resolvers)
			if err != nil {
				return err
			}
			validateMode := func(mode kibotos.NetworkMode) error {
				if mode == kibotos.NetworkModeDenyAll && len(allow)+len(deny)+len(egresses)+len(resolvers) != 0 {
					return usageError("deny_all requires empty allow, deny, egress and resolver lists")
				}
				return nil
			}
			if err := validateMode(parsedMode); err != nil {
				return err
			}
			params.DrainSeconds, err = optionalIntFlag(cmd, "drain-seconds", drain, 0, 3600)
			if err != nil {
				return err
			}
			wait, _ := cmd.Flags().GetBool("wait")
			noWait, _ := cmd.Flags().GetBool("no-wait")
			waitTimeout, _ := cmd.Flags().GetString("wait-timeout")
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "Select a workspace to update network policy:")
			if err != nil {
				return err
			}

			if parsedMode == "" {
				choice, err := a.prompter().Select("Outbound network mode:", "Mode", []prompt.Option{
					{Value: string(kibotos.NetworkModePublicInternet), Label: "public_internet", Description: "Allow outbound internet access"},
					{Value: string(kibotos.NetworkModeDenyAll), Label: "deny_all", Description: "Block all outbound internet traffic"},
				}, 0)
				if err != nil {
					return err
				}
				parsedMode = kibotos.NetworkMode(choice.Value)
				if err := validateMode(parsedMode); err != nil {
					return err
				}
			}

			params.Mode = parsedMode
			return a.setWorkspaceNetwork(ctx, client, wsID, params, shouldWait, waitDur)
		},
	}

	f := cmd.Flags()
	f.StringVar(&mode, "mode", "", "Outbound network mode: public_internet or deny_all")
	f.StringArrayVar(&allow, "allow", nil, "Allowed outbound IP or CIDR (repeatable)")
	f.StringArrayVar(&deny, "deny", nil, "Denied outbound IP or CIDR (repeatable)")
	f.StringArrayVar(&egresses, "egress", nil, "Egress rule as egress_id=IP_OR_CIDR[,IP_OR_CIDR...]")
	f.StringArrayVar(&resolvers, "resolver", nil, "Resolver as resolver_id (direct) or resolver_id=egress_id")
	f.IntVar(&drain, "drain-seconds", 0, "Seconds to drain connections on the previous egress path (0–3600)")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the network policy is applied")
	for _, child := range []*cobra.Command{
		a.newWorkspaceNetworkAddressesCmd(true), a.newWorkspaceNetworkAddressesCmd(false),
		a.newWorkspaceNetworkEgressCmd(), a.newWorkspaceNetworkResolverCmd(),
	} {
		preserveNetworkWorkspaceName(cmd, child)
		cmd.AddCommand(child)
	}
	return cmd
}

// A workspace can have the same name as an incremental subcommand. A full
// policy flag disambiguates the legacy form, such as network allow --mode deny_all.
func preserveNetworkWorkspaceName(parent, child *cobra.Command) {
	names := []string{"mode", "allow", "deny", "egress", "resolver", "drain-seconds"}
	for _, name := range names {
		flag := *parent.Flags().Lookup(name)
		flag.Hidden = true
		child.Flags().AddFlag(&flag)
	}
	run := child.RunE
	child.RunE = func(cmd *cobra.Command, args []string) error {
		for _, name := range names {
			if !cmd.Flags().Changed(name) {
				continue
			}
			if len(args) != 0 || cmd.Flags().Changed("remove") || cmd.Flags().Changed("clear") || cmd.Flags().Changed("via-egress") {
				return usageError("full network policy flags cannot be combined with incremental network arguments or flags")
			}
			return parent.RunE(cmd, []string{child.Name()})
		}
		return run(cmd, args)
	}
}

type workspaceNetworkEdit struct {
	ref    string
	adding bool
	apply  func(context.Context, *kibotos.Client, *kibotos.Workspace, *kibotos.WorkspaceNetworkInput) error
}

func (a *App) configureNetworkEdit(cmd *cobra.Command, parse func([]string) (workspaceNetworkEdit, error)) {
	var wait, noWait bool
	var waitTimeout string
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the network policy is applied")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		edit, err := parse(args)
		if err != nil {
			return err
		}
		shouldWait, timeout, err := resolveWait(cmd, wait, noWait, waitTimeout)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("wait-timeout") && !shouldWait {
			if _, err := transitionTimeout(waitTimeout); err != nil {
				return err
			}
		}
		ctx := cmd.Context()
		client, id, _, err := a.resolveCommandWorkspace(ctx, edit.ref, nil, "", "Select a workspace to update network policy:")
		if err != nil {
			return err
		}
		ws, err := client.Workspaces.Get(ctx, id)
		if err != nil {
			return err
		}
		policy := ws.Network
		if edit.adding && policy.Mode == kibotos.NetworkModeDenyAll {
			return &CLIError{
				ExitCode: ExitError,
				Code:     "invalid_state",
				Message:  "cannot add network rules in deny_all mode",
				Hint:     fmt.Sprintf("Switch to public_internet first with `kibo ws network %s --mode public_internet`.", id),
			}
		}
		params := kibotos.WorkspaceNetworkInput{
			Mode: policy.Mode, Allow: policy.Allow, Deny: policy.Deny,
			Egresses: policy.Egresses, Resolvers: policy.Resolvers,
		}
		if err := edit.apply(ctx, client, ws, &params); err != nil {
			return err
		}
		return a.setWorkspaceNetwork(ctx, client, id, params, shouldWait, timeout)
	}
}

func (a *App) setWorkspaceNetwork(ctx context.Context, client *kibotos.Client, id string, params kibotos.WorkspaceNetworkInput, shouldWait bool, timeout time.Duration) error {
	ws, err := client.Workspaces.SetNetwork(ctx, id, params)
	if err != nil {
		return err
	}
	if shouldWait && ws.Status != kibotos.WorkspaceStatusStopped && !networkApplied(ws) {
		stopSpin := a.printer().StartSpinner(fmt.Sprintf("Waiting for network policy %q to apply on workspace %s...", params.Mode, id))
		ws, err = a.waitWorkspaceNetworkApplied(ctx, client, id, ws.Network, timeout)
		if err != nil {
			stopSpin(fmt.Sprintf("Failed applying network policy on %s", id), false)
			return err
		}
		stopSpin(fmt.Sprintf("Network policy %q applied on %s", params.Mode, id), true)
	}
	return a.renderWorkspace(ws)
}

func (a *App) newWorkspaceNetworkAddressesCmd(allow bool) *cobra.Command {
	name := "deny"
	if allow {
		name = "allow"
	}
	var remove bool
	cmd := &cobra.Command{
		Use:     name + " [workspace] <ip-or-cidr...>",
		Short:   "Add or remove addresses in the outbound " + name + " list",
		Example: fmt.Sprintf("  kibo ws network %[1]s 203.0.113.0/24\n  kibo ws network %[1]s wks_123 203.0.113.0/24 --no-input --json", name),
		Args:    cobra.ArbitraryArgs,
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the given addresses")
	a.configureNetworkEdit(cmd, func(args []string) (workspaceNetworkEdit, error) {
		var edit workspaceNetworkEdit
		if len(args) > 0 && (!a.isInteractive() || validateNetworkAddresses(args[:1], name) != nil) {
			edit.ref, args = args[0], args[1:]
		}
		if strings.TrimSpace(edit.ref) == "" && !a.isInteractive() {
			return edit, missingWorkspaceRef()
		}
		if len(args) == 0 {
			return edit, usageErrorWithHint("Pass one or more IP addresses or CIDRs, for example 203.0.113.0/24.", "network %s requires at least one IP address or CIDR", name)
		}
		if err := validateNetworkAddresses(args, name); err != nil {
			return edit, err
		}
		edit.adding = !remove
		edit.apply = func(_ context.Context, _ *kibotos.Client, _ *kibotos.Workspace, params *kibotos.WorkspaceNetworkInput) error {
			target := &params.Deny
			if allow {
				target = &params.Allow
			}
			*target = editNetworkAddresses(*target, args, remove)
			return nil
		}
		return edit, nil
	})
	return cmd
}

func editNetworkAddresses(current, addresses []string, remove bool) []string {
	seen := make(map[string]bool, len(addresses)+len(current))
	next := make([]string, 0, len(current)+len(addresses))
	if remove {
		for _, address := range addresses {
			seen[networkAddressKey(address)] = true
		}
		for _, address := range current {
			if !seen[networkAddressKey(address)] {
				next = append(next, address)
			}
		}
		return next
	}
	for _, list := range [][]string{current, addresses} {
		for _, address := range list {
			key := networkAddressKey(address)
			if !seen[key] {
				next = append(next, address)
				seen[key] = true
			}
		}
	}
	return next
}

func networkAddressKey(value string) string {
	if ip, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(ip, ip.BitLen()).String()
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().String()
	}
	return value
}

func (a *App) newWorkspaceNetworkEgressCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:     "use-egress [workspace] [egress] [dest-cidr...]",
		Short:   "Set or clear the workspace egress rule",
		Example: "  kibo ws network use-egress\n  kibo ws network use-egress wks_123 egr_123 0.0.0.0/0 --no-input --json",
		Args:    cobra.ArbitraryArgs,
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear the egress rule")
	a.configureNetworkEdit(cmd, func(args []string) (workspaceNetworkEdit, error) {
		edit := workspaceNetworkEdit{ref: firstArg(args), adding: !clear}
		resourceArgs := args[min(len(args), 1):]
		if clear && len(resourceArgs) > 0 {
			return edit, usageError("--clear cannot be combined with an egress or destination CIDRs")
		}
		if !a.isInteractive() && strings.TrimSpace(edit.ref) == "" {
			return edit, missingWorkspaceRef()
		}
		ref := strings.TrimSpace(firstArg(resourceArgs))
		destinations := resourceArgs[min(len(resourceArgs), 1):]
		if !clear && ref == "" && (len(resourceArgs) > 0 || !a.isInteractive()) {
			return edit, missingNetworkRef("egress")
		}
		if !clear && len(destinations) == 0 && !a.isInteractive() {
			return edit, usageErrorWithHint("Pass destination CIDRs, for example 0.0.0.0/0.", "network use-egress requires at least one destination CIDR")
		}
		if err := validateEgressDestinations(destinations); err != nil {
			return edit, err
		}
		edit.apply = func(ctx context.Context, client *kibotos.Client, _ *kibotos.Workspace, params *kibotos.WorkspaceNetworkInput) error {
			if clear {
				params.Egresses = nil
				return nil
			}
			id, _, err := a.resolveEgress(ctx, client, ref, "Select an egress:")
			if err != nil {
				return err
			}
			if len(destinations) == 0 {
				raw, err := a.prompter().Input("Destination CIDRs (comma-separated, e.g. 0.0.0.0/0)", "0.0.0.0/0", func(raw string) error {
					return validateEgressDestinations(splitDestinationCIDRs(raw))
				})
				if err != nil {
					return err
				}
				destinations = splitDestinationCIDRs(raw)
			}
			params.Egresses = []kibotos.NetworkEgressRule{{EgressID: id, Destinations: destinations}}
			return nil
		}
		return edit, nil
	})
	return cmd
}

func splitDestinationCIDRs(raw string) []string {
	values := strings.Split(raw, ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
	}
	return values
}

func validateEgressDestinations(values []string) error {
	if len(values) > 256 {
		return usageError("network use-egress accepts up to 256 destinations")
	}
	for _, value := range values {
		if _, err := netip.ParsePrefix(value); err != nil {
			return usageError("invalid destination CIDR %q", value)
		}
	}
	return nil
}

func (a *App) newWorkspaceNetworkResolverCmd() *cobra.Command {
	var clear, viaEgress bool
	cmd := &cobra.Command{
		Use:     "use-resolver [workspace] [resolver]",
		Short:   "Set or clear the workspace DNS resolver rule",
		Example: "  kibo ws network use-resolver\n  kibo ws network use-resolver wks_123 rsv_123 --via-egress --no-input --json",
		Args:    cobra.MaximumNArgs(2),
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "Clear the resolver rule")
	cmd.Flags().BoolVar(&viaEgress, "via-egress", false, "Route DNS through the workspace's configured egress")
	a.configureNetworkEdit(cmd, func(args []string) (workspaceNetworkEdit, error) {
		edit := workspaceNetworkEdit{ref: firstArg(args), adding: !clear}
		if clear && (viaEgress || len(args) > 1) {
			return edit, usageError("--clear cannot be combined with a resolver or --via-egress")
		}
		if strings.TrimSpace(edit.ref) == "" && !a.isInteractive() {
			return edit, missingWorkspaceRef()
		}
		var ref string
		if len(args) > 1 {
			ref = strings.TrimSpace(args[1])
		}
		if !clear && ref == "" && (len(args) > 1 || !a.isInteractive()) {
			return edit, missingNetworkRef("resolver")
		}
		edit.apply = func(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, params *kibotos.WorkspaceNetworkInput) error {
			if clear {
				params.Resolvers = nil
				return nil
			}
			route := kibotos.NetworkResolverPath{Type: "direct"}
			if viaEgress {
				if len(params.Egresses) != 1 {
					return usageErrorWithHint(fmt.Sprintf("Configure one egress first with `kibo ws network use-egress %s <egress> <dest-cidr>`.", ws.ID), "--via-egress requires one configured workspace egress")
				}
				route = kibotos.NetworkResolverPath{Type: "egress", EgressID: params.Egresses[0].EgressID}
			}
			id, _, err := a.resolveResolver(ctx, client, ref, "Select a resolver:")
			if err != nil {
				return err
			}
			params.Resolvers = []kibotos.NetworkResolverRule{{ResolverID: id, Path: route}}
			return nil
		}
		return edit, nil
	})
	return cmd
}
