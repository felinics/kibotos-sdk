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
	return parseLabels(f.labels)
}

func (f networkMetadataFlags) update(cmd *cobra.Command) (kibotos.NetworkResourceUpdate, error) {
	params := kibotos.NetworkResourceUpdate{}
	if cmd.Flags().Changed("name") {
		if f.clearName {
			return params, usageError("--name and --clear-name cannot be combined")
		}
		params.Name = kibotos.Some(f.name)
	} else if f.clearName {
		params.Name = kibotos.Null[string]()
	}
	var err error
	params.Labels, err = parseUpdateLabels(f.labels, f.unset)
	return params, err
}

func networkUpdateMode(cmd *cobra.Command, configFlags ...string) (bool, error) {
	flags := cmd.Flags()
	metadata := flags.Changed("name") || flags.Changed("clear-name") || flags.Changed("label") || flags.Changed("unset-label")
	replace := false
	for _, name := range configFlags {
		replace = replace || flags.Changed(name)
	}
	switch {
	case metadata && replace:
		return false, usageError("update configuration and metadata in separate commands")
	case !metadata && !replace:
		return false, usageError("specify configuration or metadata flags to update")
	default:
		return replace, nil
	}
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

func (f egressFlags) config(cmd *cobra.Command, env func(string) string) (kibotos.EgressSocks5Input, error) {
	config := kibotos.EgressSocks5Input{Address: f.address}
	host, port, err := net.SplitHostPort(f.address)
	number, portErr := strconv.Atoi(port)
	if err != nil || host == "" || portErr != nil || number < 1 || number > 65535 {
		return config, usageError("--address requires a SOCKS5 host:port")
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
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			item, err := client.Egresses.Create(cmd.Context(), kibotos.CreateEgressRequest{
				Name: createMeta.name, Zone: createMeta.zone, Labels: labels,
				Protocol: kibotos.EgressProtocolSocks5, Socks5: config, UDPEnabled: createFlags.udp,
			})
			if err != nil {
				return err
			}
			return a.renderEgress(item)
		}}
	createMeta.add(create, true)
	createFlags.add(create, true)
	update := &cobra.Command{Use: "update <egress_id>", Short: "Update metadata or replace SOCKS5 configuration", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			replace, err := networkUpdateMode(cmd, "address", "username", "password", "password-env", "tls", "server-name", "ca-cert", "udp-enabled", "drain-seconds")
			if err != nil {
				return err
			}
			if !replace {
				body, err := updateMeta.update(cmd)
				if err != nil {
					return err
				}
				return a.runEgressChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Egress, error) {
					return c.Egresses.Update(ctx, args[0], body)
				})
			}
			config, err := updateFlags.config(cmd, a.envLookup)
			if err != nil {
				return err
			}
			drain, err := optionalIntFlag(cmd, "drain-seconds", updateFlags.drain, 0, 3600)
			if err != nil {
				return err
			}
			return a.runEgressChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Egress, error) {
				return c.Egresses.Replace(ctx, args[0], kibotos.ReplaceEgressRequest{
					Protocol: kibotos.EgressProtocolSocks5, Socks5: config, UDPEnabled: updateFlags.udp, DrainSeconds: drain,
				})
			})
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
		&cobra.Command{Use: "get <egress_id>", Short: "Show an egress", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.runEgressChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Egress, error) {
					return c.Egresses.Get(ctx, args[0])
				})
			}},
		create, update,
		a.networkDeleteCmd("egress", func(ctx context.Context, c *kibotos.Client, id string) error { return c.Egresses.Delete(ctx, id) }),
	)
	return root
}

func (a *App) runEgressChange(ctx context.Context, fn func(context.Context, *kibotos.Client) (*kibotos.Egress, error)) error {
	client, _, err := a.requireClient()
	if err != nil {
		return err
	}
	item, err := fn(ctx, client)
	if err != nil {
		return err
	}
	return a.renderEgress(item)
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

func (f resolverFlags) config() (kibotos.ReplaceResolverRequest, error) {
	config := kibotos.ReplaceResolverRequest{Protocol: kibotos.ResolverProtocol(f.protocol)}
	switch config.Protocol {
	case kibotos.ResolverProtocolHTTPS:
		u, err := url.Parse(f.endpoint)
		if f.address != "" || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
			return config, usageError("https requires --url with an HTTPS endpoint")
		}
		config.HTTPS = &kibotos.ResolverHTTPS{URL: f.endpoint}
	case kibotos.ResolverProtocolUDP, kibotos.ResolverProtocolTCP, kibotos.ResolverProtocolTLS:
		if strings.TrimSpace(f.address) == "" || f.endpoint != "" {
			return config, usageError("%s requires --address", f.protocol)
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
		return config, usageError("--protocol must be udp, tcp, tls or https")
	}
	return config, nil
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
			config, err := createFlags.config()
			if err != nil {
				return err
			}
			return a.runResolverChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Resolver, error) {
				return c.Resolvers.Create(ctx, kibotos.CreateResolverRequest{
					Name: createMeta.name, Zone: createMeta.zone, Labels: labels, Protocol: config.Protocol,
					UDP: config.UDP, TCP: config.TCP, TLS: config.TLS, HTTPS: config.HTTPS,
				})
			})
		}}
	createMeta.add(create, true)
	createFlags.add(create)
	update := &cobra.Command{Use: "update <resolver_id>", Short: "Update metadata or replace the DNS upstream", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			replace, err := networkUpdateMode(cmd, "protocol", "address", "url")
			if err != nil {
				return err
			}
			if !replace {
				body, err := updateMeta.update(cmd)
				if err != nil {
					return err
				}
				return a.runResolverChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Resolver, error) {
					return c.Resolvers.Update(ctx, args[0], body)
				})
			}
			body, err := updateFlags.config()
			if err != nil {
				return err
			}
			return a.runResolverChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Resolver, error) {
				return c.Resolvers.Replace(ctx, args[0], body)
			})
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
		&cobra.Command{Use: "get <resolver_id>", Short: "Show a resolver", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.runResolverChange(cmd.Context(), func(ctx context.Context, c *kibotos.Client) (*kibotos.Resolver, error) {
					return c.Resolvers.Get(ctx, args[0])
				})
			}},
		create, update,
		a.networkDeleteCmd("resolver", func(ctx context.Context, c *kibotos.Client, id string) error { return c.Resolvers.Delete(ctx, id) }),
	)
	return root
}

func (a *App) runResolverChange(ctx context.Context, fn func(context.Context, *kibotos.Client) (*kibotos.Resolver, error)) error {
	client, _, err := a.requireClient()
	if err != nil {
		return err
	}
	r, err := fn(ctx, client)
	if err != nil {
		return err
	}
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

func (a *App) networkDeleteCmd(kind string, deleteResource func(context.Context, *kibotos.Client, string) error) *cobra.Command {
	return &cobra.Command{Use: "delete <" + kind + "_id>", Short: "Delete a " + kind, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			id := args[0]
			ok, err := a.confirmDestructive(fmt.Sprintf("Delete %s %s?", kind, id))
			if err != nil {
				return err
			}
			if !ok {
				return prompt.ErrCancelled
			}
			if err := deleteResource(cmd.Context(), client, id); err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(map[string]any{"id": id, "deleted": true})
			}
			return p.ID(id)
		}}
}
