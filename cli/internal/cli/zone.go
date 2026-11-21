package cli

import (
	"fmt"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

func (a *App) newZoneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "zone",
		Aliases: []string{"zones"},
		Short:   "Inspect available Kibotos compute zones",
	}

	var (
		limit     int
		pageToken string
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available compute zones",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("limit") && limit <= 0 {
				return usageError("--limit must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			p := a.printer()

			var page *kibotos.Page[kibotos.Zone]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Zones.List(ctx, &kibotos.ZoneListParams{
					Limit:     limit,
					PageToken: pageToken,
				})
				if err != nil {
					return err
				}
			} else {
				var items []kibotos.Zone
				for z, iterErr := range client.Zones.Iter(ctx, nil) {
					if iterErr != nil {
						return iterErr
					}
					items = append(items, z)
				}
				if items == nil {
					items = []kibotos.Zone{}
				}
				page = &kibotos.Page[kibotos.Zone]{Data: items}
			}

			if p.IsJSON() {
				return p.JSON(page)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No zones found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, z := range page.Data {
				def := ""
				if z.Default {
					def = "yes"
				}
				rows = append(rows, []string{
					z.ID,
					output.EmptyDash(z.DisplayName),
					output.EmptyDash(z.Region),
					string(z.Status),
					def,
				})
			}
			return p.Table([]string{"ID", "NAME", "REGION", "STATUS", "DEFAULT"}, rows)
		},
	}

	listCmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	listCmd.Flags().StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")

	cmd.AddCommand(listCmd)
	return cmd
}
