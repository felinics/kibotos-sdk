package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	kibotos "github.com/felinics/kibotos-sdk/sdks/go"

	"github.com/felinics/kibotos-sdk/cli/internal/config"
	"github.com/felinics/kibotos-sdk/cli/internal/prompt"
)

// confirmDestructive asks the user for confirmation when running interactively without --yes.
// In non-interactive mode (agents, scripts, CI, --json, --no-input), it returns true immediately.
func (a *App) confirmDestructive(message string) (bool, error) {
	if a.flagYes || !a.isInteractive() {
		return true, nil
	}
	return a.prompter().Confirm(message, false)
}

// resolveWorkspace resolves an optional workspace ID or name argument.
// - Fast path: if ref starts with "wks_", returns it directly without extra API calls.
// - Name path: lists workspaces and matches by exact name (interactive pick on ambiguity, error in non-interactive).
// - Missing path: in interactive mode, presents a filtered list picker; in non-interactive mode, returns exit 2 with hint.
func (a *App) resolveWorkspace(
	ctx context.Context,
	client *kibotos.Client,
	ref string,
	filter func(kibotos.Workspace) bool,
	emptyFilterMsg string,
	promptTitle string,
) (string, *kibotos.Workspace, error) {
	ref = strings.TrimSpace(ref)

	if ref == "" {
		if !a.isInteractive() {
			return "", nil, missingWorkspaceRef()
		}
		return a.pickWorkspace(ctx, client, filter, emptyFilterMsg, promptTitle, nil)
	}

	// Fast path for explicit workspace IDs: zero extra round-trips.
	if strings.HasPrefix(ref, "wks_") {
		return ref, nil, nil
	}

	// Name lookup fallback.
	page, err := collectAllWorkspaces(ctx, client, nil)
	if err != nil {
		return "", nil, err
	}
	var exact, ci []kibotos.Workspace
	for _, ws := range page.Data {
		if ws.ID == ref {
			return ws.ID, &ws, nil
		}
		if ws.Name == ref {
			exact = append(exact, ws)
		} else if ws.Name != "" && strings.EqualFold(ws.Name, ref) {
			ci = append(ci, ws)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = ci
	}
	switch len(matches) {
	case 0:
		// Fall through to let API return workspace_not_found if ref was a custom ID.
		return ref, nil, nil
	case 1:
		return matches[0].ID, &matches[0], nil
	default:
		if a.isInteractive() {
			opts := make([]prompt.Option, 0, len(matches))
			byID := make(map[string]kibotos.Workspace, len(matches))
			for _, ws := range matches {
				if filter != nil && !filter(ws) {
					continue
				}
				byID[ws.ID] = ws
				opts = append(opts, prompt.Option{
					Value:       ws.ID,
					Label:       fmt.Sprintf("%s (%s)", ws.Name, ws.ID),
					Description: fmt.Sprintf("%s · %s · %s", ws.Status, ws.Zone, ws.Template.ID),
				})
			}
			if len(opts) == 0 {
				return "", nil, &CLIError{ExitCode: ExitError, Code: "no_matching_workspace", Message: emptyFilterMsg}
			}
			choice, err := a.prompter().Select(fmt.Sprintf("Multiple workspaces named %q:", ref), "Workspace", opts, 0)
			if err != nil {
				return "", nil, err
			}
			ws := byID[choice.Value]
			return ws.ID, &ws, nil
		}
		ids := make([]string, 0, len(matches))
		for _, ws := range matches {
			ids = append(ids, ws.ID)
		}
		return "", nil, &CLIError{
			ExitCode: ExitUsage,
			Code:     "ambiguous_name",
			Message:  fmt.Sprintf("multiple workspaces match name %q: %s", ref, strings.Join(ids, ", ")),
			Hint:     "Pass the explicit workspace ID (wks_...) instead.",
			Details:  map[string]any{"candidates": ids},
		}
	}
}

func (a *App) pickWorkspace(ctx context.Context, client *kibotos.Client, filter func(kibotos.Workspace) bool, emptyFilterMsg, promptTitle string, compare func(kibotos.Workspace, kibotos.Workspace) int) (string, *kibotos.Workspace, error) {
	page, err := collectAllWorkspaces(ctx, client, nil)
	if err != nil {
		return "", nil, err
	}
	candidates := make([]kibotos.Workspace, 0, len(page.Data))
	for _, ws := range page.Data {
		if filter == nil || filter(ws) {
			candidates = append(candidates, ws)
		}
	}
	if len(candidates) == 0 {
		if emptyFilterMsg == "" {
			emptyFilterMsg = "no workspaces found"
		}
		return "", nil, &CLIError{
			ExitCode: ExitError,
			Code:     "no_matching_workspace",
			Message:  emptyFilterMsg,
		}
	}
	if compare != nil {
		slices.SortStableFunc(candidates, compare)
	}
	opts := make([]prompt.Option, 0, len(candidates))
	byID := make(map[string]kibotos.Workspace, len(candidates))
	for _, ws := range candidates {
		byID[ws.ID] = ws
		label := ws.ID
		if ws.Name != "" {
			label = fmt.Sprintf("%s (%s)", ws.Name, ws.ID)
		}
		desc := fmt.Sprintf("%s · %s · %s", ws.Status, ws.Zone, ws.Template.ID)
		opts = append(opts, prompt.Option{
			Value:       ws.ID,
			Label:       label,
			Description: desc,
		})
	}
	if promptTitle == "" {
		promptTitle = "Select a workspace:"
	}
	choice, err := a.prompter().Select(promptTitle, "Workspace", opts, 0)
	if err != nil {
		return "", nil, err
	}
	ws := byID[choice.Value]
	return ws.ID, &ws, nil
}

// resolveVolume resolves an optional volume ID or name argument.
func (a *App) resolveVolume(
	ctx context.Context,
	client *kibotos.Client,
	ref string,
	filter func(kibotos.Volume) bool,
	emptyFilterMsg string,
	promptTitle string,
) (string, *kibotos.Volume, error) {
	ref = strings.TrimSpace(ref)

	if ref == "" {
		if !a.isInteractive() {
			return "", nil, missingVolumeRef()
		}
		page, err := collectAllVolumes(ctx, client, nil)
		if err != nil {
			return "", nil, err
		}
		candidates := make([]kibotos.Volume, 0, len(page.Data))
		for _, v := range page.Data {
			if filter == nil || filter(v) {
				candidates = append(candidates, v)
			}
		}
		if len(candidates) == 0 {
			if emptyFilterMsg == "" {
				emptyFilterMsg = "no volumes found"
			}
			return "", nil, &CLIError{
				ExitCode: ExitError,
				Code:     "no_matching_volume",
				Message:  emptyFilterMsg,
			}
		}
		opts := make([]prompt.Option, 0, len(candidates))
		byID := make(map[string]kibotos.Volume, len(candidates))
		for _, v := range candidates {
			byID[v.ID] = v
			label := v.ID
			if v.Name != "" {
				label = fmt.Sprintf("%s (%s)", v.Name, v.ID)
			}
			desc := fmt.Sprintf("%d GiB · %s · %s", v.SizeGiB, v.Status, v.Zone)
			opts = append(opts, prompt.Option{
				Value:       v.ID,
				Label:       label,
				Description: desc,
			})
		}
		if promptTitle == "" {
			promptTitle = "Select a volume:"
		}
		choice, err := a.prompter().Select(promptTitle, "Volume", opts, 0)
		if err != nil {
			return "", nil, err
		}
		v := byID[choice.Value]
		return v.ID, &v, nil
	}

	if strings.HasPrefix(ref, "vol_") {
		return ref, nil, nil
	}

	page, err := collectAllVolumes(ctx, client, nil)
	if err != nil {
		return "", nil, err
	}
	var exact, ci []kibotos.Volume
	for _, v := range page.Data {
		if v.ID == ref {
			return v.ID, &v, nil
		}
		if v.Name == ref {
			exact = append(exact, v)
		} else if v.Name != "" && strings.EqualFold(v.Name, ref) {
			ci = append(ci, v)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = ci
	}
	switch len(matches) {
	case 0:
		return ref, nil, nil
	case 1:
		return matches[0].ID, &matches[0], nil
	default:
		if a.isInteractive() {
			opts := make([]prompt.Option, 0, len(matches))
			byID := make(map[string]kibotos.Volume, len(matches))
			for _, v := range matches {
				if filter != nil && !filter(v) {
					continue
				}
				byID[v.ID] = v
				opts = append(opts, prompt.Option{
					Value:       v.ID,
					Label:       fmt.Sprintf("%s (%s)", v.Name, v.ID),
					Description: fmt.Sprintf("%d GiB · %s · %s", v.SizeGiB, v.Status, v.Zone),
				})
			}
			if len(opts) == 0 {
				return "", nil, &CLIError{ExitCode: ExitError, Code: "no_matching_volume", Message: emptyFilterMsg}
			}
			choice, err := a.prompter().Select(fmt.Sprintf("Multiple volumes named %q:", ref), "Volume", opts, 0)
			if err != nil {
				return "", nil, err
			}
			v := byID[choice.Value]
			return v.ID, &v, nil
		}
		ids := make([]string, 0, len(matches))
		for _, v := range matches {
			ids = append(ids, v.ID)
		}
		return "", nil, &CLIError{
			ExitCode: ExitUsage,
			Code:     "ambiguous_name",
			Message:  fmt.Sprintf("multiple volumes match name %q: %s", ref, strings.Join(ids, ", ")),
			Hint:     "Pass the explicit volume ID (vol_...) instead.",
			Details:  map[string]any{"candidates": ids},
		}
	}
}

// resolveExposure resolves an optional exposure ID on a workspace.
func (a *App) resolveExposure(
	ctx context.Context,
	client *kibotos.Client,
	wsID string,
	ref string,
	includeRevoked bool,
	promptTitle string,
) (string, *kibotos.Exposure, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		return ref, nil, nil
	}
	if !a.isInteractive() {
		return "", nil, missingExposureRef()
	}
	var items []kibotos.Exposure
	for exp, err := range client.Exposures.Iter(ctx, wsID, &kibotos.ExposureListParams{IncludeRevoked: includeRevoked}) {
		if err != nil {
			return "", nil, err
		}
		items = append(items, exp)
	}
	if len(items) == 0 {
		return "", nil, &CLIError{
			ExitCode: ExitError,
			Code:     "no_matching_exposure",
			Message:  fmt.Sprintf("no exposures found for workspace %s", wsID),
		}
	}
	opts := make([]prompt.Option, 0, len(items))
	byID := make(map[string]kibotos.Exposure, len(items))
	for _, exp := range items {
		byID[exp.ID] = exp
		opts = append(opts, prompt.Option{
			Value:       exp.ID,
			Label:       fmt.Sprintf("%s (port %d)", exp.ID, exp.Port),
			Description: fmt.Sprintf("%s · %s", exp.Status, exp.URL),
		})
	}
	if promptTitle == "" {
		promptTitle = "Select an exposure:"
	}
	choice, err := a.prompter().Select(promptTitle, "Exposure", opts, 0)
	if err != nil {
		return "", nil, err
	}
	exp := byID[choice.Value]
	return exp.ID, &exp, nil
}

// resolveTemplate interactively picks a template when omitted in TTY mode.
func (a *App) resolveTemplate(ctx context.Context, client *kibotos.Client, zoneFilter string) (*kibotos.Template, error) {
	page, err := collectAllTemplates(ctx, client, &kibotos.TemplateListParams{Zone: zoneFilter})
	if err != nil {
		return nil, err
	}
	if len(page.Data) == 0 {
		return nil, &CLIError{
			ExitCode: ExitError,
			Code:     "no_templates",
			Message:  "no templates available",
		}
	}
	opts := make([]prompt.Option, 0, len(page.Data))
	byID := make(map[string]kibotos.Template, len(page.Data))
	for _, t := range page.Data {
		byID[t.ID] = t
		descParts := []string{}
		if t.DisplayName != "" && t.DisplayName != t.ID {
			descParts = append(descParts, t.DisplayName)
		}
		if len(t.Zones) > 0 {
			descParts = append(descParts, "zones: "+strings.Join(t.Zones, ", "))
		}
		opts = append(opts, prompt.Option{
			Value:       t.ID,
			Label:       t.ID,
			Description: strings.Join(descParts, " · "),
		})
	}
	choice, err := a.prompter().Select("Select a template:", "Template", opts, 0)
	if err != nil {
		return nil, err
	}
	tpl := byID[choice.Value]
	return &tpl, nil
}

// resolveProfileName picks a saved profile interactively when name is omitted in TTY mode.
func (a *App) resolveProfileName(cfg *config.Config, ref, promptTitle string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		return ref, nil
	}
	if !a.isInteractive() {
		return "", usageErrorWithHint(
			"Pass a profile name. Run `kibo profile list --json` to see saved profiles.",
			"profile name is required",
		)
	}
	names := cfg.SortedProfileNames()
	if len(names) == 0 {
		return "", &CLIError{
			ExitCode: ExitError,
			Code:     "no_profiles",
			Message:  "no profiles configured",
			Hint:     "Run `kibo login` to sign in.",
		}
	}
	active := cfg.ActiveProfileName()
	opts := make([]prompt.Option, 0, len(names))
	defaultIdx := 0
	for i, name := range names {
		prof := cfg.Profiles[name]
		desc := formatOrg(prof.OrganizationName, prof.OrganizationID)
		if name == active {
			desc += " (active)"
			defaultIdx = i
		}
		opts = append(opts, prompt.Option{
			Value:       name,
			Label:       name,
			Description: desc,
		})
	}
	if promptTitle == "" {
		promptTitle = "Select a profile:"
	}
	choice, err := a.prompter().Select(promptTitle, "Profile", opts, defaultIdx)
	if err != nil {
		return "", err
	}
	return choice.Value, nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func resourceLabel(id, name string) string {
	if name == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", name, id)
}

func missingWorkspaceRef() error {
	return usageErrorWithHint(
		"Pass a workspace ID or name. Run `kibo workspace list --json` to see available workspaces.",
		"workspace ID or name is required",
	)
}

func missingVolumeRef() error {
	return usageErrorWithHint(
		"Pass a volume ID or name. Run `kibo volume list --json` to see available volumes.",
		"volume ID or name is required",
	)
}

func missingExposureRef() error {
	return usageErrorWithHint(
		"Pass an exposure ID. Run `kibo exposure list <workspace> --json` to see exposures.",
		"exposure ID is required",
	)
}

func missingPackRef() error {
	return usageErrorWithHint(
		"Pass a Pack ID. Run `kibo pack list --json` to see available Packs.",
		"Pack ID is required",
	)
}

func missingNetworkRef(kind string) error {
	hint := "Pass a resolver ID or name. Run `kibo resolver list --json` to see available resolvers."
	if kind == "egress" {
		hint = "Pass an egress ID or name. Run `kibo egress list --json` to see available egresses."
	}
	return usageErrorWithHint(hint, "%s ID or name is required", kind)
}

func networkArticleSuffix(kind string) string {
	if kind == "egress" {
		return "n"
	}
	return ""
}

func collectAllPacks(ctx context.Context, client *kibotos.Client, base *kibotos.PackListParams) (*kibotos.MultiZonePage[kibotos.Pack], error) {
	var params kibotos.PackListParams
	if base != nil {
		params = *base
	}
	return collectCatalogPages("Packs", params.PageToken, func(token string) (*kibotos.MultiZonePage[kibotos.Pack], error) {
		params.PageToken = token
		return client.Packs.List(ctx, &params)
	})
}

func collectAllEgresses(ctx context.Context, client *kibotos.Client, base *kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Egress], error) {
	return collectNetworkPages("egresses", base, func(params *kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Egress], error) {
		return client.Egresses.List(ctx, params)
	})
}

func collectAllResolvers(ctx context.Context, client *kibotos.Client, base *kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Resolver], error) {
	return collectNetworkPages("resolvers", base, func(params *kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[kibotos.Resolver], error) {
		return client.Resolvers.List(ctx, params)
	})
}

func collectNetworkPages[T any](noun string, base *kibotos.NetworkResourceListParams, list func(*kibotos.NetworkResourceListParams) (*kibotos.MultiZonePage[T], error)) (*kibotos.MultiZonePage[T], error) {
	var params kibotos.NetworkResourceListParams
	if base != nil {
		params = *base
	}
	return collectCatalogPages(noun, params.PageToken, func(token string) (*kibotos.MultiZonePage[T], error) {
		params.PageToken = token
		return list(&params)
	})
}

func collectCatalogPages[T any](noun, token string, list func(string) (*kibotos.MultiZonePage[T], error)) (*kibotos.MultiZonePage[T], error) {
	all := &kibotos.MultiZonePage[T]{Data: []T{}, UnavailableZones: []string{}}
	seenTokens, seenZones := map[string]bool{}, map[string]bool{}
	for {
		if seenTokens[token] {
			return nil, fmt.Errorf("repeated page token while listing %s", noun)
		}
		seenTokens[token] = true
		page, err := list(token)
		if err != nil {
			return nil, err
		}
		all.Data = append(all.Data, page.Data...)
		for _, zone := range page.UnavailableZones {
			if !seenZones[zone] {
				all.UnavailableZones = append(all.UnavailableZones, zone)
				seenZones[zone] = true
			}
		}
		token = page.Token()
		if token == "" {
			return all, nil
		}
	}
}

func (a *App) resolvePack(ctx context.Context, client *kibotos.Client, ref, zoneFilter, promptTitle string) (string, *kibotos.Pack, error) {
	if ref != "" {
		return ref, nil, nil
	}
	if !a.isInteractive() {
		return "", nil, missingPackRef()
	}
	page, err := collectAllPacks(ctx, client, &kibotos.PackListParams{Zone: zoneFilter})
	if err != nil {
		return "", nil, err
	}
	if len(page.Data) == 0 {
		return "", nil, &CLIError{ExitCode: ExitError, Code: "no_matching_pack", Message: "no Packs found"}
	}
	if promptTitle == "" {
		promptTitle = "Select a Pack:"
	}
	return pickResource(a, promptTitle, "Pack", page.Data, func(p kibotos.Pack) prompt.Option {
		var desc []string
		if p.DisplayName != "" {
			desc = append(desc, p.DisplayName)
		}
		if p.Version != "" {
			desc = append(desc, p.Version)
		}
		if len(p.Zones) > 0 {
			desc = append(desc, "zones: "+strings.Join(p.Zones, ", "))
		}
		return prompt.Option{Value: p.ID, Label: p.ID, Description: strings.Join(desc, " · ")}
	})
}

func (a *App) resolveEgress(ctx context.Context, client *kibotos.Client, ref, promptTitle string) (string, *kibotos.Egress, error) {
	return resolveNetworkResource(a, ref, "egress", "egresses", "egr_", promptTitle, func() (*kibotos.MultiZonePage[kibotos.Egress], error) {
		return collectAllEgresses(ctx, client, nil)
	}, func(e kibotos.Egress) (string, string, string) {
		return e.ID, e.Name, fmt.Sprintf("%s · %s", e.Socks5.Address, e.Zone)
	})
}

func (a *App) resolveResolver(ctx context.Context, client *kibotos.Client, ref, promptTitle string) (string, *kibotos.Resolver, error) {
	return resolveNetworkResource(a, ref, "resolver", "resolvers", "rsv_", promptTitle, func() (*kibotos.MultiZonePage[kibotos.Resolver], error) {
		return collectAllResolvers(ctx, client, nil)
	}, func(r kibotos.Resolver) (string, string, string) {
		return r.ID, r.Name, fmt.Sprintf("%s · %s · %s", r.Protocol, resolverUpstream(r), r.Zone)
	})
}

func resolveNetworkResource[T any](a *App, ref, kind, plural, prefix, title string,
	list func() (*kibotos.MultiZonePage[T], error), describe func(T) (id, name, description string),
) (string, *T, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, prefix) {
		return ref, nil, nil
	}
	if ref == "" && !a.isInteractive() {
		return "", nil, missingNetworkRef(kind)
	}
	page, err := list()
	if err != nil {
		return "", nil, err
	}
	items := page.Data
	if ref == "" {
		if len(items) == 0 {
			return "", nil, &CLIError{ExitCode: ExitError, Code: "no_matching_" + kind, Message: "no " + plural + " found"}
		}
	} else {
		var exact, folded []T
		for _, item := range items {
			id, name, _ := describe(item)
			switch {
			case id == ref:
				return id, &item, nil
			case name == ref:
				exact = append(exact, item)
			case strings.EqualFold(name, ref):
				folded = append(folded, item)
			}
		}
		items = exact
		if len(items) == 0 {
			items = folded
		}
		switch len(items) {
		case 0:
			return ref, nil, nil
		case 1:
			id, _, _ := describe(items[0])
			return id, &items[0], nil
		}
		if !a.isInteractive() {
			ids := make([]string, 0, len(items))
			for _, item := range items {
				id, _, _ := describe(item)
				ids = append(ids, id)
			}
			return "", nil, &CLIError{
				ExitCode: ExitUsage,
				Code:     "ambiguous_name",
				Message:  fmt.Sprintf("multiple %s match name %q: %s", plural, ref, strings.Join(ids, ", ")),
				Hint:     fmt.Sprintf("Pass the explicit %s ID (%s...) instead.", kind, prefix),
				Details:  map[string]any{"candidates": ids},
			}
		}
		title = fmt.Sprintf("Multiple %s named %q:", plural, ref)
	}
	if title == "" {
		title = fmt.Sprintf("Select a%s %s:", networkArticleSuffix(kind), kind)
	}
	label := strings.ToUpper(kind[:1]) + kind[1:]
	return pickResource(a, title, label, items, func(item T) prompt.Option {
		id, name, desc := describe(item)
		return prompt.Option{Value: id, Label: resourceLabel(id, name), Description: desc}
	})
}

func pickResource[T any](a *App, title, label string, items []T, option func(T) prompt.Option) (string, *T, error) {
	opts := make([]prompt.Option, 0, len(items))
	byID := make(map[string]T, len(items))
	for _, item := range items {
		opt := option(item)
		opts = append(opts, opt)
		byID[opt.Value] = item
	}
	chosen, err := a.prompter().Select(title, label, opts, 0)
	if err != nil {
		return "", nil, err
	}
	item := byID[chosen.Value]
	return chosen.Value, &item, nil
}
