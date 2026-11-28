# `kibo` CLI Complete Reference

## Global Flags

Available on every `kibo` command:

| Flag | Default | Description |
| --- | --- | --- |
| `--json` | `false` | Write strictly valid JSON to `stdout` (shorthand for `-o json`) and disable interactive prompts |
| `-o, --output <format>` | `table` | Output format: `table` or `json` (env: `KIBO_OUTPUT` / `KIBOTOS_OUTPUT`) |
| `-q, --quiet` | `false` | Print bare output such as resource IDs or a Desktop URL and suppress progress messages |
| `--no-input` | `false` | Disable interactive prompts and fail fast on missing inputs (env: `KIBO_NO_INPUT` / `KIBOTOS_NO_INPUT` / `CI`) |
| `-y, --yes` | `false` | Skip interactive `[y/N]` confirmation prompts for destructive actions |
| `--no-color` | `false` | Disable colored output (also respects `NO_COLOR`) |
| `--profile <name>` | active profile | Named profile from `config.json` |
| `--org <id>` | active profile org | Override target organization ID (`X-Kibotos-Organization`, env: `KIBO_ORG` / `KIBOTOS_ORG`) |
| `--api-key <key>` | — | Override Kibotos API key (`kb_...`) or session token (`kbs_...`) for this invocation |
| `--api-url <url>` | `https://api.kibo.to/v1` | Override Kibotos v1 API base URL |

Use `--no-input --json` with explicit IDs and required values for automation. Use
`-q` for bare values such as IDs and Desktop URLs. Essential missing inputs open guided prompts in a terminal.
JSON output, quiet mode, `--no-input`, `TERM=dumb`, truthy `CI` or no-input
environment variables, and non-terminal stdin or stderr disable prompts.
Missing required inputs fail before authentication with exit 2 and a hint.

## Configuration & Precedence

Config file location (first match wins):

1. `$KIBO_CONFIG_DIR/config.json`
2. `$KIBOTOS_CONFIG_DIR/config.json`
3. `$XDG_CONFIG_HOME/kibo/config.json`
4. `~/.config/kibo/config.json` (mode `0600`)

Credential & URL precedence (highest to lowest):

- **Profile**: `--profile` > `KIBO_PROFILE` > `KIBOTOS_PROFILE` > `current_profile` > `default`
- **API Key**: `--api-key` > `KIBOTOS_API_KEY` > `KIBO_API_KEY` > active profile `api_key`
- **API URL**: `--api-url` > `KIBOTOS_API_URL` > `KIBO_API_URL` > active profile `api_url` > `https://api.kibo.to/v1`
- **Console URL**: `--console-url` > `KIBOTOS_CONSOLE_URL` > `KIBO_CONSOLE_URL` > active profile `console_url` > `https://console.kibo.to`

## Exit Codes

| Exit Code | Meaning | Agent Action |
| --- | --- | --- |
| `0` | Success | Parse `stdout` JSON |
| `1` | API or runtime error | Parse `stderr` JSON (`error.code`, `error.retryable`, `error.request_id`) |
| `2` | Usage or validation error | Fix flags/arguments before re-running |
| `3` | Unauthenticated / HTTP `401` | Guide user to run `kibo login` or configure `KIBOTOS_API_KEY` |

`exec` also returns the remote exit code, 124 on timeout, or 128 plus the signal number. With `--json`, CLI and API errors use the structured envelope on stderr.

```json
{
  "error": {
    "code": "invalid_state",
    "message": "Human-readable explanation",
    "exit_code": 1,
    "status": 409,
    "request_id": "req_...",
    "retryable": false,
    "hint": "Optional remediation hint"
  }
}
```

## Authentication & Profile Commands

### `kibo login`

Authenticate interactively via browser Device Authorization Flow (default) or non-interactively with `--api-key`.

| Flag | Description |
| --- | --- |
| `--console-url <url>` | Console base URL for device authorization (default `https://console.kibo.to`) |
| `--api-url <url>` | Override API base URL stored in the profile |
| `--profile <name>` | Profile name to save credentials into |
| `--device-name <label>` | Device label displayed during browser approval (default: hostname) |
| `--no-browser` | Print verification URL without attempting to open a browser |
| `--api-key <kb_...>` | Non-interactive login with an API key (verifies via `GET /v1/zones` unless `--skip-verify`) |
| `--skip-verify` | Skip live API check when using `--api-key` |

### `kibo logout`

Remove saved credentials for the active profile (or `--profile <name>`).

### `kibo whoami`

Show active profile, auth source, organization, email, API URL, masked key, and live accessible zones.

| Flag | Description |
| --- | --- |
| `--offline` | Skip live `GET /v1/zones` connectivity check |

### `kibo org` (aliases: `orgs`, `organization`, `organizations`)

- `kibo org list` — List organizations accessible to your signed-in CLI user session (`*` marks active org)
- `kibo org use [id-or-name]` — Switch the active organization for the current profile (prompts interactively if omitted in TTY)

### `kibo profile` (alias: `profiles`)

- `kibo profile list` — List saved profiles (`*` marks active profile)
- `kibo profile use [name]` — Set active profile
- `kibo profile delete [name]` — Delete a saved profile

### `kibo version`

Show CLI version, Go SDK version, Go runtime, and platform.

## Zone Commands

### `kibo zone` (alias: `zones`)

- `kibo zone list [--limit <n>] [--page-token <tok>]`
  - Returns `{ "data": Zone[], "next_page_token": string | null }`. Each `Zone` includes `id`, `display_name`, `region`, `country` (ISO 3166-1 alpha-2 uppercase string or `null`), `status`, and `default`.
  - Table output columns: `ID`, `NAME`, `REGION`, `COUNTRY` (`-` when null/empty), `STATUS`, `DEFAULT`.
  - Auto-paginates all pages when neither `--limit` nor `--page-token` is passed.

## Template Commands

### `kibo template` (aliases: `tpl`, `templates`)

- `kibo template list [--owner <platform|organization>] [--zone <zone>] [--limit <n>] [--page-token <tok>]`
  - Returns `{ "data": Template[], "unavailable_zones": string[], "next_page_token": string | null }`.
- `kibo template get <id>`
- `kibo template revisions <id> [--limit <n>] [--page-token <tok>]`
- `kibo template revision <id> <revision_id>`

## Workspace Commands

### `kibo workspace` (aliases: `ws`, `workspaces`)

All workspace commands accepting `[id-or-name]` accept either a `wks_...` ID or a unique workspace name (or open an inline arrow-key selector in interactive TTY mode). Async mutations (`create`, `start`, `stop`, `delete`, `network`) **wait until completion by default**; pass `--no-wait` (or `--wait=false`) to return immediately.

- `kibo workspace list [-s|--status <s>] [-z|--zone <z>] [-l|--label k=v] [--limit <n>] [--page-token <tok>]`
  - `--label` is repeatable.
  - Status values: `creating`, `available`, `stopping`, `stopped`, `starting`, `deleting`, `failed`, `unknown`.
- `kibo workspace get [id-or-name]`
- `kibo workspace create [-t|--template <id>] [flags]`
  - `-t, --template <id>` *(prompted in TTY; required non-interactively)*: Template ID (supports `template@revision`)
  - `--revision <rev>`: Pin specific template revision
  - `-n, --name <name>`: Human-readable workspace name
  - `-z, --zone <zone>`: Target zone ID (defaults to org default zone)
  - `--vcpus <n>`: vCPU count override
  - `--memory <size>`: Memory allocation (`4Gi`, `4096`; bare integer is MiB)
  - `--disk <size>`: Root disk size (`20Gi`, `20480`; bare integer is MiB)
  - `--network <mode>`: `public_internet` or `deny_all`
  - `--volume <vol_id:/path>` binds an existing volume at creation. Repeat it for each mount or pass one legacy volume ID or name with `--mount-path`.
  - `--pack <pack_id>` binds a Pack at creation. Pass up to four unique IDs.
  - `--desktop` attaches a Pack that provides `desktop.v1`, counts it toward the four-Pack limit, and shows the Desktop URL in human output.
  - `--open` launches the desktop when the workspace is available and requires `--desktop`.
  - `--console-url <url>` overrides the HTTP or HTTPS base for desktop links. JSON and quiet mode produce the workspace object and workspace ID, respectively.
  - `--mount-path <path>`: Mount path for `--volume` (e.g. `/data`)
  - `--delete-volume`: Delete the attached volume when the workspace is deleted (requires `--volume`)
  - `--timeout <dur>`: Auto-timeout duration (`1h`, `30m`, `3600`; bare integer is seconds)
  - `--timeout-action <action>`: `stop` or `delete` (requires `--timeout`)
  - `-l, --label k=v`: Workspace label (repeatable)
  - `--wait` *(default `true`)* / `--no-wait`: Block until status reaches `available` (default) or return immediately
  - `--wait-timeout <dur>`: Max wait duration (default `300s` / `5m`)
- `kibo workspace update [id-or-name] [-n|--name <n>] [--clear-name] [-l|--label k=v] [--unset-label k]`
  - `--name` and `--clear-name` are mutually exclusive. `--label` and `--unset-label` are repeatable.
- `kibo workspace delete [id-or-name] [--no-wait] [--wait-timeout <dur>] [-y]`
  - Waits by default until deletion completes (`404`) and returns `"status": "deleted"`. Confirms in TTY unless `-y`.
- `kibo workspace start [id-or-name] [--timeout <dur>] [--timeout-action <stop|delete>] [--no-wait] [--wait-timeout <dur>]`
- `kibo workspace stop [id-or-name] [--force] [--no-wait] [--wait-timeout <dur>] [-y]`
- `kibo workspace timeout [id-or-name] --seconds <dur> [--action <stop|delete>]`
- `kibo workspace network [id-or-name] --mode <public_internet|deny_all> [--no-wait] [--wait-timeout <dur>]`
  - Add repeatable `--allow <IP_OR_CIDR>` and `--deny <IP_OR_CIDR>`, up to one `--egress <egress_id=IP_OR_CIDR[,IP_OR_CIDR...]>`, and up to one `--resolver <resolver_id[=egress_id]>`. The resolver uses a direct path when the egress ID is omitted. Use `--drain-seconds <0..3600>` to control egress connection draining.
  - Updates replace the complete policy. Running workspaces wait by default until `network.applied` is true. Stopped workspaces apply the policy on the next start.
- `kibo workspace volume attach [workspace] [volume] --mount /data [--delete-with-workspace]` adds one binding. IDs and names are accepted. In a terminal, omit `--mount` to be prompted for it. The legacy `attach [workspace] --volume <id-or-name> [--mount-path <path>]` form can use the default mount for the first volume. The guided volume picker shows only same-zone, available, unbound volumes.
- `kibo workspace volume detach [workspace] [volume]` removes one binding when a volume is given. With no volume in a terminal, a single binding is confirmed and multiple bindings open a picker, including **All volumes**. With no volume in automation, detach clears all bindings.
- `kibo workspace volumes set [workspace] [vol_id:/path...]` replaces all volume bindings. Omitting bindings detaches every volume, with confirmation in a terminal. If the workspace is omitted, the picker shows stopped workspaces.
- `kibo workspace resize [id-or-name] [--vcpus <n>] [--memory-mib <n>] [--disk-mib <n>]` changes at least one resource. Supply at least one flag in automation. Guided resize uses current values as defaults and sends changed fields. The workspace must be stopped or failed and the root disk can only grow.
- `kibo workspace packs set [workspace] [pack_id...]` replaces the ordered list for the next start. In a terminal, omit the workspace to choose one and enter up to four Pack IDs separated by commas or spaces. Leave the field blank or pass only a workspace to clear the list. Clearing requires confirmation in a terminal unless `--yes` is set. Automation clears immediately.
- `kibo workspace metrics [id-or-name] [--start <RFC3339>] [--end <RFC3339>]` reads host and guest time series. If both bounds are given, start must precede end. Only the workspace is prompted.
- `kibo workspace wait [id-or-name] -s|--status <s1,s2> [--timeout <dur>]`
  - `--status` accepts comma-separated values or repeated flags (default `--timeout`: `30s`).

### `kibo desktop [workspace]` (also `kibo ws desktop`)

- Choose a workspace in a terminal or pass an ID or unique name. Attaches a Pack that provides `desktop.v1` if needed, preserving existing Pack bindings.
- `--start` starts a stopped workspace. `--restart` restarts an available workspace when the command just attached the Pack. In a terminal, the command offers the applicable action if its flag is omitted.
- `--wait` is enabled by default, with `--wait-timeout` set to `5m`. `--no-wait` skips waiting for the workspace to become available. A restart always waits for the stop before sending the start request.
- `--open` launches the browser when the workspace is available and any required restart has been performed. `--console-url <url>` overrides the HTTP or HTTPS base. The link includes `?org=<id>` when the CLI has an organization ID.
- `--json` returns `workspace_id`, `status`, `desktop_enabled`, `pack_id`, `restart_required`, and `url`. `-q` prints the URL alone. When `restart_required` is true, stop and start the workspace to apply the Pack.

## Execution, file transfers, and tunnels

- `kibo exec [workspace] [flags] [-- command [args...]]` runs explicit argv after `--`. Automation supplies both the workspace and argv. In a terminal, omit the workspace to choose an available one. Omit argv to enter one command interpreted by `/bin/sh -c`.
  - Flags are `--workdir <path>`, repeatable `--env KEY=VALUE`, `--timeout <seconds>`, `--user <user>`, `--stdin <text|->`, and `--stream`. Stdin size is limited to 3 MiB when read with `--stdin -`, and the resulting request must fit the 4 MiB API limit.
  - The default synchronous timeout is 30 seconds and the synchronous maximum is 600 seconds. Use `--stream --timeout <seconds>` for long-running commands. `--stream --json` emits one event per line.
  - `kibo exec [workspace] -it` opens a Bash or sh login shell on a Unix PTY. Add `-- <command> [args...]` for an explicit PTY command. PTY requires terminal stdin and is incompatible with `--stream`, `--stdin`, and JSON.
- `kibo files ls [workspace] [path] [--depth <n>]` lists entries under the path. The default `.` resolves to the selected user's home directory.
- `kibo files stat [workspace] [path]` shows attributes. `kibo files get [workspace] [path] [local-file|-] [--range bytes=0-1023]` writes bytes to stdout by default. With `--json`, supply a local file to receive metadata.
- `kibo files put [workspace] [local-file|-] [path] [--mode 0644] [--create-only]` uploads from a local file or stdin. The guided destination defaults to `./<local-filename>` for local files.
- `kibo files mkdir [workspace] [path] [--mode 0755]`, `kibo files mv [workspace] [source] [destination]`, and `kibo files rm [workspace] [path]` manage workspace files. Terminal deletion asks for confirmation.
- All file commands accept `--user <user>`. They prompt only for the available workspace and missing required paths. Supply those explicitly in automation.
- `kibo tunnel [workspace] [port]` forwards a local TCP listener to guest loopback. Supply a positional port, `--remote <port>`, or `--unix-path <absolute-path>` in automation. A terminal asks for the guest port if the target is omitted, with `8080` as the default. Optional flags are `--family ipv4|ipv6` for TCP and `--listen <host:port>` (default `127.0.0.1:18080`). JSON startup output includes `workspace_id` and `listen`.

## Packs, Egresses, and Resolvers

- `kibo pack list [--zone <id>] [--limit <n>] [--page-token <token>]` queries the catalog. `kibo pack get [pack_id] [--revision <id>]` picks a Pack in a terminal when the ID is omitted. Always supply a Pack ID in automation.
- `kibo egress list` and `kibo resolver list` accept `--zone`, repeatable `--label KEY=VALUE`, `--limit`, and `--page-token`. Lists auto-paginate unless a limit or cursor is supplied.
- Both resources support `get [id-or-name]`, `update [id-or-name]`, and `delete [id-or-name]`. Terminal commands pick a resource when omitted. Automation requires a canonical ID or a unique name. Names match exactly first, then case-insensitively. Ambiguity yields exit 2 and candidate IDs in automation. A terminal delete confirms the action. Deletion fails while a workspace policy references the resource.
- `kibo egress create --address <host:port>` creates a SOCKS5 proxy configuration. Omit the address in a terminal to enter it and optionally give the egress a name. Other flags include `--name`, `--zone`, repeatable `--label`, `--username`, `--password` or `--password-env <variable>`, `--tls`, `--server-name`, `--ca-cert <PEM-file>`, and `--udp-enabled`.
- `kibo resolver create --protocol <udp|tcp|tls> --address <upstream>` configures an address-based DNS upstream. For DNS-over-HTTPS, use `--protocol https --url <endpoint>`. In a terminal, `--url` implies HTTPS. When a terminal prompts for a missing protocol or upstream, it also offers an optional name if `--name` was omitted. Creation flags also include `--zone` and repeatable `--label`.
- Egress and resolver updates accept `--name`, `--clear-name`, `--label`, and `--unset-label`. A flagless update in a terminal prompts for a new name. Use a separate update invocation with the full endpoint configuration to replace it. Replacing an authenticated egress requires resupplying credentials. Egress replacements also accept `--drain-seconds <0..3600>`.

## Volume Commands

### `kibo volume` (aliases: `vol`, `volumes`)

All volume commands accepting `[id-or-name]` accept either a `vol_...` ID or a unique volume name (or prompt in TTY). Async mutations (`create`, `resize`, `delete`) **wait by default**; pass `--no-wait` to return immediately.

- `kibo volume list [-z|--zone <z>] [--attached <true|false>] [-l|--label k=v] [--limit <n>] [--page-token <tok>]`
  - Status values: `creating`, `available`, `attached`, `resizing`, `deleting`, `failed`, `lost`, `unknown` (native Kibotos 1:1 semantics: `workspace_id` expresses binding; `available` means ready and not mounted to a running workspace; `attached` means mounted to a running workspace; `lost` means storage pool lost).
- `kibo volume get [id-or-name]`
- `kibo volume create -s|--size <size> [-n|--name <n>] [-z|--zone <z>] [-l|--label k=v] [--no-wait] [--wait-timeout <dur>]`
  - `--size` accepts bare GiB (`50`) or units (`50Gi`, `1Ti`). Waits for `available` by default.
- `kibo volume update [id-or-name] [-n|--name <n>] [--clear-name] [-l|--label k=v] [--unset-label k]`
- `kibo volume resize [id-or-name] -s|--size <size> [--no-wait] [--wait-timeout <dur>]`
  - Grows an `available` volume to a larger size. Waits for `available` by default.
- `kibo volume delete [id-or-name] [--no-wait] [--wait-timeout <dur>] [-y]`
- `kibo volume wait [id-or-name] -s|--status <s1,s2> [--timeout <dur>]` (default `--timeout`: `30s`)

## Exposure Commands

### `kibo exposure` (aliases: `exp`, `exposures`)

- `kibo exposure list [workspace] [--include-revoked] [--limit <n>] [--page-token <tok>]`
  - Status values: `active`, `revoked`, `expired`.
- `kibo exposure get [workspace] [exposure_id]`
- `kibo exposure create [workspace] [port] [-p|--port <1-65535>] [--ttl <dur>]`
  - Port can be passed via `-p`/`--port` or as a second positional argument. `--ttl` accepts durations (`1h`, `30m`) or seconds (API default: `3600`).
- `kibo exposure revoke [workspace] [exposure_id] [-y]`

## Common API Error Codes

| HTTP | Code | Retryable | Meaning |
| --- | --- | --- | --- |
| `400` | `invalid_request`, `template_unavailable_in_zone`, `zone_mismatch` | No | Fix parameters (e.g., match volume and workspace zone) |
| `401` | `unauthenticated` | No | Exit `3`: re-authenticate via `kibo login` or `KIBOTOS_API_KEY` |
| `403` | `zone_not_permitted`, `permission_denied`, `organization_suspended`, `quota_exceeded` | No | Insufficient permissions or org quota reached |
| `404` | `workspace_not_found`, `volume_not_found`, `exposure_not_found`, `template_not_found`, `template_revision_not_found` | No | Resource ID does not exist or was deleted |
| `408` | `wait_timeout` | Yes | Wait window elapsed before target state; inspect `details` and re-wait |
| `409` | `operation_in_progress` | Yes | Another async transition is active; wait briefly and retry |
| `409` | `invalid_state`, `unexpected_status`, `volume_attached`, `template_not_ready`, `zone_draining`, `idempotency_key_reused` | No | Precondition failed (e.g., stop workspace before attaching/detaching volume) |
| `429` | `rate_limited` | Yes | Back off and retry |
| `500` | `internal_error` | Yes | Transient server error |
| `503` | `capacity_unavailable`, `zone_unavailable`, `authentication_unavailable`, `outcome_unknown` | Yes | Transient zone/capacity issue; retry or try another zone |
