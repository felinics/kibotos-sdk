package cli

import (
	"context"
	"fmt"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

func (a *App) newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"tpl", "templates"},
		Short:   "Inspect Kibotos workspace templates and revisions",
	}

	cmd.AddCommand(
		a.newTemplateListCmd(),
		a.newTemplateGetCmd(),
		a.newTemplateRevisionsCmd(),
		a.newTemplateRevisionCmd(),
	)
	return cmd
}

func (a *App) newTemplateListCmd() *cobra.Command {
	var (
		owner     string
		zone      string
		limit     int
		pageToken string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available workspace templates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsedOwner, err := parseTemplateOwner(owner)
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

			params := &kibotos.TemplateListParams{
				Owner:     parsedOwner,
				Zone:      strings.TrimSpace(zone),
				Limit:     limit,
				PageToken: pageToken,
			}

			var page *kibotos.MultiZonePage[kibotos.Template]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Templates.List(ctx, params)
				if err != nil {
					return err
				}
			} else {
				page, err = collectAllTemplates(ctx, client, params)
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
				for _, t := range page.Data {
					ids = append(ids, t.ID)
				}
				return p.IDs(ids)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No templates found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, t := range page.Data {
				rows = append(rows, []string{
					t.ID,
					string(t.Owner),
					output.EmptyDash(t.DisplayName),
					output.EmptyDash(t.DefaultRevision),
					output.EmptyDash(strings.Join(t.Zones, ",")),
				})
			}
			return p.Table([]string{"ID", "OWNER", "NAME", "DEFAULT REVISION", "ZONES"}, rows)
		},
	}

	f := cmd.Flags()
	f.StringVar(&owner, "owner", "", "Filter by template owner (platform or organization)")
	f.StringVarP(&zone, "zone", "z", "", "Filter by zone ID")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newTemplateGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [id]",
		Short: "Get details of a workspace template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			tplID := ""
			if len(args) > 0 {
				tplID = strings.TrimSpace(args[0])
			} else if a.isInteractive() {
				picked, err := a.resolveTemplate(ctx, client, "")
				if err != nil {
					return err
				}
				tplID = picked.ID
			} else {
				return usageErrorWithHint("Pass a template ID. Run `kibo template list --json` to see available templates.", "template ID is required")
			}

			tpl, err := client.Templates.Get(ctx, tplID)
			if err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(tpl)
			}
			if p.IsQuiet() {
				return p.ID(tpl.ID)
			}
			latest := "-"
			if tpl.LatestRevision != nil {
				latest = fmt.Sprintf("%s (%s)", tpl.LatestRevision.ID, tpl.LatestRevision.Status)
			}
			return p.Details([]output.KV{
				{Key: "ID", Value: tpl.ID},
				{Key: "Owner", Value: string(tpl.Owner)},
				{Key: "Display Name", Value: output.EmptyDash(tpl.DisplayName)},
				{Key: "Description", Value: output.EmptyDash(tpl.Description)},
				{Key: "Default Revision", Value: output.EmptyDash(tpl.DefaultRevision)},
				{Key: "Latest Revision", Value: latest},
				{Key: "Zones", Value: output.EmptyDash(strings.Join(tpl.Zones, ", "))},
				{Key: "Created At", Value: output.FormatTimePtr(tpl.CreatedAt)},
				{Key: "Updated At", Value: output.FormatTimePtr(tpl.UpdatedAt)},
			})
		},
	}
}

func (a *App) newTemplateRevisionsCmd() *cobra.Command {
	var (
		limit     int
		pageToken string
	)

	cmd := &cobra.Command{
		Use:   "revisions [id]",
		Short: "List revisions for a workspace template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePageLimit(cmd, limit); err != nil {
				return err
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			tplID := ""
			if len(args) > 0 {
				tplID = strings.TrimSpace(args[0])
			} else if a.isInteractive() {
				picked, err := a.resolveTemplate(ctx, client, "")
				if err != nil {
					return err
				}
				tplID = picked.ID
			} else {
				return usageErrorWithHint("Pass a template ID. Run `kibo template list --json` to see available templates.", "template ID is required")
			}

			p := a.printer()

			var page *kibotos.Page[kibotos.TemplateRevision]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Templates.ListRevisions(ctx, tplID, &kibotos.TemplateRevisionListParams{
					Limit:     limit,
					PageToken: pageToken,
				})
				if err != nil {
					return err
				}
			} else {
				page, err = collectAllTemplateRevisions(ctx, client, tplID)
				if err != nil {
					return err
				}
			}

			if p.IsJSON() {
				return p.JSON(page)
			}
			if p.IsQuiet() {
				ids := make([]string, 0, len(page.Data))
				for _, rev := range page.Data {
					ids = append(ids, rev.ID)
				}
				return p.IDs(ids)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No template revisions found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, rev := range page.Data {
				sizeStr := "-"
				if rev.SizeMiB != nil {
					sizeStr = fmt.Sprintf("%d", *rev.SizeMiB)
				}
				rows = append(rows, []string{
					rev.ID,
					p.FormatStatus(rev.Status),
					sizeStr,
					output.EmptyDash(strings.Join(rev.Zones, ",")),
					output.FormatTimePtr(rev.ReadyAt),
				})
			}
			return p.Table([]string{"REVISION", "STATUS", "SIZE (MiB)", "ZONES", "READY AT"}, rows)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	cmd.Flags().StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newTemplateRevisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revision <id> <revision_id>",
		Short: "Get details of a specific template revision",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			rev, err := client.Templates.GetRevision(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(rev)
			}
			if p.IsQuiet() {
				return p.ID(rev.ID)
			}
			sizeStr := "-"
			if rev.SizeMiB != nil {
				sizeStr = fmt.Sprintf("%d MiB", *rev.SizeMiB)
			}
			items := []output.KV{
				{Key: "ID", Value: rev.ID},
				{Key: "Template ID", Value: rev.TemplateID},
				{Key: "Status", Value: p.FormatStatus(rev.Status)},
				{Key: "Size", Value: sizeStr},
				{Key: "Zones", Value: output.EmptyDash(strings.Join(rev.Zones, ", "))},
				{Key: "Created At", Value: output.FormatTimePtr(rev.CreatedAt)},
				{Key: "Ready At", Value: output.FormatTimePtr(rev.ReadyAt)},
			}
			if rev.Failure != nil {
				items = append(items, output.KV{
					Key:   "Failure",
					Value: fmt.Sprintf("%s: %s (retryable=%t)", rev.Failure.Code, rev.Failure.Message, rev.Failure.Retryable),
				})
			}
			return p.Details(items)
		},
	}
}

func collectAllTemplates(ctx context.Context, client *kibotos.Client, base *kibotos.TemplateListParams) (*kibotos.MultiZonePage[kibotos.Template], error) {
	var p kibotos.TemplateListParams
	if base != nil {
		p = *base
	}
	all := []kibotos.Template{}
	unavailSeen := make(map[string]struct{})
	unavail := []string{}
	for {
		page, err := client.Templates.List(ctx, &p)
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
	return &kibotos.MultiZonePage[kibotos.Template]{
		Data:             all,
		UnavailableZones: unavail,
	}, nil
}

func collectAllTemplateRevisions(ctx context.Context, client *kibotos.Client, templateID string) (*kibotos.Page[kibotos.TemplateRevision], error) {
	var p kibotos.TemplateRevisionListParams
	all := []kibotos.TemplateRevision{}
	for {
		page, err := client.Templates.ListRevisions(ctx, templateID, &p)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		next := page.Token()
		if next == "" || next == p.PageToken {
			break
		}
		p.PageToken = next
	}
	return &kibotos.Page[kibotos.TemplateRevision]{Data: all}, nil
}
