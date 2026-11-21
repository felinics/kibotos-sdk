package cli

import (
	"fmt"
	"strconv"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

func (a *App) newExposureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "exposure",
		Aliases: []string{"exp", "exposures"},
		Short:   "Manage temporary HTTPS port exposures for workspaces",
	}

	cmd.AddCommand(
		a.newExposureListCmd(),
		a.newExposureGetCmd(),
		a.newExposureCreateCmd(),
		a.newExposureRevokeCmd(),
	)
	return cmd
}

func (a *App) newExposureListCmd() *cobra.Command {
	var (
		includeRevoked bool
		limit          int
		pageToken      string
	)

	cmd := &cobra.Command{
		Use:   "list <workspace_id>",
		Short: "List HTTPS exposures for a workspace",
		Args:  cobra.ExactArgs(1),
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

			params := &kibotos.ExposureListParams{
				IncludeRevoked: includeRevoked,
				Limit:          limit,
				PageToken:      pageToken,
			}

			var page *kibotos.Page[kibotos.Exposure]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Exposures.List(ctx, args[0], params)
				if err != nil {
					return err
				}
			} else {
				var items []kibotos.Exposure
				for exp, iterErr := range client.Exposures.Iter(ctx, args[0], params) {
					if iterErr != nil {
						return iterErr
					}
					items = append(items, exp)
				}
				if items == nil {
					items = []kibotos.Exposure{}
				}
				page = &kibotos.Page[kibotos.Exposure]{Data: items}
			}

			if p.IsJSON() {
				return p.JSON(page)
			}
			if len(page.Data) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No exposures found.")
				return nil
			}
			rows := make([][]string, 0, len(page.Data))
			for _, e := range page.Data {
				rows = append(rows, []string{
					e.ID,
					strconv.Itoa(e.Port),
					string(e.Status),
					e.URL,
					output.FormatTime(e.ExpiresAt),
				})
			}
			return p.Table([]string{"ID", "PORT", "STATUS", "URL", "EXPIRES AT"}, rows)
		},
	}

	f := cmd.Flags()
	f.BoolVar(&includeRevoked, "include-revoked", false, "Include revoked exposures in the results")
	f.IntVar(&limit, "limit", 0, "Maximum number of results per page (1-100)")
	f.StringVar(&pageToken, "page-token", "", "Opaque pagination token from a previous response")
	return cmd
}

func (a *App) newExposureGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <workspace_id> <exposure_id>",
		Short: "Get details of an HTTPS port exposure",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			exp, err := client.Exposures.Get(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return a.renderExposure(exp)
		},
	}
}

func (a *App) newExposureCreateCmd() *cobra.Command {
	var (
		port int
		ttl  int
	)

	cmd := &cobra.Command{
		Use:   "create <workspace_id>",
		Short: "Expose a workspace port over a temporary HTTPS URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("port") || port <= 0 || port > 65535 {
				return usageError("--port is required and must be between 1 and 65535")
			}
			if cmd.Flags().Changed("ttl") && ttl <= 0 {
				return usageError("--ttl must be greater than zero")
			}

			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			exp, err := client.Exposures.Create(cmd.Context(), args[0], kibotos.CreateExposureParams{
				Port:       port,
				TTLSeconds: ttl,
			})
			if err != nil {
				return err
			}
			return a.renderExposure(exp)
		},
	}

	f := cmd.Flags()
	f.IntVar(&port, "port", 0, "Workspace TCP port to expose (1-65535, required)")
	f.IntVar(&ttl, "ttl", 0, "Exposure lifetime in seconds (API default: 3600)")
	return cmd
}

func (a *App) newExposureRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <workspace_id> <exposure_id>",
		Short: "Revoke an active HTTPS port exposure",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.requireClient()
			if err != nil {
				return err
			}
			exp, err := client.Exposures.Revoke(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			return a.renderExposure(exp)
		},
	}
}

func (a *App) renderExposure(exp *kibotos.Exposure) error {
	p := a.printer()
	if p.IsJSON() {
		return p.JSON(exp)
	}
	return p.Details([]output.KV{
		{Key: "ID", Value: exp.ID},
		{Key: "Workspace ID", Value: exp.WorkspaceID},
		{Key: "Port", Value: strconv.Itoa(exp.Port)},
		{Key: "URL", Value: exp.URL},
		{Key: "Status", Value: string(exp.Status)},
		{Key: "Expires At", Value: output.FormatTime(exp.ExpiresAt)},
		{Key: "Revoked At", Value: output.FormatTimePtr(exp.RevokedAt)},
		{Key: "Created At", Value: output.FormatTime(exp.CreatedAt)},
	})
}
