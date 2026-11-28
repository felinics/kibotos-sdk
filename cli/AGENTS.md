# kibo CLI design standard

This standard applies to the Kibotos CLI, its tests, and its user-facing documentation.

## Design for humans and agents

- Make the common terminal workflow short. A user should be able to type a command
  and supply its essential missing inputs without memorizing IDs or defaults.
- Provide a complete explicit invocation for every guided command. Scripts, CI,
  and AI agents supply arguments and flags with `--no-input` and `--json` or `-q`.
- Keep command syntax, output, and exit status stable for existing callers. Use
  arguments for primary targets and flags for tuning.
- Use **Kibotos** as the product name in help, prompts, errors, examples, docs,
  and skills. Use `kibo` for the binary and command names.

## Keep interaction minimal

- Start with one or two questions at most. Ask for a missing primary target and
  the core value needed to perform the action. Skip questions for supplied inputs.
- Eligible questions include a missing workspace, volume, template, or other
  target, and an essential port, address, protocol, command, path, or mount.
  A short optional name is appropriate in an existing guided creation flow.
- Some operations need multiple essential values. File move and upload can ask for
  two paths. Resolver creation may ask for protocol, upstream, and optional name.
  Guided resize asks for three current resource values after choosing the workspace.
  These flows stay inline and use defaults where the command has them.
- Keep optional tuning on flags. Examples include `--zone`, `--label`, `--workdir`,
  `--env`, `--timeout`, `--user`, `--tls`, `--ca-cert`, `--udp-enabled`,
  file `--mode`, `--range`, `--family`, `--listen`, `--revision`, `--start`,
  `--end`, and resource overrides during workspace creation.
- Use only `internal/prompt` for questions. `Select` handles arrow keys,
  `Input` handles a short validated line, and `Confirm` handles an explicit
  yes or no. Prompts go to stderr. Terminal selectors collapse to
  `✓ Label: value` and restore terminal state before returning.
- Reuse the same validation for prompted and explicit input where applicable.
  Give the question a clear label, add a safe default where useful, and allow
  cancellation without applying a mutation.
- Filter resource picker options by the operation's eligible states, zones, or
  bindings. For example, exec uses available workspaces and attach uses
  same-zone, available, unbound volumes. Give an empty picker a clear message.
- Use `confirmDestructive` for destructive actions such as delete, revoke,
  file removal, force-stop, or clearing all bindings. Default to no.
  `--yes` skips confirmation. Non-interactive commands skip it automatically.
- Use the prompt reader for subsequent stdin data if a prompt has buffered
  bytes. Finish picker interaction before starting a remote PTY.

## Preserve the automation contract

`a.isInteractive()` is false under any of these conditions.

- `--no-input` or `-q` is set.
- The effective format is JSON, including `--json`, `-o json`, or the
  `KIBO_OUTPUT` / `KIBOTOS_OUTPUT` environment setting.
- `CI`, `KIBO_NO_INPUT`, or `KIBOTOS_NO_INPUT` is truthy.
- `TERM=dumb` or either stdin or stderr is a non-TTY.

`InteractiveOverride` controls the TTY check in tests. Production commands use
`a.isInteractive()` for all guided branches. A pipe on stdout alone leaves the
stdin and stderr checks in effect. Ask for explicit machine output in scripts.

### Validation and resource resolution

1. Parse the argument shape and validate explicitly passed flags before
   `a.requireClient()`. Check cross-flag conflicts at the same point.
2. In non-interactive mode, check required arguments and flags before
   `a.requireClient()`. Return `ExitUsage` (2) with an actionable `Hint`.
   Use `missingFlagError` for flags with a name in `details.missing_flags`.
3. Resolve resource references after local validation. Reuse the shared
   `resolve*` and pagination helpers. Canonical `wks_`, `vol_`, `egr_`,
   and `rsv_` IDs bypass lookup. Pack references are IDs and go directly
   to the requested API endpoint.
4. Prefer an exact name match, then case-insensitive matches. Interactive
   ambiguity uses a picker. Non-interactive ambiguity returns `ambiguous_name`
   and candidate IDs with exit 2. A reference with zero matches goes to the API.
5. Fetch resource state when the operation needs it. The legacy volume attach
   form can use the API default mount for a first volume, so an omitted
   `--mount-path` can only be rejected after reading the current bindings.

### Output and status

- Keep data, JSON results, IDs, and file bytes on stdout. Send prompts,
  spinners, warnings, hints, and errors to stderr.
- Keep JSON field names and shape stable. `exec --stream --json` emits one
  event per line. `files get --json` requires a local destination and emits
  download metadata. Preserve each command's quiet output.
- Wait by default for async mutations that expose wait flags. Use `--no-wait`
  to return immediately and preserve the existing `--wait-timeout` behavior.
- Auto-paginate lists unless the caller supplies `--limit` or `--page-token`.
  Guard against repeated page tokens. Deduplicate unavailable-zone warnings
  where collecting pages.
- Accept readable units where supported, such as `4Gi` and `1h`. Preserve
  existing bare-unit and machine-oriented flags such as `--memory-mib`.

| Status | Contract |
| --- | --- |
| `0` | Success |
| `1` | API or runtime error, including cancelled prompts |
| `2` | Usage, validation, or ambiguous name |
| `3` | Missing, invalid, or expired authentication |
| `4`, `5` | No dedicated CLI meanings in the current contract |
| `130` | Remote `exec` process interrupted by SIGINT |
| Other `exec` statuses | Remote process status, `124` on timeout, or `128 + signal` for a signal |

Structured CLI and API errors go to stderr in JSON mode. Remote process status
travels with the exec result or exit event.

```json
{
  "error": {
    "code": "invalid_usage",
    "message": "workspace ID or name is required",
    "exit_code": 2,
    "hint": "Pass a workspace ID or name."
  }
}
```

API errors can also include `status`, `request_id`, `retryable`, and `details`.

## Checklist for changing a command

- Write the short terminal usage and the complete explicit usage. Reflect both
  in Cobra help and keep defaults consistent with the request sent to the API.
- Exercise each guided branch with `InteractiveOverride` and scripted stdin.
  Assert prompt labels, filtered candidates, and the final API request.
- Test missing required input with `--no-input` and `--json`. Assert exit 2,
  an actionable hint, empty stdout, and zero auth or HTTP requests.
- Test JSON and quiet output. Cover canonical ID fast paths, exact and folded
  name lookup, ambiguity, and pagination for commands that resolve resources.
- Where confirmation applies, test cancel, accept, and `--yes`. Assert that
  cancellation never sends a mutation.
- Update `README.md`, `docs/` and its page index, and `../skills/use-kibo/`
  when syntax, behavior, or output changes.
- Run `go test -race ./...`, `go vet ./...`, and `go build ./cmd/kibo`
  from `cli/`. Check the product name in the changed user-facing files.
