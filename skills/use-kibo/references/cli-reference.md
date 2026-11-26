# `kibo` CLI Complete Reference

## Global Flags

Available on every `kibo` command:

| Flag | Default | Description |
| --- | --- | --- |
| `--json` | `false` | Write strictly valid JSON to `stdout` (shorthand for `-o json`) |
| `-o, --output <format>` | `table` | Output format: `table` or `json` |
| `--profile <name>` | active profile | Named profile from `config.json` |
| `--api-key <key>` | — | Override Kibotos API key (`kb_...`) for this invocation |
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

### `kibo profile` (alias: `profiles`)

- `kibo profile list` — List saved profiles (`*` marks active profile)
- `kibo profile use <name>` — Set active profile
- `kibo profile delete <name>` — Delete a saved profile

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

- `kibo workspace list [--status <s>] [--zone <z>] [--label k=v] [--limit <n>] [--page-token <tok>]`
  - `--label` is repeatable.
  - Status values: `creating`, `available`, `stopping`, `stopped`, `starting`, `deleting`, `failed`, `unknown`.
- `kibo workspace get <id>`
- `kibo workspace create --template <id> [flags]`
  - `--template <id>` *(required)*: Template ID
  - `--revision <rev>`: Pin specific template revision
  - `--name <name>`: Human-readable workspace name
  - `--zone <zone>`: Target zone ID (defaults to org default zone)
  - `--vcpus <n>`: vCPU count override
  - `--memory <mib>`: Memory allocation in MiB
  - `--disk <mib>`: Root disk size in MiB
  - `--network <mode>`: `public_internet` or `deny_all`
  - `--volume <vol_id>`: Existing volume ID to attach at creation
  - `--mount-path <path>`: Mount path for `--volume` (e.g. `/data`)
  - `--delete-volume`: Delete the attached volume when the workspace is deleted (requires `--volume`)
  - `--timeout <sec>`: Auto-timeout duration in seconds
  - `--timeout-action <action>`: `stop` or `delete` (requires `--timeout`)
  - `--label k=v`: Workspace label (repeatable)
  - `--wait`: Block until status reaches `available`
  - `--wait-timeout <sec>`: Max wait seconds (default `300`)
- `kibo workspace update <id> [--name <n>] [--clear-name] [--label k=v] [--unset-label k]`
  - `--name` and `--clear-name` are mutually exclusive. `--label` and `--unset-label` are repeatable.
- `kibo workspace delete <id> [--wait] [--wait-timeout <sec>]`
  - With `--wait`, polls until deletion completes (`404`) and returns `"status": "deleted"`.
- `kibo workspace start <id> [--timeout <sec>] [--timeout-action <stop|delete>] [--wait] [--wait-timeout <sec>]`
- `kibo workspace stop <id> [--force] [--wait] [--wait-timeout <sec>]`
- `kibo workspace timeout <id> --seconds <sec> [--action <stop|delete>]`
- `kibo workspace network <id> --mode <public_internet|deny_all> [--wait] [--wait-timeout <sec>]`
  - With `--wait`, polls until `network.applied` is `true`.
- `kibo workspace volume attach <workspace_id> --volume <vol_id> [--mount-path </data>] [--delete-with-workspace]`
- `kibo workspace volume detach <workspace_id>`
- `kibo workspace wait <id> --status <s1,s2> [--timeout <sec>]`
  - `--status` accepts comma-separated values or repeated flags (default `--timeout`: `30`).

## Volume Commands

### `kibo volume` (aliases: `vol`, `volumes`)

- `kibo volume list [--zone <z>] [--attached <true|false>] [--label k=v] [--limit <n>] [--page-token <tok>]`
  - Status values: `creating`, `available`, `attached`, `resizing`, `deleting`, `failed`, `lost`, `unknown` (native Kibotos 1:1 semantics: `workspace_id` expresses binding; `available` means ready and not mounted to a running workspace; `attached` means mounted to a running workspace; `lost` means storage pool lost).
- `kibo volume get <id>`
- `kibo volume create --size <gib> [--name <n>] [--zone <z>] [--label k=v] [--wait] [--wait-timeout <sec>]`
  - With `--wait`, waits for status `available`.
- `kibo volume update <id> [--name <n>] [--clear-name] [--label k=v] [--unset-label k]`
- `kibo volume resize <id> --size <gib> [--wait] [--wait-timeout <sec>]`
  - Grows an `available` volume to a larger size in GiB. With `--wait`, waits for `available`.
- `kibo volume delete <id> [--wait] [--wait-timeout <sec>]`
- `kibo volume wait <id> --status <s1,s2> [--timeout <sec>]` (default `--timeout`: `30`)

## Exposure Commands

### `kibo exposure` (aliases: `exp`, `exposures`)

- `kibo exposure list <workspace_id> [--include-revoked] [--limit <n>] [--page-token <tok>]`
  - Status values: `active`, `revoked`, `expired`.
- `kibo exposure get <workspace_id> <exposure_id>`
- `kibo exposure create <workspace_id> --port <1-65535> [--ttl <sec>]`
  - `--ttl` defaults to `3600` on the API when omitted.
- `kibo exposure revoke <workspace_id> <exposure_id>`

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
