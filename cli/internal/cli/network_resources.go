package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

type networkMetadataFlags struct {
	name, zone    string
	labels, unset []string
	clearName     bool
}

func (f *networkMetadataFlags) add(cmd *cobra.Command, create bool) {
	flags := cmd.Flags()
	flags.StringVarP(&f.name, "name", "n", "", "Display name")
	flags.StringArrayVarP(&f.labels, "label", "l", nil, "Set a label (key=value, repeatable)")
	if create {
		flags.StringVarP(&f.zone, "zone", "z", "", "Availability zone")
	} else {
		flags.BoolVar(&f.clearName, "clear-name", false, "Clear the display name")
		flags.StringArrayVar(&f.unset, "unset-label", nil, "Remove a label (repeatable)")
	}
}

func (f networkMetadataFlags) create(cmd *cobra.Command) (map[string]string, error) {
	if err := validateNonEmptyFlag(cmd, "zone", f.zone); err != nil {
		return nil, err
	}
	if _, err := validateResourceName(f.name, true); err != nil {
		return nil, err
	}
	return parseLabels(f.labels)
}

func (f networkMetadataFlags) update(cmd *cobra.Command) (kibotos.NetworkResourceUpdate, error) {
	params := kibotos.NetworkResourceUpdate{}
	if cmd.Flags().Changed("name") {
		if f.clearName {
			return params, usageError("--name and --clear-name cannot be combined")
		}
		if _, err := validateResourceName(f.name, true); err != nil {
			return params, err
		}
		params.Name = kibotos.Some(f.name)
	} else if f.clearName {
		params.Name = kibotos.Null[string]()
	}
	var err error
	params.Labels, err = parseUpdateLabels(f.labels, f.unset)
	return params, err
}

const networkMetadataHint = "Pass --name, --clear-name, --label, or --unset-label to update metadata."

func (a *App) networkUpdateMode(cmd *cobra.Command, configFlags ...string) (replace, rename bool, err error) {
	flags := cmd.Flags()
	metadata := flags.Changed("name") || flags.Changed("clear-name") || flags.Changed("label") || flags.Changed("unset-label")
	for _, name := range configFlags {
		replace = replace || flags.Changed(name)
	}
	switch {
	case metadata && replace:
		return false, false, usageError("update configuration and metadata in separate commands")
	case !metadata && !replace && !a.isInteractive():
		return false, false, usageErrorWithHint(networkMetadataHint, "specify configuration or metadata flags to update")
	default:
		return replace, !metadata && !replace, nil
	}
}

func (a *App) promptNetworkRename(kind, current, config string) (kibotos.NetworkResourceUpdate, error) {
	name, err := a.prompter().Input("New "+kind+" name", current, validateOptionalResourceName)
	if err != nil {
		return kibotos.NetworkResourceUpdate{}, err
	}
	if name == "" || name == current {
		return kibotos.NetworkResourceUpdate{}, usageError("no changes specified; pass --name, --clear-name, --label, --unset-label, or %s config flags", config)
	}
	return kibotos.NetworkResourceUpdate{Name: kibotos.Some(name)}, nil
}

type egressFlags struct {
	address, username, password, passwordEnv, serverName, caPath string
	tls, udp                                                     bool
	drain                                                        int
}

func (f *egressFlags) add(cmd *cobra.Command, create bool) {
	flags := cmd.Flags()
	flags.StringVar(&f.address, "address", "", "SOCKS5 host:port")
	flags.StringVar(&f.username, "username", "", "SOCKS5 username")
	flags.StringVar(&f.password, "password", "", "SOCKS5 password (prefer --password-env)")
	flags.StringVar(&f.passwordEnv, "password-env", "", "Read the SOCKS5 password from this environment variable")
	flags.BoolVar(&f.tls, "tls", false, "Connect to the SOCKS5 endpoint over TLS")
	flags.StringVar(&f.serverName, "server-name", "", "TLS server name for SNI and certificate verification")
	flags.StringVar(&f.caPath, "ca-cert", "", "Path to a PEM file with TLS trust roots")
	flags.BoolVar(&f.udp, "udp-enabled", false, "Allow UDP DNS through this egress")
	if !create {
		flags.IntVar(&f.drain, "drain-seconds", 0, "Seconds to drain established connections (0–3600)")
	}
}

func validateHostPort(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || validatePort(port) != nil {
		return usageError("--address requires a SOCKS5 host:port")
	}
	return nil
}

func (f egressFlags) config(cmd *cobra.Command, env func(string) string) (kibotos.EgressSocks5Input, error) {
	config := kibotos.EgressSocks5Input{Address: f.address}
	if f.address != "" || cmd.Flags().Changed("address") {
		if err := validateHostPort(f.address); err != nil {
			return config, err
		}
	}
	flags := cmd.Flags()
	if flags.Changed("username") {
		config.Username = &f.username
	}
	if flags.Changed("password") && flags.Changed("password-env") {
		return config, usageError("--password and --password-env cannot be combined")
	}
	if flags.Changed("password-env") {
		secret := env(f.passwordEnv)
		if secret == "" {
			return config, usageError("--password-env must name a nonempty environment variable")
		}
		config.Password = &secret
	} else if flags.Changed("password") {
		config.Password = &f.password
	}
	if f.tls || flags.Changed("server-name") || flags.Changed("ca-cert") {
		config.TLS = &kibotos.EgressTLSInput{}
		if flags.Changed("server-name") {
			config.TLS.ServerName = &f.serverName
		}
		if flags.Changed("ca-cert") {
			file, err := os.Open(f.caPath)
			if err != nil {
				return config, err
			}
			defer file.Close()
			const maxCA = 64 << 10
			pem, err := io.ReadAll(io.LimitReader(file, maxCA+1))
			if err != nil {
				return config, err
			}
			if len(pem) == 0 || len(pem) > maxCA {
				return config, usageError("--ca-cert requires a PEM file of at most 64 KiB")
			}
			config.TLS.CACert = kibotos.Ptr(string(pem))
		}
	}
	return config, nil
}

func (a *App) newEgressCmd() *cobra.Command {
	root := &cobra.Command{Use: "egress", Aliases: []string{"egresses"}, Short: "Manage SOCKS5 egresses for workspaces"}
	var createMeta, updateMeta networkMetadataFlags
	var createFlags, updateFlags egressFlags
	create := &cobra.Command{Use: "create", Short: "Create a SOCKS5 egress", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			labels, err := createMeta.create(cmd)
			if err != nil {
				return err
			}
			config, err := createFlags.config(cmd, a.envLookup)
			if err != nil {
				return err
			}
			name := createMeta.name
			if config.Address == "" {
				if !a.isInteractive() {
					return missingFlagError("--address", "Pass --address <host:port> for the SOCKS5 upstream.")
				}
				config.Address, err = a.prompter().Input("SOCKS5 address (host:port)", "", validateHostPort)
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("name") {
					name, err = a.prompter().Input("Name (optional)", "", validateOptionalResourceName)
					if err != nil {
						return err
					}
				}
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			item, err := client.Egresses.Create(cmd.Context(), kibotos.CreateEgressRequest{
				Name: name, Zone: createMeta.zone, Labels: labels,
				Protocol: kibotos.EgressProtocolSocks5, Socks5: config, UDPEnabled: createFlags.udp,
			})
			if err != nil {
				return err
			}
			return a.renderEgress(item)
		}}
	createMeta.add(create, true)
	createFlags.add(create, true)
	update := &cobra.Command{Use: "update [id-or-name]", Short: "Update metadata or replace SOCKS5 configuration", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, ref := cmd.Context(), firstArg(args)
			replace, rename, err := a.networkUpdateMode(cmd, "address", "username", "password", "password-env", "tls", "server-name", "ca-cert", "udp-enabled", "drain-seconds")
			if err != nil {
				return err
			}
			var body kibotos.NetworkResourceUpdate
			var replacement kibotos.ReplaceEgressRequest
			if replace {
				config, err := updateFlags.config(cmd, a.envLookup)
				if err != nil {
					return err
				}
				if config.Address == "" {
					return missingFlagError("--address", "Pass --address <host:port> when replacing SOCKS5 configuration.")
				}
				drain, err := optionalIntFlag(cmd, "drain-seconds", updateFlags.drain, 0, 3600)
				if err != nil {
					return err
				}
				replacement = kibotos.ReplaceEgressRequest{
					Protocol: kibotos.EgressProtocolSocks5, Socks5: config, UDPEnabled: updateFlags.udp, DrainSeconds: drain,
				}
			} else {
				body, err = updateMeta.update(cmd)
				if err != nil {
					return err
				}
			}
			client, id, item, err := resolveNetworkTarget(a, ctx, ref, "egress", a.resolveEgress)
			if err != nil {
				return err
			}
			if rename {
				if item == nil {
					item, err = client.Egresses.Get(ctx, id)
					if err != nil {
						return err
					}
				}
				body, err = a.promptNetworkRename("egress", item.Name, "SOCKS5")
				if err != nil {
					return err
				}
			}
			if replace {
				item, err = client.Egresses.Replace(ctx, id, replacement)
			} else {
				item, err = client.Egresses.Update(ctx, id, body)
			}
			if err != nil {
				return err
			}
			return a.renderEgress(item)
		}}
	updateMeta.add(update, false)
	updateFlags.add(update, false)

	root.AddCommand(
		newCatalogListCmd(a, "egresses", true, []string{"ID", "NAME", "ZONE", "ADDRESS", "UDP", "REVISION"},
			func(ctx context.Context, c *kibotos.Client, p kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Egress], error) {
				return c.Egresses.List(ctx, &p)
			}, func(e kibotos.Egress) []string {
				return []string{e.ID, output.EmptyDash(e.Name), e.Zone, e.Socks5.Address, strconv.FormatBool(e.UDPEnabled), fmt.Sprint(e.Revision)}
			}),
		networkGetCmd(a, "egress", a.resolveEgress, func(ctx context.Context, c *kibotos.Client, id string) (*kibotos.Egress, error) {
			return c.Egresses.Get(ctx, id)
		}, a.renderEgress),
		create, update,
		networkDeleteCmd(a, "egress", a.resolveEgress, func(e *kibotos.Egress) string { return e.Name },
			func(ctx context.Context, c *kibotos.Client, id string) error { return c.Egresses.Delete(ctx, id) }),
	)
	return root
}

func (a *App) renderEgress(e *kibotos.Egress) error {
	return a.renderCatalogResource(e.ID, e, []output.KV{
		{Key: "ID", Value: e.ID}, {Key: "Name", Value: output.EmptyDash(e.Name)}, {Key: "Zone", Value: e.Zone},
		{Key: "Protocol", Value: string(e.Protocol)}, {Key: "Address", Value: e.Socks5.Address},
		{Key: "TLS", Value: strconv.FormatBool(e.Socks5.TLS != nil)}, {Key: "UDP Enabled", Value: strconv.FormatBool(e.UDPEnabled)},
		{Key: "Revision", Value: fmt.Sprint(e.Revision)}, {Key: "Labels", Value: output.FormatLabels(e.Labels)},
	})
}

type resolverFlags struct {
	protocol, address, endpoint string
}

func (f *resolverFlags) add(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.protocol, "protocol", "", "DNS protocol (udp, tcp, tls or https)")
	flags.StringVar(&f.address, "address", "", "DNS upstream address for udp, tcp, or tls")
	flags.StringVar(&f.endpoint, "url", "", "DNS-over-HTTPS endpoint")
}

func validateHTTPSURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return usageError("https requires --url with an HTTPS endpoint")
	}
	return nil
}

func (f resolverFlags) validate(cmd *cobra.Command) error {
	if cmd.Flags().Changed("protocol") {
		switch f.protocol {
		case "udp", "tcp", "tls", "https":
		default:
			return usageError("--protocol must be udp, tcp, tls or https")
		}
	}
	if cmd.Flags().Changed("url") {
		if err := validateHTTPSURL(f.endpoint); err != nil {
			return err
		}
		if f.protocol != "" && f.protocol != "https" {
			return usageError("--url requires --protocol https")
		}
	}
	if err := validateNonEmptyFlag(cmd, "address", f.address); err != nil {
		return err
	}
	if f.address != "" && (f.endpoint != "" || f.protocol == "https") {
		return usageError("--address is only valid for udp, tcp or tls; use --url for https")
	}
	return nil
}

func (f resolverFlags) config() (kibotos.ReplaceResolverRequest, error) {
	config := kibotos.ReplaceResolverRequest{Protocol: kibotos.ResolverProtocol(f.protocol)}
	switch config.Protocol {
	case kibotos.ResolverProtocolHTTPS:
		if f.endpoint == "" {
			return config, missingFlagError("--url", "Pass --url <https://endpoint> for DNS-over-HTTPS.")
		}
		if f.address != "" {
			return config, usageError("https requires --url without --address")
		}
		config.HTTPS = &kibotos.ResolverHTTPS{URL: f.endpoint}
	case kibotos.ResolverProtocolUDP, kibotos.ResolverProtocolTCP, kibotos.ResolverProtocolTLS:
		if f.address == "" {
			return config, missingFlagError("--address", "Pass --address <host[:port]> for udp, tcp, or tls.")
		}
		if f.endpoint != "" {
			return config, usageError("%s requires --address without --url", f.protocol)
		}
		address := &kibotos.ResolverAddress{Address: f.address}
		switch config.Protocol {
		case kibotos.ResolverProtocolUDP:
			config.UDP = address
		case kibotos.ResolverProtocolTCP:
			config.TCP = address
		case kibotos.ResolverProtocolTLS:
			config.TLS = address
		}
	default:
		return config, missingFlagError("--protocol", "Pass --protocol udp, tcp, tls or https.")
	}
	return config, nil
}

func (a *App) promptResolverConfig(f *resolverFlags) (bool, error) {
	if f.protocol == "" && f.endpoint != "" {
		f.protocol = "https"
	}
	prompted := false
	if f.protocol == "" {
		options := []prompt.Option{
			{Value: "udp", Label: "udp", Description: "Standard UDP DNS"},
			{Value: "tcp", Label: "tcp", Description: "TCP DNS"},
			{Value: "tls", Label: "tls", Description: "DNS-over-TLS"},
			{Value: "https", Label: "https", Description: "DNS-over-HTTPS"},
		}
		if f.address != "" {
			options = options[:3]
		}
		choice, err := a.prompter().Select("DNS protocol:", "Protocol", options, 0)
		if err != nil {
			return true, err
		}
		f.protocol = choice.Value
		prompted = true
	}
	if f.protocol == "https" {
		if f.endpoint == "" {
			var err error
			f.endpoint, err = a.prompter().Input("DNS-over-HTTPS URL (https://...)", "", validateHTTPSURL)
			return true, err
		}
	} else if f.address == "" {
		var err error
		f.address, err = a.prompter().Input("DNS upstream address (host or host:port)", "", validateNonEmpty)
		return true, err
	}
	return prompted, nil
}

func (a *App) newResolverCmd() *cobra.Command {
	root := &cobra.Command{Use: "resolver", Aliases: []string{"resolvers"}, Short: "Manage custom DNS upstreams for workspaces"}
	var createMeta, updateMeta networkMetadataFlags
	var createFlags, updateFlags resolverFlags
	create := &cobra.Command{Use: "create", Short: "Create a DNS resolver", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			labels, err := createMeta.create(cmd)
			if err != nil {
				return err
			}
			flags := createFlags
			if err := flags.validate(cmd); err != nil {
				return err
			}
			name := createMeta.name
			if a.isInteractive() {
				prompted, err := a.promptResolverConfig(&flags)
				if err != nil {
					return err
				}
				if prompted && !cmd.Flags().Changed("name") {
					name, err = a.prompter().Input("Name (optional)", "", validateOptionalResourceName)
					if err != nil {
						return err
					}
				}
			}
			config, err := flags.config()
			if err != nil {
				return err
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			item, err := client.Resolvers.Create(cmd.Context(), kibotos.CreateResolverRequest{
				Name: name, Zone: createMeta.zone, Labels: labels, Protocol: config.Protocol,
				UDP: config.UDP, TCP: config.TCP, TLS: config.TLS, HTTPS: config.HTTPS,
			})
			if err != nil {
				return err
			}
			return a.renderResolver(item)
		}}
	createMeta.add(create, true)
	createFlags.add(create)
	update := &cobra.Command{Use: "update [id-or-name]", Short: "Update metadata or replace the DNS upstream", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, ref := cmd.Context(), firstArg(args)
			replace, rename, err := a.networkUpdateMode(cmd, "protocol", "address", "url")
			if err != nil {
				return err
			}
			var body kibotos.NetworkResourceUpdate
			var replacement kibotos.ReplaceResolverRequest
			if replace {
				if err := updateFlags.validate(cmd); err != nil {
					return err
				}
				replacement, err = updateFlags.config()
			} else {
				body, err = updateMeta.update(cmd)
			}
			if err != nil {
				return err
			}
			client, id, item, err := resolveNetworkTarget(a, ctx, ref, "resolver", a.resolveResolver)
			if err != nil {
				return err
			}
			if rename {
				if item == nil {
					item, err = client.Resolvers.Get(ctx, id)
					if err != nil {
						return err
					}
				}
				body, err = a.promptNetworkRename("resolver", item.Name, "DNS")
				if err != nil {
					return err
				}
			}
			if replace {
				item, err = client.Resolvers.Replace(ctx, id, replacement)
			} else {
				item, err = client.Resolvers.Update(ctx, id, body)
			}
			if err != nil {
				return err
			}
			return a.renderResolver(item)
		}}
	updateMeta.add(update, false)
	updateFlags.add(update)

	root.AddCommand(
		newCatalogListCmd(a, "resolvers", true, []string{"ID", "NAME", "ZONE", "PROTOCOL", "UPSTREAM", "REVISION"},
			func(ctx context.Context, c *kibotos.Client, p kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Resolver], error) {
				return c.Resolvers.List(ctx, &p)
			}, func(r kibotos.Resolver) []string {
				return []string{r.ID, output.EmptyDash(r.Name), r.Zone, string(r.Protocol), resolverUpstream(r), fmt.Sprint(r.Revision)}
			}),
		networkGetCmd(a, "resolver", a.resolveResolver, func(ctx context.Context, c *kibotos.Client, id string) (*kibotos.Resolver, error) {
			return c.Resolvers.Get(ctx, id)
		}, a.renderResolver),
		create, update,
		networkDeleteCmd(a, "resolver", a.resolveResolver, func(r *kibotos.Resolver) string { return r.Name },
			func(ctx context.Context, c *kibotos.Client, id string) error { return c.Resolvers.Delete(ctx, id) }),
	)
	return root
}

func (a *App) renderResolver(r *kibotos.Resolver) error {
	return a.renderCatalogResource(r.ID, r, []output.KV{
		{Key: "ID", Value: r.ID}, {Key: "Name", Value: output.EmptyDash(r.Name)}, {Key: "Zone", Value: r.Zone},
		{Key: "Protocol", Value: string(r.Protocol)}, {Key: "Upstream", Value: resolverUpstream(*r)},
		{Key: "Revision", Value: fmt.Sprint(r.Revision)}, {Key: "Labels", Value: output.FormatLabels(r.Labels)},
	})
}

func resolverUpstream(r kibotos.Resolver) string {
	if r.HTTPS != nil {
		return r.HTTPS.URL
	}
	for _, address := range []*kibotos.ResolverAddress{r.UDP, r.TCP, r.TLS} {
		if address != nil {
			return address.Address
		}
	}
	return "-"
}

type networkResourceResolver[T any] func(context.Context, *kibotos.Client, string, string) (string, *T, error)

func resolveNetworkTarget[T any](a *App, ctx context.Context, ref, kind string, resolve networkResourceResolver[T]) (*kibotos.Client, string, *T, error) {
	if strings.TrimSpace(ref) == "" && !a.isInteractive() {
		return nil, "", nil, missingNetworkRef(kind)
	}
	client, _, err := a.requireClient()
	if err != nil {
		return nil, "", nil, err
	}
	id, item, err := resolve(ctx, client, ref, "")
	return client, id, item, err
}

func networkGetCmd[T any](a *App, kind string, resolve networkResourceResolver[T],
	get func(context.Context, *kibotos.Client, string) (*T, error), render func(*T) error,
) *cobra.Command {
	return &cobra.Command{Use: "get [id-or-name]", Short: "Show a" + networkArticleSuffix(kind) + " " + kind, Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, id, _, err := resolveNetworkTarget(a, ctx, firstArg(args), kind, resolve)
			if err != nil {
				return err
			}
			item, err := get(ctx, client, id)
			if err != nil {
				return err
			}
			return render(item)
		}}
}

func networkDeleteCmd[T any](a *App, kind string, resolve networkResourceResolver[T],
	name func(*T) string, deleteResource func(context.Context, *kibotos.Client, string) error,
) *cobra.Command {
	return &cobra.Command{Use: "delete [id-or-name]", Short: "Delete a" + networkArticleSuffix(kind) + " " + kind, Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, id, item, err := resolveNetworkTarget(a, ctx, firstArg(args), kind, resolve)
			if err != nil {
				return err
			}
			label := id
			if item != nil {
				label = resourceLabel(id, name(item))
			}
			ok, err := a.confirmDestructive(fmt.Sprintf("Delete %s %s?", kind, label))
			if err != nil {
				return err
			}
			if !ok {
				return prompt.ErrCancelled
			}
			if err := deleteResource(ctx, client, id); err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(map[string]any{"id": id, "deleted": true})
			}
			return p.ID(id)
		}}
}
