package cli

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/config"
	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

const (
	desktopCapability    = "desktop.v1"
	defaultDesktopPackID = "kibotos-desktop-env"
)

func hasDesktopCapability(ws *kibotos.Workspace) bool {
	return workspaceDesktopPackID(ws) != ""
}

func workspaceDesktopPackID(ws *kibotos.Workspace) string {
	for _, pack := range ws.Packs {
		if slices.Contains(pack.ProvidedCapabilities, desktopCapability) ||
			(len(pack.ProvidedCapabilities) == 0 && pack.ID == defaultDesktopPackID) {
			return pack.ID
		}
	}
	return ""
}

func resolveDesktopPackID(ctx context.Context, client *kibotos.Client, zone string) (string, error) {
	page, err := collectAllPacks(ctx, client, &kibotos.PackListParams{Zone: zone})
	if err != nil {
		return "", err
	}
	var firstMatch string
	for _, pack := range page.Data {
		if !slices.Contains(pack.ProvidedCapabilities, desktopCapability) {
			continue
		}
		if pack.ID == defaultDesktopPackID {
			return pack.ID, nil
		}
		if firstMatch == "" {
			firstMatch = pack.ID
		}
	}
	if firstMatch != "" {
		return firstMatch, nil
	}
	message := "No Pack provides desktop.v1"
	if zone != "" {
		message += fmt.Sprintf(" in zone %q", zone)
	}
	return "", &CLIError{
		ExitCode: ExitError,
		Code:     "desktop_pack_unavailable",
		Message:  message,
		Hint:     "Run `kibo pack list` to inspect available Packs.",
	}
}

func buildDesktopURL(consoleURL, wsID, orgID string) string {
	base := strings.TrimSpace(consoleURL)
	if end := strings.IndexAny(base, "?#"); end >= 0 {
		base = base[:end]
	}
	base = strings.TrimRight(base, "/")
	if base == "" {
		base = config.DefaultConsoleURL
	}
	desktopURL := base + "/desktop/" + wsID
	if strings.TrimSpace(orgID) != "" {
		desktopURL += "?org=" + url.QueryEscape(orgID)
	}
	return desktopURL
}

func resolveConsoleURL(override string, res config.Resolved) string {
	if override != "" {
		return override
	}
	return res.ConsoleURL
}

func validateDesktopConsoleURL(cmd *cobra.Command, raw string) error {
	if !cmd.Flags().Changed("console-url") {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return usageError("--console-url must be a valid HTTP or HTTPS URL")
	}
	return nil
}

func (a *App) newDesktopCmd() *cobra.Command {
	var (
		openFlag    bool
		startFlag   bool
		restartFlag bool
		consoleURL  string
		wait        bool
		noWait      bool
		waitTimeout string
	)
	cmd := &cobra.Command{
		Use:   "desktop [workspace]",
		Short: "Enable and open a workspace's remote desktop URL",
		Example: `  kibo desktop
  kibo desktop wks_123 --start --restart --no-input --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateDesktopConsoleURL(cmd, consoleURL); err != nil {
				return err
			}
			shouldWait, waitDur, err := resolveWait(cmd, wait, noWait, waitTimeout)
			if err != nil {
				return err
			}
			if !shouldWait {
				// Restart still needs to wait for the workspace to stop before starting it.
				seconds, err := parseDurationSeconds(waitTimeout, "wait-timeout")
				if err != nil {
					return err
				}
				waitDur = time.Duration(seconds) * time.Second
			}
			ref := strings.TrimSpace(firstArg(args))
			if ref == "" && !a.isInteractive() {
				return missingWorkspaceRef()
			}

			client, res, err := a.requireClient()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			wsID, ws, err := a.resolveWorkspace(ctx, client, ref, nil, "no workspaces found", "Select a workspace for desktop:")
			if err != nil {
				return err
			}
			if ws == nil {
				ws, err = client.Workspaces.Get(ctx, wsID)
				if err != nil {
					return err
				}
			}

			packID := workspaceDesktopPackID(ws)
			alreadyEnabled := packID != ""
			var nextPacks []string
			if !alreadyEnabled {
				packID, err = resolveDesktopPackID(ctx, client, ws.Zone)
				if err != nil {
					return err
				}
				nextPacks = make([]string, 0, len(ws.Packs)+1)
				for _, pack := range ws.Packs {
					nextPacks = append(nextPacks, pack.ID)
				}
				nextPacks = append(nextPacks, packID)
				if err := validatePacks(nextPacks); err != nil {
					return err
				}
			}

			// Ask before changing Pack bindings so cancelling leaves the workspace untouched.
			var start, restart bool
			switch {
			case ws.Status == kibotos.WorkspaceStatusStopped:
				start, err = a.confirmDesktopAction(startFlag, "Start workspace now to open desktop?")
			case ws.Status == kibotos.WorkspaceStatusAvailable && !alreadyEnabled:
				restart, err = a.confirmDesktopAction(restartFlag, "Restart workspace now to activate desktop?")
			}
			if err != nil {
				return err
			}
			if !alreadyEnabled {
				ws, err = client.Workspaces.SetPacks(ctx, wsID, nextPacks)
				if err != nil {
					return err
				}
			}

			p := a.printer()
			restartRequired := ws.Status == kibotos.WorkspaceStatusAvailable && !alreadyEnabled
			if restartRequired && (restartFlag || restart) {
				ws, err = client.Workspaces.Stop(ctx, wsID, nil)
				if err != nil {
					return err
				}
				ws, err = waitForWorkspaceStatus(ctx, client, p, ws, kibotos.WorkspaceStatusStopped, waitDur)
				if err != nil {
					return err
				}
				restartRequired = false
				start = true
			}
			if ws.Status == kibotos.WorkspaceStatusStopped && (startFlag || start) {
				ws, err = client.Workspaces.Start(ctx, wsID, nil)
				if err != nil {
					return err
				}
				if shouldWait {
					ws, err = waitForWorkspaceStatus(ctx, client, p, ws, kibotos.WorkspaceStatusAvailable, waitDur)
					if err != nil {
						return err
					}
				}
			}

			desktopURL := buildDesktopURL(resolveConsoleURL(consoleURL, res), wsID, res.OrganizationID)
			if openFlag && !restartRequired && ws.Status == kibotos.WorkspaceStatusAvailable {
				a.openDesktopURL(p, desktopURL)
			}
			if p.IsJSON() {
				return p.JSON(desktopResult{
					WorkspaceID:     wsID,
					Status:          ws.Status,
					DesktopEnabled:  true,
					PackID:          packID,
					RestartRequired: restartRequired,
					URL:             desktopURL,
				})
			}
			if p.IsQuiet() {
				return p.ID(desktopURL)
			}
			if restartRequired {
				p.Warnf("Desktop Pack attached; restart the workspace (%s) for it to take effect.", wsID)
			}
			return p.Details([]output.KV{
				{Key: "Workspace", Value: wsID},
				{Key: "Status", Value: p.FormatStatus(string(ws.Status))},
				{Key: "Desktop Pack", Value: packID},
				{Key: "Desktop URL", Value: p.Link(desktopURL)},
			})
		},
	}

	f := cmd.Flags()
	f.BoolVar(&openFlag, "open", false, "Open the desktop URL in your browser")
	f.BoolVar(&startFlag, "start", false, "Start the workspace if it is stopped")
	f.BoolVar(&restartFlag, "restart", false, "Restart a running workspace if desktop was just enabled")
	f.StringVar(&consoleURL, "console-url", "", "Override Kibotos console URL used for desktop links")
	addWaitFlags(cmd, &wait, &noWait, &waitTimeout, "the workspace reaches available")
	return cmd
}

type desktopResult struct {
	WorkspaceID     string                  `json:"workspace_id"`
	Status          kibotos.WorkspaceStatus `json:"status"`
	DesktopEnabled  bool                    `json:"desktop_enabled"`
	PackID          string                  `json:"pack_id"`
	RestartRequired bool                    `json:"restart_required"`
	URL             string                  `json:"url"`
}

func (a *App) confirmDesktopAction(requested bool, message string) (bool, error) {
	if requested || !a.isInteractive() {
		return requested, nil
	}
	if a.flagYes {
		return true, nil
	}
	return a.prompter().Confirm(message, true)
}

func (a *App) openDesktopURL(p *output.Printer, desktopURL string) {
	if err := a.openBrowser(desktopURL); err != nil {
		p.Warnf("Could not open the desktop URL in your browser: %v", err)
	}
}
