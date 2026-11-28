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

	"github.com/fatih/color"
	kibotos "github.com/felinics/kibotos-sdk/sdks/go"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/felinics/kibotos-sdk/cli/internal/auth"
	"github.com/felinics/kibotos-sdk/cli/internal/config"
	"github.com/felinics/kibotos-sdk/cli/internal/output"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
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
	InteractiveOverride  *bool
	ColorOverride        *bool
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
	interactiveOverride  *bool
	colorOverride        *bool
	build                BuildInfo
	prompterInst         *prompt.Prompter

	// Global persistent flags
	flagJSON    bool
	flagOutput  string
	flagQuiet   bool
	flagNoInput bool
	flagYes     bool
	flagNoColor bool
	flagProfile string
	flagOrg     string
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
			return app.validateOutputFlags(cmd)
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
	pf.StringVarP(&app.flagOutput, "output", "o", "", "Output format: table or json (env: KIBO_OUTPUT)")
	pf.BoolVarP(&app.flagQuiet, "quiet", "q", false, "Only output resource IDs to stdout and suppress progress messages")
	pf.BoolVar(&app.flagNoInput, "no-input", false, "Disable interactive prompts and fail fast on missing inputs (env: KIBO_NO_INPUT)")
	pf.BoolVarP(&app.flagYes, "yes", "y", false, "Skip interactive confirmation prompts")
	pf.BoolVar(&app.flagNoColor, "no-color", false, "Disable colored output (env: NO_COLOR)")
	pf.StringVar(&app.flagProfile, "profile", "", "Profile name to use")
	pf.StringVar(&app.flagOrg, "org", "", "Override organization ID for this command (env: KIBO_ORG)")
	pf.StringVar(&app.flagAPIKey, "api-key", "", "Override Kibotos API key for this command")
	pf.StringVar(&app.flagAPIURL, "api-url", "", "Override Kibotos API base URL")

	root.AddCommand(
		app.newLoginCmd(),
		app.newLogoutCmd(),
		app.newWhoamiCmd(),
		app.newOrgCmd(),
		app.newProfileCmd(),
		app.newZoneCmd(),
		app.newTemplateCmd(),
		app.newWorkspaceCmd(),
		app.newVolumeCmd(),
		app.newExposureCmd(),
		app.newExecCmd(),
		app.newFilesCmd(),
		app.newTunnelCmd(),
		app.newPackCmd(),
		app.newEgressCmd(),
		app.newResolverCmd(),
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
	envLookup := opts.EnvLookup
	if envLookup == nil {
		envLookup = os.Getenv
	}

	root := NewRootCommand(opts)
	root.SetArgs(args)

	if err := root.ExecuteContext(ctx); err != nil {
		if status, ok := err.(processStatus); ok {
			return int(status)
		}
		code, detail := classifyError(err)
		if argsWantJSON(args, envLookup) {
			p := &output.Printer{Out: opts.Stdout, Err: opts.Stderr, Format: output.FormatJSON}
			p.ErrorJSON(jsonErrorEnvelope{Error: detail})
		} else {
			colorErr := stderrColorForExecute(args, opts, envLookup)
			errLabel := styleText(colorErr, color.FgRed, color.Bold, "Error:")
			_, _ = fmt.Fprintf(opts.Stderr, "%s %s\n", errLabel, detail.Message)
			if formattedDetails := formatErrorDetails(detail.Details); formattedDetails != "" {
				detLabel := styleText(colorErr, color.Faint, color.FgWhite, "Details:")
				_, _ = fmt.Fprintf(opts.Stderr, "%s %s\n", detLabel, formattedDetails)
			}
			if detail.RequestID != "" {
				reqLabel := styleText(colorErr, color.Faint, color.FgWhite, "Request ID:")
				_, _ = fmt.Fprintf(opts.Stderr, "%s %s\n", reqLabel, detail.RequestID)
			}
			if detail.Hint != "" {
				hintLabel := styleText(colorErr, color.FgCyan, color.Bold, "Hint:")
				_, _ = fmt.Fprintf(opts.Stderr, "%s %s\n", hintLabel, detail.Hint)
			}
		}
		return code
	}
	return ExitOK
}

func styleText(enabled bool, attr1, attr2 color.Attribute, text string) string {
	c := color.New(attr1, attr2)
	if enabled {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c.Sprint(text)
}

func stderrColorForExecute(args []string, opts Options, envLookup func(string) string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--no-color" || arg == "--no-color=true" {
			return false
		}
	}
	if envLookup("NO_COLOR") != "" || strings.EqualFold(envLookup("TERM"), "dumb") {
		return false
	}
	if opts.ColorOverride != nil {
		return *opts.ColorOverride
	}
	return isTerminalWriter(opts.Stderr)
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
		interactiveOverride:  opts.InteractiveOverride,
		colorOverride:        opts.ColorOverride,
		build:                build,
	}
}

func (a *App) effectiveOutputRaw() string {
	if strings.TrimSpace(a.flagOutput) != "" {
		return strings.TrimSpace(a.flagOutput)
	}
	if v := strings.TrimSpace(a.envLookup("KIBO_OUTPUT")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.envLookup("KIBOTOS_OUTPUT")); v != "" {
		return v
	}
	return "table"
}

func (a *App) validateOutputFlags(cmd *cobra.Command) error {
	if a.flagJSON {
		return nil
	}
	raw := a.effectiveOutputRaw()
	switch strings.ToLower(raw) {
	case "", "table", "json":
		return nil
	default:
		return usageError("invalid output format %q: must be table or json", raw)
	}
}

func (a *App) outputFormat() output.Format {
	if a.flagJSON || strings.EqualFold(a.effectiveOutputRaw(), "json") {
		return output.FormatJSON
	}
	return output.FormatTable
}

func (a *App) isInteractive() bool {
	if a.flagNoInput || a.flagQuiet || a.outputFormat() == output.FormatJSON {
		return false
	}
	if isEnvTruthy(a.envLookup("KIBO_NO_INPUT")) ||
		isEnvTruthy(a.envLookup("KIBOTOS_NO_INPUT")) ||
		isEnvTruthy(a.envLookup("CI")) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(a.envLookup("TERM")), "dumb") {
		return false
	}
	if a.interactiveOverride != nil {
		return *a.interactiveOverride
	}
	return isTerminalReader(a.stdin) && isTerminalWriter(a.stderr)
}

func (a *App) colorEnabledFor(w io.Writer) bool {
	if a.flagNoColor || a.flagQuiet || a.outputFormat() == output.FormatJSON {
		return false
	}
	if a.envLookup("NO_COLOR") != "" || strings.EqualFold(strings.TrimSpace(a.envLookup("TERM")), "dumb") {
		return false
	}
	if a.colorOverride != nil {
		return *a.colorOverride
	}
	return isTerminalWriter(w)
}

func (a *App) prompter() *prompt.Prompter {
	if a.prompterInst == nil {
		a.prompterInst = prompt.New(a.stdin, a.stderr, a.colorEnabledFor(a.stderr))
	}
	return a.prompterInst
}

func (a *App) printer() *output.Printer {
	ttyErr := isTerminalWriter(a.stderr)
	if a.interactiveOverride != nil {
		ttyErr = *a.interactiveOverride
	}
	return &output.Printer{
		Out:      a.stdout,
		Err:      a.stderr,
		Format:   a.outputFormat(),
		Quiet:    a.flagQuiet,
		ColorOut: a.colorEnabledFor(a.stdout),
		ColorErr: a.colorEnabledFor(a.stderr),
		TTYErr:   ttyErr,
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
		Org:        a.flagOrg,
	})
	return cfg, res, nil
}

type orgHeaderTransport struct {
	base  http.RoundTripper
	orgID string
}

func (t *orgHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if t.orgID != "" && clone.Header.Get("X-Kibotos-Organization") == "" {
		clone.Header.Set("X-Kibotos-Organization", t.orgID)
	}
	return t.base.RoundTrip(clone)
}

func (a *App) newClientFromResolved(res config.Resolved) (*kibotos.Client, error) {
	cred := res.Credential()
	if cred == "" {
		return nil, unauthenticatedError(
			"not authenticated: no Kibotos session or API key configured",
			"Run `kibo login` to sign in interactively, or set KIBOTOS_API_KEY.",
		)
	}
	opts := []kibotos.ClientOption{
		kibotos.WithBaseURL(res.APIURL),
		kibotos.WithUserAgent(a.userAgent()),
	}
	hc := a.httpClient
	if res.AuthType == "user_session" && strings.TrimSpace(res.OrganizationID) != "" {
		baseClient := hc
		if baseClient == nil {
			baseClient = &http.Client{}
		}
		baseRT := baseClient.Transport
		if baseRT == nil {
			baseRT = http.DefaultTransport
		}
		wrapped := *baseClient
		wrapped.Transport = &orgHeaderTransport{base: baseRT, orgID: strings.TrimSpace(res.OrganizationID)}
		hc = &wrapped
	}
	if hc != nil {
		opts = append(opts, kibotos.WithHTTPClient(hc))
	}
	client, err := kibotos.NewClient(cred, opts...)
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

func argsWantJSON(args []string, envLookup func(string) string) bool {
	explicitOutput := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
		if strings.HasPrefix(arg, "-o=") {
			explicitOutput = strings.TrimPrefix(arg, "-o=")
		} else if strings.HasPrefix(arg, "--output=") {
			explicitOutput = strings.TrimPrefix(arg, "--output=")
		} else if (arg == "-o" || arg == "--output") && i+1 < len(args) {
			explicitOutput = args[i+1]
			i++
		}
	}
	if explicitOutput != "" {
		return strings.EqualFold(strings.TrimSpace(explicitOutput), "json")
	}
	if envLookup != nil {
		if v := strings.TrimSpace(envLookup("KIBO_OUTPUT")); v != "" {
			return strings.EqualFold(v, "json")
		}
		if v := strings.TrimSpace(envLookup("KIBOTOS_OUTPUT")); v != "" {
			return strings.EqualFold(v, "json")
		}
	}
	return false
}

func isEnvTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isTerminalReader(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
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
		if err := validateLabelKey(k); err != nil {
			return nil, err
		}
		if err := validateLabelValue(k, v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	if len(out) > maxUserLabels {
		return nil, usageError("too many labels (%d): at most %d labels are allowed", len(out), maxUserLabels)
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
		if err := validateLabelKey(k); err != nil {
			return nil, err
		}
		if err := validateLabelValue(k, v); err != nil {
			return nil, err
		}
		valCopy := v
		out[k] = &valCopy
	}
	for _, key := range unsetKeys {
		k := strings.TrimSpace(key)
		if err := validateLabelKey(k); err != nil {
			return nil, err
		}
		if _, exists := out[k]; exists {
			return nil, usageError("label %q cannot be both set and unset in the same update", k)
		}
		out[k] = nil
	}
	if len(out) > maxUserLabels {
		return nil, usageError("too many label changes (%d): at most %d labels are allowed", len(out), maxUserLabels)
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
