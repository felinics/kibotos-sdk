package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"
)

func (a *App) newOpenCmd() *cobra.Command {
	var ttl string
	var noOpen bool
	cmd := &cobra.Command{
		Use:     "open [workspace] [port]",
		Short:   "Expose a workspace port over HTTPS and open or print its URL",
		Example: "  kibo open\n  kibo open wks_123 8080 --ttl 30m --no-input --json",
		Args:    cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var params kibotos.CreateExposureParams
			var err error
			if cmd.Flags().Changed("ttl") {
				params.TTLSeconds, err = parseExposureTTLSeconds(ttl)
				if err != nil {
					return err
				}
			}
			if len(args) == 2 {
				raw := strings.TrimSpace(args[1])
				if err := validatePort(raw); err != nil {
					return usageError("invalid port %q: must be between 1 and 65535", args[1])
				}
				params.Port, _ = strconv.Atoi(raw)
			}
			if !a.isInteractive() {
				if strings.TrimSpace(firstArg(args)) == "" {
					return missingWorkspaceRef()
				}
				if params.Port == 0 {
					return usageErrorWithHint("Pass a port, for example `kibo open wks_123 8080 --no-open`.", "workspace port is required")
				}
			}
			ctx := cmd.Context()
			client, id, err := a.resolveAvailableWorkspace(ctx, firstArg(args))
			if err != nil {
				return err
			}
			if params.Port == 0 {
				raw, err := a.prompter().Input("Port (1-65535)", "8080", validatePort)
				if err != nil {
					return err
				}
				params.Port, _ = strconv.Atoi(raw)
			}
			var exposure *kibotos.Exposure
			now := time.Now()
			for item, err := range client.Exposures.Iter(ctx, id, nil) {
				if err != nil {
					return err
				}
				if item.Port == params.Port && item.Status == kibotos.ExposureStatusActive &&
					(item.ExpiresAt.IsZero() || item.ExpiresAt.After(now)) {
					exposure = &item
					break
				}
			}
			if exposure == nil {
				exposure, err = client.Exposures.Create(ctx, id, params)
				if err != nil {
					return err
				}
			}
			p := a.printer()
			if p.IsJSON() {
				return p.JSON(exposure)
			}
			if _, err := fmt.Fprintln(p.Out, exposure.URL); err != nil {
				return err
			}
			if !noOpen && !p.IsQuiet() {
				if err := a.openBrowser(exposure.URL); err != nil {
					p.Warnf("Could not open the exposure URL in your browser: %v", err)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ttl, "ttl", "", "New exposure lifetime (e.g. 1h, 30m, 3600; API default: 3600s)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Print the URL without launching a browser")
	return cmd
}
