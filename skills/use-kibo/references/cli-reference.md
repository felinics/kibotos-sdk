# `kibo` CLI Complete Reference

## Global Flags

Available on every `kibo` command:

| Flag | Default | Description |
| --- | --- | --- |
| `--json` | `false` | Write strictly valid JSON to `stdout` (shorthand for `-o json`) and disable interactive prompts |
| `-o, --output <format>` | `table` | Output format: `table` or `json` (env: `KIBO_OUTPUT` / `KIBOTOS_OUTPUT`) |
| `-q, --quiet` | `false` | Print only resource IDs to `stdout` and suppress progress messages |
| `--no-input` | `false` | Disable interactive prompts and fail fast on missing inputs (env: `KIBO_NO_INPUT` / `KIBOTOS_NO_INPUT` / `CI`) |
| `-y, --yes` | `false` | Skip interactive `[y/N]` confirmation prompts for destructive actions |
| `--no-color` | `false` | Disable colored output (also respects `NO_COLOR`) |
| `--profile <name>` | active profile | Named profile from `config.json` |
| `--org <id>` | active profile org | Override target organization ID (`X-Kibotos-Organization`, env: `KIBO_ORG` / `KIBOTOS_ORG`) |
| `--api-key <key>` | — | Override Kibotos API key (`kb_...`) or session token (`kbs_...`) for this invocation |
| `--api-url <url>` | `https://api.kibo.to/v1` | Override Kibotos v1 API base URL |

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

In `--json` mode, errors are emitted to `stderr` as:

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
  - `--volume <vol>`: Existing volume ID or name to attach at creation
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
  - Waits by default until `network.applied` is `true`.
- `kibo workspace volume attach [workspace] --volume <vol> [--mount-path </data>] [--delete-with-workspace]`
- `kibo workspace volume detach [workspace]`
- `kibo workspace wait [id-or-name] -s|--status <s1,s2> [--timeout <dur>]`
  - `--status` accepts comma-separated values or repeated flags (default `--timeout`: `30s`).

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
