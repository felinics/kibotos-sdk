package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
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
		Use:   "list [workspace]",
		Short: "List HTTPS exposures for a workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePageLimit(cmd, limit); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "Select a workspace:")
			if err != nil {
				return err
			}
			p := a.printer()

			params := &kibotos.ExposureListParams{
				IncludeRevoked: includeRevoked,
				Limit:          limit,
				PageToken:      pageToken,
			}

			var page *kibotos.Page[kibotos.Exposure]
			if cmd.Flags().Changed("limit") || cmd.Flags().Changed("page-token") {
				page, err = client.Exposures.List(ctx, wsID, params)
				if err != nil {
					return err
				}
			} else {
				var items []kibotos.Exposure
				for exp, iterErr := range client.Exposures.Iter(ctx, wsID, params) {
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
			if p.IsQuiet() {
				ids := make([]string, 0, len(page.Data))
				for _, e := range page.Data {
					ids = append(ids, e.ID)
				}
				return p.IDs(ids)
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
					p.FormatStatus(string(e.Status)),
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

func (a *App) resolveCommandExposure(ctx context.Context, args []string, includeRevoked bool, title string) (*kibotos.Client, string, string, *kibotos.Exposure, error) {
	wsRef, expRef := firstArg(args), ""
	if len(args) > 1 {
		expRef = args[1]
	}
	if !a.isInteractive() {
		if strings.TrimSpace(wsRef) == "" {
			return nil, "", "", nil, missingWorkspaceRef()
		}
		if strings.TrimSpace(expRef) == "" {
			return nil, "", "", nil, missingExposureRef()
		}
	}
	client, _, err := a.requireClient()
	if err != nil {
		return nil, "", "", nil, err
	}
	wsID, _, err := a.resolveWorkspace(ctx, client, wsRef, nil, "", "Select a workspace:")
	if err != nil {
		return nil, "", "", nil, err
	}
	expID, exp, err := a.resolveExposure(ctx, client, wsID, expRef, includeRevoked, title)
	return client, wsID, expID, exp, err
}

func (a *App) newExposureGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [workspace] [exposure_id]",
		Short: "Get details of an HTTPS port exposure",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, wsID, expID, _, err := a.resolveCommandExposure(ctx, args, true, "Select an exposure to inspect:")
			if err != nil {
				return err
			}
			exp, err := client.Exposures.Get(ctx, wsID, expID)
			if err != nil {
				return err
			}
			return a.renderExposure(exp)
		},
	}
}

func (a *App) newExposureCreateCmd() *cobra.Command {
	var (
		port   int
		ttlRaw string
	)

	cmd := &cobra.Command{
		Use:   "create [workspace] [port]",
		Short: "Expose a workspace port over a temporary HTTPS URL",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			portChanged := cmd.Flags().Changed("port")
			if len(args) == 2 {
				posPort, err := strconv.Atoi(strings.TrimSpace(args[1]))
				if err != nil || posPort <= 0 || posPort > 65535 {
					return usageError("invalid port %q: must be between 1 and 65535", args[1])
				}
				if portChanged && port != posPort {
					return usageError("conflicting port values: %d (positional) vs %d (--port)", posPort, port)
				}
				port = posPort
				portChanged = true
			}

			if portChanged {
				if port <= 0 || port > 65535 {
					return usageError("--port must be between 1 and 65535")
				}
			} else if !a.isInteractive() {
				return missingFlagError("--port", "Pass --port <1-65535> (or positional port, e.g. `kibo exp create <ws> 8080`).")
			}

			var ttlSec int
			if cmd.Flags().Changed("ttl") {
				var err error
				ttlSec, err = parseExposureTTLSeconds(ttlRaw)
				if err != nil {
					return err
				}
			}

			ctx := cmd.Context()
			client, wsID, _, err := a.resolveCommandWorkspace(
				ctx, firstArg(args),
				func(w kibotos.Workspace) bool { return w.Status == kibotos.WorkspaceStatusAvailable },
				"no available workspaces found to expose",
				"Select an available workspace:",
			)
			if err != nil {
				return err
			}

			if !portChanged {
				portInput, err := a.prompter().Input("Port to expose (1-65535)", "8080", func(v string) error {
					n, err := strconv.Atoi(strings.TrimSpace(v))
					if err != nil || n <= 0 || n > 65535 {
						return fmt.Errorf("enter a port number between 1 and 65535")
					}
					return nil
				})
				if err != nil {
					return err
				}
				port, _ = strconv.Atoi(strings.TrimSpace(portInput))
			}

			exp, err := client.Exposures.Create(ctx, wsID, kibotos.CreateExposureParams{
				Port:       port,
				TTLSeconds: ttlSec,
			})
			if err != nil {
				return err
			}
			return a.renderExposure(exp)
		},
	}

	f := cmd.Flags()
	f.IntVarP(&port, "port", "p", 0, "Workspace TCP port to expose (1-65535)")
	f.StringVar(&ttlRaw, "ttl", "", "Exposure lifetime (e.g. 1h, 30m, 3600; API default: 3600s)")
	return cmd
}

func (a *App) newExposureRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke [workspace] [exposure_id]",
		Short: "Revoke an active HTTPS port exposure",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, wsID, expID, picked, err := a.resolveCommandExposure(ctx, args, false, "Select an active exposure to revoke:")
			if err != nil {
				return err
			}

			targetLabel := expID
			if picked != nil {
				targetLabel = fmt.Sprintf("%s (port %d, %s)", picked.ID, picked.Port, picked.URL)
			}
			ok, err := a.confirmDestructive(fmt.Sprintf("Revoke exposure %s?", targetLabel))
			if err != nil {
				return err
			}
			if !ok {
				return prompt.ErrCancelled
			}

			exp, err := client.Exposures.Revoke(ctx, wsID, expID)
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
	if p.IsQuiet() {
		return p.ID(exp.ID)
	}
	return p.Details([]output.KV{
		{Key: "ID", Value: exp.ID},
		{Key: "Workspace ID", Value: exp.WorkspaceID},
		{Key: "Port", Value: strconv.Itoa(exp.Port)},
		{Key: "URL", Value: exp.URL},
		{Key: "Status", Value: p.FormatStatus(string(exp.Status))},
		{Key: "Expires At", Value: output.FormatTime(exp.ExpiresAt)},
		{Key: "Revoked At", Value: output.FormatTimePtr(exp.RevokedAt)},
		{Key: "Created At", Value: output.FormatTime(exp.CreatedAt)},
	})
}
