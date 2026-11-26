# `kibo` — Official Kibotos CLI

`kibo` is the official command-line interface for **Kibotos** (Kibo). Provision and manage cloud microVM workspaces, persistent volumes, and temporary HTTPS port exposures from your terminal, CI pipelines, or AI agents.

## Installation

Build from source (requires Go 1.24+):

```bash
cd cli
go build -o /usr/local/bin/kibo ./cmd/kibo
```

Or run directly during development:

```bash
go run ./cmd/kibo --help
```

## Authentication

### Interactive Browser Login (Device Authorization Flow)

```bash
kibo login
```

`kibo` displays a one-time verification code and opens the Kibotos Console in your browser to approve the device and issue a scoped API key. Use `--no-browser` in remote or SSH sessions.

### Non-Interactive / CI Login

```bash
kibo login --api-key kb_...
```

Or pass credentials per-command via environment variables:

```bash
export KIBOTOS_API_KEY="kb_..."
kibo whoami
```

### Configuration & Precedence

Profiles are stored in `~/.config/kibo/config.json` (mode `0600`). Override the directory with `KIBO_CONFIG_DIR`, `KIBOTOS_CONFIG_DIR`, or `XDG_CONFIG_HOME`.

Resolution precedence (highest to lowest):

1. Flags: `--api-key`, `--api-url`, `--profile`
2. Environment variables:
   - API key: `KIBOTOS_API_KEY` > `KIBO_API_KEY`
   - API URL: `KIBOTOS_API_URL` > `KIBO_API_URL`
   - Console URL: `KIBOTOS_CONSOLE_URL` > `KIBO_CONSOLE_URL`
   - Profile: `KIBO_PROFILE` > `KIBOTOS_PROFILE`
3. Active profile in `config.json` (`current_profile`, defaulting to `default`)
4. Defaults: API `https://api.kibo.to/v1`, Console `https://console.kibo.to`

## Quick Start

```bash
# Check active identity and accessible zones
kibo whoami

# Discover zones and templates
kibo zone list
kibo template list

# Create a workspace and wait until it is available
kibo workspace create --template python-3.12 --name devbox --timeout 3600 --wait

# Expose port 8080 over a temporary HTTPS URL
kibo exposure create wks_... --port 8080 --ttl 3600

# Create a 50 GiB persistent volume and attach it to a stopped workspace
kibo volume create --size 50 --name datasets --wait
kibo workspace stop wks_... --wait
kibo workspace volume attach wks_... --volume vol_... --mount-path /data
kibo workspace start wks_... --wait

# Clean up
kibo workspace delete wks_... --wait
```

## Command Overview

| Command | Aliases | Description |
| --- | --- | --- |
| `kibo login` | — | Sign in via browser Device Flow or `--api-key` |
| `kibo logout` | — | Remove saved credentials for a profile |
| `kibo whoami` | — | Show active profile, auth source, and live zone check |
| `kibo profile` | `profiles` | `list`, `use <name>`, `delete <name>` |
| `kibo zone` | `zones` | `list` |
| `kibo template` | `tpl`, `templates` | `list`, `get <id>`, `revisions <id>`, `revision <id> <revision_id>` |
| `kibo workspace` | `ws`, `workspaces` | `list`, `get`, `create`, `update`, `delete`, `start`, `stop`, `timeout`, `network`, `volume attach\|detach`, `wait` |
| `kibo volume` | `vol`, `volumes` | `list`, `get`, `create`, `update`, `resize`, `delete`, `wait` |
| `kibo exposure` | `exp`, `exposures` | `list`, `get`, `create`, `revoke` |
| `kibo version` | — | Show CLI and SDK version info |

## Scripting & AI Agents

- Pass `--json` (or `-o json`) to write strictly valid JSON to `stdout`. Prompts, progress updates, warnings, and structured error JSON are always written to `stderr`.
- Exit codes:
  - `0`: Success
  - `1`: API or runtime error
  - `2`: Usage or flag validation error
  - `3`: Unauthenticated (no credentials or HTTP `401`)

For AI coding agents driving `kibo`, see the paired [`use-kibo`](../skills/use-kibo/SKILL.md) skill.
