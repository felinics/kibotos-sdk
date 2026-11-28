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

`kibo` displays a one-time verification code and opens the Kibotos Console in your browser to sign in on this device. This issues a **user-level CLI session token (`kbs_...`)** managed in **Account → Sessions** (no organization API key is minted), and lets you switch organizations anytime with `kibo org list` / `kibo org use` or `--org <id>`. Use `--no-browser` in remote or SSH sessions.

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

# Create a workspace interactively (walks you through template & optional settings)
kibo ws create

# Or create directly with human-friendly flags (waits until available by default!)
kibo ws create -t python-3.12 -n devbox --memory 4Gi --timeout 1h

# Expose port 8080 over a temporary HTTPS URL (supports workspace names as well as IDs)
kibo exp create devbox 8080 --ttl 1h

# Create a 50 GiB persistent volume and attach it to a stopped workspace
kibo vol create -s 50Gi -n datasets
kibo ws stop devbox
kibo ws volume attach devbox --volume datasets --mount-path /data
kibo ws start devbox

# Clean up (prompts for confirmation in interactive TTY; pass -y to skip)
kibo ws delete devbox
```

## Ergonomics: Interactive vs. Scripting / AI Agents

- **Interactive terminal (TTY)**:
  - Omitting required flags or resource IDs opens minimal inline arrow-key pickers (`↑`/`↓` + `Enter`, e.g., `kibo ws create`, `kibo ws stop`, `kibo exp create`) — no heavy full-screen TUI.
  - Async mutations (`workspace create/start/stop/delete/network`, `volume create/resize/delete`) **wait by default** with a live stderr spinner; pass `--no-wait` to return immediately.
  - Destructive commands (`delete`, `revoke`, `stop --force`) ask `[y/N]` confirmation in interactive terminals (skip with `-y` / `--yes`).
  - Supports human-friendly units (`--memory 4Gi`, `--disk 20Gi`, `--size 50Gi`, `--timeout 1h30m`, `--template python-3.12@r42`) and unique workspace/volume names in place of IDs.
  - Colored status output via ANSI (automatically disabled on non-TTY or when `NO_COLOR` / `--no-color` is set).
- **Scripting & AI Agents (Non-Interactive)**:
  - Automatically enters non-interactive mode when stdin/stderr is not a TTY, or when `--json`, `-q`/`--quiet`, `--no-input`, `CI=true`, or `KIBO_NO_INPUT=1` is set — **never blocks on stdin**.
  - Pass `--json` (or `-o json`, or set `KIBO_OUTPUT=json`) for strictly valid JSON on `stdout` and structured JSON errors (with actionable `hint` / `details.missing_flags`) on `stderr`.
  - Pass `-q` / `--quiet` to print only resource IDs to `stdout` (ideal for `WS=$(kibo ws create -t python-3.12 -q)`).
  - Exit codes:
    - `0`: Success
    - `1`: API, runtime, or cancelled error
    - `2`: Usage, missing flag, ambiguous name, or validation error
    - `3`: Unauthenticated (no credentials or HTTP `401`)

For AI coding agents driving `kibo`, see the paired [`use-kibo`](../skills/use-kibo/SKILL.md) skill.
