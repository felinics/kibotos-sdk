package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newPackCmd() *cobra.Command {
	root := &cobra.Command{Use: "pack", Aliases: []string{"packs"}, Short: "Browse available workspace Packs"}
	root.AddCommand(
		newCatalogListCmd(a, "Packs", false, []string{"ID", "NAME", "VERSION", "ARCHITECTURE", "ZONES"},
			func(ctx context.Context, c *kibotos.Client, options kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Pack], error) {
				return c.Packs.List(ctx, &kibotos.PackListParams{Zone: options.Zone, Limit: options.Limit, PageToken: options.PageToken})
			}, func(p kibotos.Pack) []string {
				return []string{p.ID, p.DisplayName, p.Version, p.Architecture, strings.Join(p.Zones, ", ")}
			}),
	)
	var revision string
	get := &cobra.Command{
		Use:   "get <pack_id>",
		Short: "Show a Pack or a specific revision",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateNonEmptyFlag(cmd, "revision", revision); err != nil {
				return err
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			if revision != "" {
				r, err := client.Packs.GetRevision(cmd.Context(), args[0], revision)
				if err != nil {
					return err
				}
				return a.renderCatalogResource(r.ID, r, []output.KV{
					{Key: "Pack", Value: r.PackID}, {Key: "Revision", Value: r.ID}, {Key: "Version", Value: r.Version},
					{Key: "Architecture", Value: r.Architecture}, {Key: "Capabilities", Value: strings.Join(r.ProvidedCapabilities, ", ")},
					{Key: "Requirements", Value: strings.Join(r.GuestRequirements, ", ")},
					{Key: "Zones", Value: strings.Join(r.Zones, ", ")}, {Key: "Published", Value: output.FormatTime(r.PublishedAt)},
				})
			}
			p, err := client.Packs.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.renderCatalogResource(p.ID, p, []output.KV{
				{Key: "ID", Value: p.ID}, {Key: "Name", Value: p.DisplayName}, {Key: "Description", Value: p.Description},
				{Key: "Default Revision", Value: p.DefaultRevision}, {Key: "Version", Value: p.Version},
				{Key: "Architecture", Value: p.Architecture}, {Key: "Capabilities", Value: strings.Join(p.ProvidedCapabilities, ", ")},
				{Key: "Requirements", Value: strings.Join(p.GuestRequirements, ", ")},
				{Key: "Zones", Value: strings.Join(p.Zones, ", ")}, {Key: "Published", Value: output.FormatTime(p.PublishedAt)},
			})
		},
	}
	get.Flags().StringVar(&revision, "revision", "", "Specific revision ID")
	root.AddCommand(get)
	return root
}

func newCatalogListCmd[T any](a *App, noun string, filterLabels bool, headers []string,
	list func(context.Context, *kibotos.Client, kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[T], error),
	row func(T) []string,
) *cobra.Command {
	var options kibotos.NetworkResourceListParams
	var labels []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List " + noun,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			options.Labels, err = parseLabels(labels)
			if err != nil {
				return err
			}
			if err := validateNonEmptyFlag(cmd, "zone", options.Zone); err != nil {
				return err
			}
			if err := validatePageLimit(cmd, options.Limit); err != nil {
				return err
			}
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			page, err := list(ctx, client, options)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("limit") && !cmd.Flags().Changed("page-token") {
				seen := map[string]bool{}
				for page.HasMore() {
					options.PageToken = page.Token()
					if seen[options.PageToken] {
						return fmt.Errorf("repeated page token while listing %s", noun)
					}
					seen[options.PageToken] = true
					next, err := list(ctx, client, options)
					if err != nil {
						return err
					}
					page.Data = append(page.Data, next.Data...)
					page.UnavailableZones = append(page.UnavailableZones, next.UnavailableZones...)
					page.NextPageToken = next.NextPageToken
				}
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(page)
			}
			if len(page.UnavailableZones) > 0 {
				p.Warnf("unavailable zones: %s", strings.Join(page.UnavailableZones, ", "))
			}
			rows := make([][]string, 0, len(page.Data))
			for _, item := range page.Data {
				values := row(item)
				if p.IsQuiet() {
					if err := p.ID(values[0]); err != nil {
						return err
					}
				} else {
					rows = append(rows, values)
				}
			}
			if p.IsQuiet() {
				return nil
			}
			return p.Table(headers, rows)
		},
	}
	cmd.Flags().StringVarP(&options.Zone, "zone", "z", "", "Filter by zone")
	cmd.Flags().IntVar(&options.Limit, "limit", 0, "Maximum items per page (1–100)")
	cmd.Flags().StringVar(&options.PageToken, "page-token", "", "Fetch one page using this cursor")
	if filterLabels {
		cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "Filter by label (key=value, repeatable)")
	}
	return cmd
}

func (a *App) renderCatalogResource(id string, resource any, items []output.KV) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(resource)
	}
	if p.IsQuiet() {
		return p.ID(id)
	}
	return p.Details(items)
}
