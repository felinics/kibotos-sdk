package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
	"github.com/felinics/kibotos-sdk/cli/internal/config"
	"github.com/felinics/kibotos-sdk/cli/internal/output"
)

const (
	defaultWaitTimeoutSeconds     = 300
	defaultWaitCmdTimeoutSeconds  = 30
	defaultPollIntervalForPolling = 1 * time.Second
)

// BuildInfo carries CLI version metadata injected at build time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Options configures a CLI application instance (primarily for testing and embedding).
type Options struct {
	Stdin                io.Reader
	Stdout               io.Writer
	Stderr               io.Writer
	Store                *config.Store
	EnvLookup            func(string) string
	HTTPClient           *http.Client
	OpenBrowser          func(string) error
	PollIntervalOverride time.Duration
	Build                BuildInfo
}

// App holds shared runtime dependencies and global flags for a kibo CLI invocation.
type App struct {
	stdin                io.Reader
	stdout               io.Writer
	stderr               io.Writer
	store                *config.Store
	envLookup            func(string) string
	httpClient           *http.Client
	openBrowser          func(string) error
	pollIntervalOverride time.Duration
	build                BuildInfo

	// Global persistent flags
	flagJSON    bool
	flagOutput  string
	flagProfile string
	flagAPIKey  string
	flagAPIURL  string
}

// NewRootCommand builds the complete `kibo` cobra command tree.
func NewRootCommand(opts Options) *cobra.Command {
	app := newApp(opts)

	root := &cobra.Command{
		Use:           "kibo",
		Short:         "Official CLI for Kibotos workspaces, volumes, and exposures",
		Long:          "kibo is the official command-line interface for Kibotos.\nCreate and manage cloud microVM workspaces, persistent volumes, and temporary HTTPS port exposures.",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return app.validateOutputFlags()
		},
	}

	root.SetIn(app.stdin)
	root.SetOut(app.stdout)
	root.SetErr(app.stderr)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return wrapUsageError(err)
	})

	pf := root.PersistentFlags()
	pf.BoolVar(&app.flagJSON, "json", false, "Output valid JSON to stdout (shorthand for -o json)")
	pf.StringVarP(&app.flagOutput, "output", "o", "table", "Output format: table or json")
	pf.StringVar(&app.flagProfile, "profile", "", "Profile name to use")
	pf.StringVar(&app.flagAPIKey, "api-key", "", "Override Kibotos API key for this command")
	pf.StringVar(&app.flagAPIURL, "api-url", "", "Override Kibotos API base URL")

	root.AddCommand(
		app.newLoginCmd(),
		app.newLogoutCmd(),
		app.newWhoamiCmd(),
		app.newProfileCmd(),
		app.newZoneCmd(),
		app.newTemplateCmd(),
		app.newWorkspaceCmd(),
		app.newVolumeCmd(),
		app.newExposureCmd(),
		app.newVersionCmd(),
	)

	return root
}

// Execute runs the kibo CLI with args and returns the process exit code.
func Execute(ctx context.Context, args []string, opts Options) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}

	root := NewRootCommand(opts)
	root.SetArgs(args)

	if err := root.ExecuteContext(ctx); err != nil {
		code, detail := classifyError(err)
		if argsWantJSON(args) {
			p := &output.Printer{Out: opts.Stdout, Err: opts.Stderr, Format: output.FormatJSON}
			p.ErrorJSON(jsonErrorEnvelope{Error: detail})
		} else {
			_, _ = fmt.Fprintf(opts.Stderr, "Error: %s\n", detail.Message)
			if detail.RequestID != "" {
				_, _ = fmt.Fprintf(opts.Stderr, "Request ID: %s\n", detail.RequestID)
			}
			if detail.Hint != "" {
				_, _ = fmt.Fprintf(opts.Stderr, "Hint: %s\n", detail.Hint)
			}
		}
		return code
	}
	return ExitOK
}

func newApp(opts Options) *App {
	envLookup := opts.EnvLookup
	if envLookup == nil {
		envLookup = os.Getenv
	}
	store := opts.Store
	if store == nil {
		store = config.NewStore(envLookup)
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	openBrowser := opts.OpenBrowser
	if openBrowser == nil {
		openBrowser = auth.OpenBrowser
	}
	build := opts.Build
	if build.Version == "" {
		build.Version = "0.1.0"
	}
	return &App{
		stdin:                stdin,
		stdout:               stdout,
		stderr:               stderr,
		store:                store,
		envLookup:            envLookup,
		httpClient:           opts.HTTPClient,
		openBrowser:          openBrowser,
		pollIntervalOverride: opts.PollIntervalOverride,
		build:                build,
	}
}

func (a *App) validateOutputFlags() error {
	if a.flagJSON {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(a.flagOutput)) {
	case "", "table", "json":
		return nil
	default:
		return usageError("invalid output format %q: must be table or json", a.flagOutput)
	}
}

func (a *App) outputFormat() output.Format {
	if a.flagJSON || strings.EqualFold(strings.TrimSpace(a.flagOutput), "json") {
		return output.FormatJSON
	}
	return output.FormatTable
}

func (a *App) printer() *output.Printer {
	return &output.Printer{
		Out:    a.stdout,
		Err:    a.stderr,
		Format: a.outputFormat(),
	}
}

func (a *App) userAgent() string {
	return "kibo-cli/" + a.build.Version
}

func (a *App) loadAndResolve(consoleURLOverride string) (*config.Config, config.Resolved, error) {
	cfg, err := a.store.Load()
	if err != nil {
		return nil, config.Resolved{}, err
	}
	res := a.store.Resolve(cfg, config.Overrides{
		Profile:    a.flagProfile,
		APIKey:     a.flagAPIKey,
		APIURL:     a.flagAPIURL,
		ConsoleURL: consoleURLOverride,
	})
	return cfg, res, nil
}

func (a *App) newClientFromResolved(res config.Resolved) (*kibotos.Client, error) {
	if strings.TrimSpace(res.APIKey) == "" {
		return nil, unauthenticatedError(
			"not authenticated: no Kibotos API key configured",
			"Run `kibo login` to sign in interactively, or set KIBOTOS_API_KEY.",
		)
	}
	opts := []kibotos.ClientOption{
		kibotos.WithBaseURL(res.APIURL),
		kibotos.WithUserAgent(a.userAgent()),
	}
	if a.httpClient != nil {
		opts = append(opts, kibotos.WithHTTPClient(a.httpClient))
	}
	client, err := kibotos.NewClient(res.APIKey, opts...)
	if err != nil {
		return nil, usageError("%v", err)
	}
	return client, nil
}

func (a *App) requireClient() (*kibotos.Client, config.Resolved, error) {
	_, res, err := a.loadAndResolve("")
	if err != nil {
		return nil, config.Resolved{}, err
	}
	client, err := a.newClientFromResolved(res)
	if err != nil {
		return nil, res, err
	}
	return client, res, nil
}

func (a *App) pollDelay() time.Duration {
	if a.pollIntervalOverride > 0 {
		return a.pollIntervalOverride
	}
	return defaultPollIntervalForPolling
}

func argsWantJSON(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
		if arg == "-o=json" || arg == "--output=json" {
			return true
		}
		if (arg == "-o" || arg == "--output") && i+1 < len(args) && strings.EqualFold(args[i+1], "json") {
			return true
		}
	}
	return false
}

func parseLabels(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 {
			return nil, usageError("invalid --label %q: expected key=value", entry)
		}
		k := strings.TrimSpace(entry[:idx])
		v := entry[idx+1:]
		if k == "" {
			return nil, usageError("invalid --label %q: label key cannot be empty", entry)
		}
		out[k] = v
	}
	return out, nil
}

func parseUpdateLabels(setEntries, unsetKeys []string) (map[string]*string, error) {
	if len(setEntries) == 0 && len(unsetKeys) == 0 {
		return nil, nil
	}
	out := make(map[string]*string, len(setEntries)+len(unsetKeys))
	for _, entry := range setEntries {
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 {
			return nil, usageError("invalid --label %q: expected key=value", entry)
		}
		k := strings.TrimSpace(entry[:idx])
		v := entry[idx+1:]
		if k == "" {
			return nil, usageError("invalid --label %q: label key cannot be empty", entry)
		}
		valCopy := v
		out[k] = &valCopy
	}
	for _, key := range unsetKeys {
		k := strings.TrimSpace(key)
		if k == "" {
			return nil, usageError("invalid --unset-label: key cannot be empty")
		}
		if _, exists := out[k]; exists {
			return nil, usageError("label %q cannot be both set and unset in the same update", k)
		}
		out[k] = nil
	}
	return out, nil
}

func parseTimeoutAction(raw string) (kibotos.TimeoutAction, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", nil
	case string(kibotos.TimeoutActionStop):
		return kibotos.TimeoutActionStop, nil
	case string(kibotos.TimeoutActionDelete):
		return kibotos.TimeoutActionDelete, nil
	default:
		return "", usageError("invalid timeout action %q: must be stop or delete", raw)
	}
}

func parseNetworkMode(raw string) (kibotos.NetworkMode, error) {
	switch strings.TrimSpace(raw) {
	case string(kibotos.NetworkModePublicInternet):
		return kibotos.NetworkModePublicInternet, nil
	case string(kibotos.NetworkModeDenyAll):
		return kibotos.NetworkModeDenyAll, nil
	default:
		return "", usageError("invalid network mode %q: must be public_internet or deny_all", raw)
	}
}

func parseTemplateOwner(raw string) (kibotos.TemplateOwner, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return "", nil
	case string(kibotos.TemplateOwnerPlatform):
		return kibotos.TemplateOwnerPlatform, nil
	case string(kibotos.TemplateOwnerOrganization):
		return kibotos.TemplateOwnerOrganization, nil
	default:
		return "", usageError("invalid template owner %q: must be platform or organization", raw)
	}
}

func parseOptionalBool(raw, flagName string) (*bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, usageError("invalid --%s value %q: must be true or false", flagName, raw)
	}
	return &v, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
