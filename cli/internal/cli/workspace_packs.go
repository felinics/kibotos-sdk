package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newWorkspacePacksCmd() *cobra.Command {
	root := &cobra.Command{Use: "packs", Aliases: []string{"pack"}, Short: "Manage a workspace's Pack bindings"}
	root.AddCommand(a.newWorkspacePacksSetCmd(), a.newWorkspacePackChangeCmd(true), a.newWorkspacePackChangeCmd(false))
	return root
}

func (a *App) newWorkspacePacksSetCmd() *cobra.Command {
	var restart, wait, noWait bool
	var waitTimeout string
	cmd := &cobra.Command{
		Use:     "set [workspace] [pack_id...]",
		Short:   "Replace the Pack list (omit IDs to clear it)",
		Example: "  kibo ws packs set\n  kibo ws packs set wks_123 node-22 --restart --no-input --json",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			packs := args[min(len(args), 1):]
			if err := validatePacks(packs); err != nil {
				return err
			}
			shouldWait, timeout, err := resolveRestartWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, picked, err := a.resolveCommandWorkspace(ctx, firstArg(args), nil, "", "")
			if err != nil {
				return err
			}
			ws, err := workspaceState(ctx, client, id, picked, restart || a.isInteractive())
			if err != nil {
				return err
			}
			if len(args) == 0 {
				page, err := collectAllPacks(ctx, client, &kibotos.PackListParams{Zone: ws.Zone})
				if err != nil {
					return err
				}
				if len(page.Data) > 0 {
					ids := make([]string, 0, len(page.Data))
					for _, item := range page.Data {
						ids = append(ids, item.ID)
					}
					_, _ = fmt.Fprintln(a.stderr, "Available packs:", strings.Join(ids, ", "))
				}
				input, err := a.prompter().Input("Pack IDs (comma-separated, blank to clear)", "", validatePackCSV)
				if err != nil {
					return err
				}
				packs = splitPackCSV(input)
			}
			if isAvailableWorkspace(*ws) && (restart || a.isInteractive()) && slices.Equal(packs, workspacePackIDs(ws)) {
				return a.renderWorkspace(ws)
			}
			if len(packs) == 0 && (!isAvailableWorkspace(*ws) || restart) {
				ok, err := a.confirmDestructive(fmt.Sprintf("Clear all Packs on %s?", resourceLabel(id, ws.Name)))
				if err != nil {
					return err
				}
				if !ok {
					return prompt.ErrCancelled
				}
			}
			return a.setWorkspacePacks(ctx, client, ws, packs, restart, shouldWait, timeout)
		},
	}
	addPackRestartFlags(cmd, &restart, &wait, &noWait, &waitTimeout)
	return cmd
}

func (a *App) newWorkspacePackChangeCmd(add bool) *cobra.Command {
	var restart, wait, noWait bool
	var waitTimeout string
	name, short := "remove", "Remove a Pack from a workspace"
	var filter func(kibotos.Workspace) bool
	empty, title := "no workspaces with bound Packs found", "Select a workspace with bound Packs:"
	if add {
		name, short = "add", "Add a Pack to a workspace"
		empty, title = "", ""
	} else {
		filter = func(w kibotos.Workspace) bool { return len(w.Packs) > 0 }
	}

	cmd := &cobra.Command{
		Use:     name + " [workspace] [pack_id]",
		Short:   short,
		Example: fmt.Sprintf("  kibo ws pack %[1]s\n  kibo ws pack %[1]s wks_123 node-22 --restart --no-input --json", name),
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			packID := ""
			if len(args) == 2 {
				packID = strings.TrimSpace(args[1])
				if packID == "" {
					return missingPackRef()
				}
			}
			if !a.isInteractive() {
				if strings.TrimSpace(firstArg(args)) == "" {
					return missingWorkspaceRef()
				}
				if packID == "" {
					return missingPackRef()
				}
			}
			shouldWait, timeout, err := resolveRestartWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, id, ws, err := a.resolveCommandWorkspace(ctx, firstArg(args), filter, empty, title)
			if err != nil {
				return err
			}
			ws, err = client.Workspaces.Get(ctx, id)
			if err != nil {
				return err
			}
			packs := workspacePackIDs(ws)
			if packID == "" {
				if add {
					packID, _, err = a.resolvePack(ctx, client, "", ws.Zone, "Select a Pack to add:")
				} else {
					packID, err = a.pickBoundPack(packs)
				}
				if err != nil {
					return err
				}
			}
			index := slices.Index(packs, packID)
			if add {
				if index >= 0 {
					return a.renderWorkspace(ws)
				}
				packs = append(packs, packID)
				if err := validatePacks(packs); err != nil {
					return err
				}
			} else {
				if index < 0 {
					return a.renderWorkspace(ws)
				}
				packs = slices.Delete(packs, index, index+1)
			}
			return a.setWorkspacePacks(ctx, client, ws, packs, restart, shouldWait, timeout)
		},
	}
	addPackRestartFlags(cmd, &restart, &wait, &noWait, &waitTimeout)
	return cmd
}

func (a *App) pickBoundPack(packs []string) (string, error) {
	if len(packs) == 0 {
		return "", &CLIError{ExitCode: ExitError, Code: "no_matching_pack", Message: "workspace has no bound Packs"}
	}
	options := make([]prompt.Option, 0, len(packs))
	for _, id := range packs {
		options = append(options, prompt.Option{Value: id, Label: id})
	}
	choice, err := a.prompter().Select("Select a bound Pack to remove:", "Pack", options, 0)
	return choice.Value, err
}

func workspacePackIDs(ws *kibotos.Workspace) []string {
	packs := make([]string, 0, len(ws.Packs))
	for _, pack := range ws.Packs {
		packs = append(packs, pack.ID)
	}
	return packs
}

func addPackRestartFlags(cmd *cobra.Command, restart, wait, noWait *bool, waitTimeout *string) {
	cmd.Flags().BoolVar(restart, "restart", false, "Restart the running workspace so Pack changes take effect now")
	addWaitFlags(cmd, wait, noWait, waitTimeout, "the restarted workspace reaches available")
}

func (a *App) setWorkspacePacks(ctx context.Context, client *kibotos.Client, ws *kibotos.Workspace, packs []string, restart, shouldWait bool, timeout time.Duration) error {
	running := isAvailableWorkspace(*ws)
	if running && !restart && a.isInteractive() {
		question := "Restart workspace now for Pack changes to take effect?"
		if len(packs) == 0 {
			question = "Clear all Packs and restart the workspace now?"
		}
		if err := a.confirmTransition(question); err != nil {
			return err
		}
		restart = true
	}
	updated, err := client.Workspaces.SetPacks(ctx, ws.ID, packs)
	if err != nil {
		return err
	}
	if running && restart {
		updated, err = a.restartWorkspaceAndWait(ctx, client, updated, false, shouldWait, timeout)
		if err != nil {
			return err
		}
	}
	return a.renderWorkspace(updated)
}
