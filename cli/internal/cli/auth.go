package cli

import (
	"fmt"
	"os"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
	"github.com/felinics/kibotos-sdk/cli/internal/config"
	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

type loginResult struct {
	Profile          string `json:"profile"`
	AuthMethod       string `json:"auth_method"`
	APIURL           string `json:"api_url"`
	ConsoleURL       string `json:"console_url,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	UserEmail        string `json:"user_email,omitempty"`
	KeyID            string `json:"key_id,omitempty"`
	APIKeyMasked     string `json:"api_key_masked"`
	Verified         bool   `json:"verified"`
}

func (a *App) newLoginCmd() *cobra.Command {
	var (
		consoleURL string
		deviceName string
		noBrowser  bool
		skipVerify bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate kibo with your Kibotos account",
		Long: "Authenticate the kibo CLI using the interactive browser Device Authorization Flow,\n" +
			"or non-interactively by passing --api-key kb_... for CI and headless environments.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p := a.printer()

			cfg, res, err := a.loadAndResolve(consoleURL)
			if err != nil {
				return err
			}

			profileName := res.ProfileName
			if err := config.ValidateProfileName(profileName); err != nil {
				return usageError("%v", err)
			}

			// Non-interactive API key login when --api-key is explicitly passed on the CLI.
			if cmd.Flags().Changed("api-key") {
				key := strings.TrimSpace(a.flagAPIKey)
				if key == "" {
					return usageError("--api-key cannot be empty")
				}
				normalizedAPIURL, err := kibotos.NormalizeBaseURL(res.APIURL)
				if err != nil {
					return usageError("%v", err)
				}

				verified := false
				if !skipVerify {
					clientOpts := []kibotos.ClientOption{
						kibotos.WithBaseURL(normalizedAPIURL),
						kibotos.WithUserAgent(a.userAgent()),
					}
					if a.httpClient != nil {
						clientOpts = append(clientOpts, kibotos.WithHTTPClient(a.httpClient))
					}
					client, err := kibotos.NewClient(key, clientOpts...)
					if err != nil {
						return usageError("%v", err)
					}
					if _, err := client.Zones.List(ctx, &kibotos.ZoneListParams{Limit: 1}); err != nil {
						return err
					}
					verified = true
				}

				if cfg.Profiles == nil {
					cfg.Profiles = make(map[string]*config.Profile)
				}
				cfg.Profiles[profileName] = &config.Profile{
					APIKey:     key,
					APIURL:     normalizedAPIURL,
					ConsoleURL: res.ConsoleURL,
				}
				cfg.CurrentProfile = profileName

				if err := a.store.Save(cfg); err != nil {
					return err
				}

				result := loginResult{
					Profile:      profileName,
					AuthMethod:   "api_key",
					APIURL:       normalizedAPIURL,
					ConsoleURL:   res.ConsoleURL,
					APIKeyMasked: config.MaskAPIKey(key),
					Verified:     verified,
				}
				if p.IsJSON() {
					return p.JSON(result)
				}
				return p.Details([]output.KV{
					{Key: "Status", Value: "Authenticated"},
					{Key: "Profile", Value: result.Profile},
					{Key: "Method", Value: result.AuthMethod},
					{Key: "API URL", Value: result.APIURL},
					{Key: "API Key", Value: result.APIKeyMasked},
					{Key: "Verified", Value: fmt.Sprintf("%t", result.Verified)},
				})
			}

			// Default interactive Device Authorization Flow.
			if strings.TrimSpace(deviceName) == "" {
				if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
					deviceName = strings.TrimSpace(host)
				}
			}

			devClient, err := auth.NewDeviceClient(res.ConsoleURL, a.httpClient, a.userAgent())
			if err != nil {
				return usageError("%v", err)
			}
			if a.pollIntervalOverride > 0 {
				devClient.PollIntervalOverride = a.pollIntervalOverride
			}

			start, err := devClient.Start(ctx, deviceName)
			if err != nil {
				return err
			}

			p.Statusf("To sign in to Kibotos, open this URL in your browser:")
			p.Statusf("  %s", start.VerificationURLComplete)
			p.Statusf("Verification code: %s", start.UserCode)

			if !noBrowser && start.VerificationURLComplete != "" {
				if err := a.openBrowser(start.VerificationURLComplete); err != nil {
					p.Statusf("Could not open browser automatically (%v). Open the URL above manually.", err)
				}
			}

			p.Statusf("Waiting for authorization in browser...")
			tok, err := devClient.Poll(ctx, start)
			if err != nil {
				return err
			}

			effectiveAPIURL := res.APIURL
			if !cmd.Flags().Changed("api-url") && strings.TrimSpace(tok.APIURL) != "" {
				effectiveAPIURL = strings.TrimSpace(tok.APIURL)
			}
			normalizedAPIURL, err := kibotos.NormalizeBaseURL(effectiveAPIURL)
			if err != nil {
				return usageError("%v", err)
			}

			if cfg.Profiles == nil {
				cfg.Profiles = make(map[string]*config.Profile)
			}
			cfg.Profiles[profileName] = &config.Profile{
				APIKey:           tok.APIKey,
				APIURL:           normalizedAPIURL,
				ConsoleURL:       devClient.ConsoleURL,
				OrganizationID:   tok.OrganizationID,
				OrganizationName: tok.OrganizationName,
				UserEmail:        tok.UserEmail,
				KeyID:            tok.KeyID,
			}
			cfg.CurrentProfile = profileName

			if err := a.store.Save(cfg); err != nil {
				return err
			}

			result := loginResult{
				Profile:          profileName,
				AuthMethod:       "device_flow",
				APIURL:           normalizedAPIURL,
				ConsoleURL:       devClient.ConsoleURL,
				OrganizationID:   tok.OrganizationID,
				OrganizationName: tok.OrganizationName,
				UserEmail:        tok.UserEmail,
				KeyID:            tok.KeyID,
				APIKeyMasked:     config.MaskAPIKey(tok.APIKey),
				Verified:         true,
			}
			if p.IsJSON() {
				return p.JSON(result)
			}
			items := []output.KV{
				{Key: "Status", Value: "Authenticated"},
				{Key: "Profile", Value: result.Profile},
				{Key: "Organization", Value: formatOrg(result.OrganizationName, result.OrganizationID)},
				{Key: "User", Value: output.EmptyDash(result.UserEmail)},
				{Key: "API URL", Value: result.APIURL},
				{Key: "Key ID", Value: output.EmptyDash(result.KeyID)},
				{Key: "API Key", Value: result.APIKeyMasked},
			}
			return p.Details(items)
		},
	}

	f := cmd.Flags()
	f.StringVar(&consoleURL, "console-url", "", "Kibotos Console URL for device authorization")
	f.StringVar(&deviceName, "device-name", "", "Device label shown during browser approval (defaults to hostname)")
	f.BoolVar(&noBrowser, "no-browser", false, "Do not attempt to open the verification URL in a browser")
	f.BoolVar(&skipVerify, "skip-verify", false, "Skip live API key verification when using --api-key")

	return cmd
}

type logoutResult struct {
	Profile string `json:"profile"`
	Removed bool   `json:"removed"`
}

func (a *App) newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove saved credentials for a profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			cfg, res, err := a.loadAndResolve("")
			if err != nil {
				return err
			}

			profileName := res.ProfileName
			_, existed := cfg.Profiles[profileName]
			if existed {
				delete(cfg.Profiles, profileName)
				if cfg.CurrentProfile == profileName {
					names := cfg.SortedProfileNames()
					if len(names) > 0 {
						cfg.CurrentProfile = names[0]
					} else {
						cfg.CurrentProfile = ""
					}
				}
				if err := a.store.Save(cfg); err != nil {
					return err
				}
			}

			result := logoutResult{
				Profile: profileName,
				Removed: existed,
			}
			if p.IsJSON() {
				return p.JSON(result)
			}
			if existed {
				_, _ = fmt.Fprintf(p.Out, "Logged out of profile %q.\n", profileName)
			} else {
				_, _ = fmt.Fprintf(p.Out, "Profile %q has no saved credentials.\n", profileName)
			}
			return nil
		},
	}
}

type whoamiResult struct {
	Authenticated    bool           `json:"authenticated"`
	Profile          string         `json:"profile"`
	AuthSource       string         `json:"auth_source"`
	APIURL           string         `json:"api_url"`
	ConsoleURL       string         `json:"console_url,omitempty"`
	OrganizationID   string         `json:"organization_id,omitempty"`
	OrganizationName string         `json:"organization_name,omitempty"`
	UserEmail        string         `json:"user_email,omitempty"`
	KeyID            string         `json:"key_id,omitempty"`
	APIKeyMasked     string         `json:"api_key_masked"`
	Offline          bool           `json:"offline"`
	Zones            []kibotos.Zone `json:"zones,omitempty"`
}

func (a *App) newWhoamiCmd() *cobra.Command {
	var offline bool

	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show active Kibotos authentication, profile, and API connectivity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p := a.printer()

			_, res, err := a.loadAndResolve("")
			if err != nil {
				return err
			}
			if strings.TrimSpace(res.APIKey) == "" {
				return unauthenticatedError(
					"not authenticated: no Kibotos API key configured",
					"Run `kibo login` to sign in interactively, or set KIBOTOS_API_KEY.",
				)
			}

			normalizedAPIURL, err := kibotos.NormalizeBaseURL(res.APIURL)
			if err != nil {
				return usageError("%v", err)
			}

			result := whoamiResult{
				Authenticated:    true,
				Profile:          res.ProfileName,
				AuthSource:       res.AuthSource,
				APIURL:           normalizedAPIURL,
				ConsoleURL:       res.ConsoleURL,
				OrganizationID:   res.OrganizationID,
				OrganizationName: res.OrganizationName,
				UserEmail:        res.UserEmail,
				KeyID:            res.KeyID,
				APIKeyMasked:     config.MaskAPIKey(res.APIKey),
				Offline:          offline,
			}

			if !offline {
				client, err := a.newClientFromResolved(res)
				if err != nil {
					return err
				}
				var zones []kibotos.Zone
				for z, err := range client.Zones.Iter(ctx, nil) {
					if err != nil {
						return err
					}
					zones = append(zones, z)
				}
				result.Zones = zones
			}

			if p.IsJSON() {
				return p.JSON(result)
			}

			items := []output.KV{
				{Key: "Profile", Value: result.Profile},
				{Key: "Auth Source", Value: result.AuthSource},
				{Key: "Organization", Value: formatOrg(result.OrganizationName, result.OrganizationID)},
				{Key: "User", Value: output.EmptyDash(result.UserEmail)},
				{Key: "Key ID", Value: output.EmptyDash(result.KeyID)},
				{Key: "API Key", Value: result.APIKeyMasked},
				{Key: "API URL", Value: result.APIURL},
			}
			if offline {
				items = append(items, output.KV{Key: "Live Check", Value: "skipped (--offline)"})
			} else {
				defaultZone := "-"
				for _, z := range result.Zones {
					if z.Default {
						defaultZone = z.ID
						break
					}
				}
				items = append(items, output.KV{
					Key:   "Zones",
					Value: fmt.Sprintf("%d accessible (default: %s)", len(result.Zones), defaultZone),
				})
			}
			return p.Details(items)
		},
	}

	cmd.Flags().BoolVar(&offline, "offline", false, "Skip live API connectivity check")
	return cmd
}

type profileEntry struct {
	Name             string `json:"name"`
	Active           bool   `json:"active"`
	OrganizationID   string `json:"organization_id,omitempty"`
	OrganizationName string `json:"organization_name,omitempty"`
	UserEmail        string `json:"user_email,omitempty"`
	APIURL           string `json:"api_url,omitempty"`
	KeyID            string `json:"key_id,omitempty"`
	APIKeyMasked     string `json:"api_key_masked,omitempty"`
}

type profileListResult struct {
	CurrentProfile string         `json:"current_profile"`
	Profiles       []profileEntry `json:"profiles"`
}

func (a *App) newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "Manage named kibo authentication profiles",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List saved authentication profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			cfg, err := a.store.Load()
			if err != nil {
				return err
			}
			active := cfg.ActiveProfileName()
			names := cfg.SortedProfileNames()
			entries := make([]profileEntry, 0, len(names))
			for _, name := range names {
				prof := cfg.Profiles[name]
				entries = append(entries, profileEntry{
					Name:             name,
					Active:           name == active,
					OrganizationID:   prof.OrganizationID,
					OrganizationName: prof.OrganizationName,
					UserEmail:        prof.UserEmail,
					APIURL:           prof.APIURL,
					KeyID:            prof.KeyID,
					APIKeyMasked:     config.MaskAPIKey(prof.APIKey),
				})
			}

			if p.IsJSON() {
				return p.JSON(profileListResult{
					CurrentProfile: cfg.CurrentProfile,
					Profiles:       entries,
				})
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(p.Out, "No profiles configured. Run `kibo login` to sign in.")
				return nil
			}
			rows := make([][]string, 0, len(entries))
			for _, e := range entries {
				marker := ""
				if e.Active {
					marker = "*"
				}
				rows = append(rows, []string{
					marker,
					e.Name,
					formatOrg(e.OrganizationName, e.OrganizationID),
					output.EmptyDash(e.UserEmail),
					output.EmptyDash(e.APIURL),
					output.EmptyDash(e.APIKeyMasked),
				})
			}
			return p.Table([]string{"ACTIVE", "NAME", "ORGANIZATION", "USER", "API URL", "API KEY"}, rows)
		},
	}

	useCmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Switch the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			name := strings.TrimSpace(args[0])
			if err := config.ValidateProfileName(name); err != nil {
				return usageError("%v", err)
			}
			cfg, err := a.store.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[name]; !ok {
				return &CLIError{
					ExitCode: ExitError,
					Code:     "profile_not_found",
					Message:  fmt.Sprintf("profile %q does not exist", name),
					Hint:     "Run `kibo profile list` to see available profiles, or `kibo login --profile " + name + "` to create it.",
				}
			}
			cfg.CurrentProfile = name
			if err := a.store.Save(cfg); err != nil {
				return err
			}
			if p.IsJSON() {
				return p.JSON(map[string]any{
					"current_profile": name,
				})
			}
			_, _ = fmt.Fprintf(p.Out, "Active profile set to %q.\n", name)
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a saved profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.printer()
			name := strings.TrimSpace(args[0])
			if err := config.ValidateProfileName(name); err != nil {
				return usageError("%v", err)
			}
			cfg, err := a.store.Load()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[name]; !ok {
				return &CLIError{
					ExitCode: ExitError,
					Code:     "profile_not_found",
					Message:  fmt.Sprintf("profile %q does not exist", name),
				}
			}
			delete(cfg.Profiles, name)
			if cfg.CurrentProfile == name {
				names := cfg.SortedProfileNames()
				if len(names) > 0 {
					cfg.CurrentProfile = names[0]
				} else {
					cfg.CurrentProfile = ""
				}
			}
			if err := a.store.Save(cfg); err != nil {
				return err
			}
			if p.IsJSON() {
				return p.JSON(map[string]any{
					"deleted":         name,
					"current_profile": cfg.CurrentProfile,
				})
			}
			_, _ = fmt.Fprintf(p.Out, "Deleted profile %q.\n", name)
			return nil
		},
	}

	cmd.AddCommand(listCmd, useCmd, deleteCmd)
	return cmd
}

func formatOrg(name, id string) string {
	switch {
	case name != "" && id != "":
		return fmt.Sprintf("%s (%s)", name, id)
	case name != "":
		return name
	case id != "":
		return id
	default:
		return "-"
	}
}
