package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
)

type orgListResult struct {
	CurrentOrganizationID string                     `json:"current_organization_id,omitempty"`
	Organizations         []auth.OrganizationSummary `json:"organizations"`
}

func (a *App) newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "org",
		Aliases: []string{"orgs", "organization", "organizations"},
		Short:   "List and switch Kibotos organizations for your signed-in CLI session",
	}

	cmd.AddCommand(
		a.newOrgListCmd(),
		a.newOrgUseCmd(),
	)
	return cmd
}

func (a *App) newOrgListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List organizations accessible to your signed-in account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			_, res, err := a.loadAndResolve("")
			if err != nil {
				return err
			}
			if res.AuthType != "user_session" || res.AccessToken == "" {
				return usageErrorWithHint(
					"Run `kibo login` to sign in with your Kibotos account (API keys are scoped to a single organization).",
					"organization listing requires a user CLI session",
				)
			}

			devClient, err := auth.NewDeviceClient(res.ConsoleURL, a.httpClient, a.userAgent())
			if err != nil {
				return usageError("%v", err)
			}
			orgs, err := devClient.ListOrganizations(cmd.Context(), res.AccessToken)
			if err != nil {
				return err
			}

			if p.IsJSON() {
				return p.JSON(orgListResult{
					CurrentOrganizationID: res.OrganizationID,
					Organizations:         orgs,
				})
			}
			if p.IsQuiet() {
				ids := make([]string, 0, len(orgs))
				for _, o := range orgs {
					ids = append(ids, o.ID)
				}
				return p.IDs(ids)
			}
			if len(orgs) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No organizations found.")
				return nil
			}
			rows := make([][]string, 0, len(orgs))
			for _, o := range orgs {
				marker := ""
				if o.ID == res.OrganizationID {
					marker = "*"
				}
				rows = append(rows, []string{
					marker,
					o.ID,
					output.EmptyDash(o.DisplayName),
					output.EmptyDash(o.Region),
					output.EmptyDash(o.Role),
					p.FormatStatus(o.Status),
				})
			}
			return p.Table([]string{"ACTIVE", "ID", "NAME", "REGION", "ROLE", "STATUS"}, rows)
		},
	}
}

func (a *App) newOrgUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [id-or-name]",
		Short: "Switch the active organization for the current profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			cfg, res, err := a.loadAndResolve("")
			if err != nil {
				return err
			}
			if res.AuthType != "user_session" || res.AccessToken == "" {
				return usageErrorWithHint(
					"Run `kibo login` to sign in with your Kibotos account (API keys are fixed to a single organization).",
					"switching organizations requires a user CLI session",
				)
			}
			prof := cfg.Profiles[res.ProfileName]
			if prof == nil {
				return usageError("profile %q is not saved in config; run `kibo login` first", res.ProfileName)
			}

			devClient, err := auth.NewDeviceClient(res.ConsoleURL, a.httpClient, a.userAgent())
			if err != nil {
				return usageError("%v", err)
			}
			orgs, err := devClient.ListOrganizations(cmd.Context(), res.AccessToken)
			if err != nil {
				return err
			}
			if len(orgs) == 0 {
				return &CLIError{
					ExitCode: ExitError,
					Code:     "no_organization",
					Message:  "no organizations found for your account",
				}
			}

			ref := ""
			if len(args) > 0 {
				ref = strings.TrimSpace(args[0])
			}

			var chosen *auth.OrganizationSummary
			if ref == "" {
				if !a.isInteractive() {
					return usageErrorWithHint(
						"Pass an organization ID or name. Run `kibo org list --json` to see available organizations.",
						"organization ID or name is required",
					)
				}
				opts := make([]prompt.Option, 0, len(orgs))
				defaultIdx := 0
				for i, o := range orgs {
					desc := fmt.Sprintf("%s · %s · %s", o.Region, o.Role, o.Status)
					if o.ID == res.OrganizationID {
						desc += " (active)"
						defaultIdx = i
					}
					opts = append(opts, prompt.Option{
						Value:       o.ID,
						Label:       fmt.Sprintf("%s (%s)", o.DisplayName, o.ID),
						Description: desc,
					})
				}
				pick, err := a.prompter().Select("Select active organization:", "Organization", opts, defaultIdx)
				if err != nil {
					return err
				}
				for i := range orgs {
					if orgs[i].ID == pick.Value {
						chosen = &orgs[i]
						break
					}
				}
			} else {
				var matches []auth.OrganizationSummary
				for _, o := range orgs {
					if o.ID == ref {
						copyOrg := o
						chosen = &copyOrg
						break
					}
					if strings.EqualFold(o.DisplayName, ref) {
						matches = append(matches, o)
					}
				}
				if chosen == nil {
					switch len(matches) {
					case 0:
						return &CLIError{
							ExitCode: ExitError,
							Code:     "organization_not_found",
							Message:  fmt.Sprintf("organization %q not found", ref),
							Hint:     "Run `kibo org list` to see accessible organizations.",
						}
					case 1:
						chosen = &matches[0]
					default:
						ids := make([]string, 0, len(matches))
						for _, m := range matches {
							ids = append(ids, m.ID)
						}
						return &CLIError{
							ExitCode: ExitUsage,
							Code:     "ambiguous_name",
							Message:  fmt.Sprintf("multiple organizations match %q: %s", ref, strings.Join(ids, ", ")),
							Hint:     "Pass the explicit organization ID (org_...) instead.",
							Details:  map[string]any{"candidates": ids},
						}
					}
				}
			}

			prof.OrganizationID = chosen.ID
			prof.OrganizationName = chosen.DisplayName
			if err := a.store.Save(cfg); err != nil {
				return err
			}

			if p.IsJSON() {
				return p.JSON(map[string]any{
					"profile":           res.ProfileName,
					"organization_id":   chosen.ID,
					"organization_name": chosen.DisplayName,
					"region":            chosen.Region,
				})
			}
			if p.IsQuiet() {
				return p.ID(chosen.ID)
			}
			_, _ = fmt.Fprintf(p.Out, "Active organization set to %s (%s).\n", chosen.DisplayName, chosen.ID)
			return nil
		},
	}
}
