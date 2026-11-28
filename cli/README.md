# `kibo` — Official Kibotos CLI

`kibo` is the official command-line interface for **Kibotos**. Provision and manage cloud microVM workspaces, persistent volumes, and temporary HTTPS port exposures from your terminal, CI pipelines, or AI agents.

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
kibo ws volume attach devbox vol_... --mount /data
kibo ws start devbox

# Clean up (prompts for confirmation in interactive TTY; pass -y to skip)
kibo ws delete devbox
```

## Execute commands and transfer files

```bash
kibo exec wks_... --workdir /app --env MODE=test --timeout 60 -- python3 main.py
kibo exec wks_... --stdin 'hello' --stream -- cat
kibo exec wks_... -it -- /bin/sh

kibo files ls wks_... /app --depth 2
kibo files stat wks_... /app/main.py --json
kibo files get wks_... /app/main.py ./main.py
kibo files put wks_... ./main.py /app/main.py --mode 0644 --create-only
printf 'hello' | kibo files put wks_... - /app/input.txt
kibo files mkdir wks_... /app/archive
kibo files mv wks_... /app/input.txt /app/archive/input.txt
kibo files rm wks_... /app/archive/input.txt
```

`exec` forwards the remote exit status, uses 124 for a timeout, and uses 128 plus the signal number for a signaled process. Add `--json` for the synchronous result or combine it with `--stream` for one JSON event per line. Interactive mode uses a Unix terminal and forwards window size changes. Use `--stdin -` to read local input for noninteractive execution. File commands accept `--user`. Use `files get --range bytes=0-1023` for a partial download. Downloads write to stdout by default. Supply a local destination with `--json` to receive download metadata.

## Configure workspace resources and networking

```bash
kibo pack list --zone global-1
kibo pack get kibotos-desktop-env --revision r42
kibo workspace create -t python-3.12 --pack kibotos-desktop-env --pack node-22 \
  --volume vol_1:/data --volume vol_2:/cache
kibo workspace volume detach wks_... vol_2
kibo workspace volumes set wks_... vol_1:/data vol_2:/cache
kibo workspace resize wks_... --vcpus 4 --memory-mib 8192 --disk-mib 40960
kibo workspace packs set wks_... node-22
kibo workspace metrics wks_... --start 2026-11-16T10:00:00Z --end 2026-11-16T11:00:00Z
```

Volume bindings and resource changes require a stopped or failed workspace. Root disks can only grow. Pack changes take effect on the next start. Pass an empty list to `volumes set` or `packs set` to clear the current list.

```bash
kibo egress create --name proxy --address proxy.example.com:1080 --username agent \
  --password-env PROXY_PASSWORD --tls --udp-enabled
kibo resolver create --name dns --protocol udp --address 10.0.0.53:53
kibo workspace network wks_... --mode public_internet \
  --allow 203.0.113.10 --deny 203.0.113.0/24 \
  --egress egr_...=10.0.0.0/8 --resolver rsv_...=egr_... --drain-seconds 60
```

Both `egress` and `resolver` support `list`, `get`, `create`, `update`, and `delete`. Use `update --name` or label flags for metadata. Endpoint and protocol flags replace the complete configuration, including credentials for an authenticated egress. Network policy updates replace the whole policy. A resolver ID without `=egress_id` uses a direct path.

## Forward a local port

```bash
kibo tunnel wks_... --remote 8080 --listen 127.0.0.1:18080
kibo tunnel wks_... --unix-path /run/app.sock --listen 127.0.0.1:18081
```

Each local connection opens an authenticated WebSocket to the guest port or socket. Press Ctrl-C to stop forwarding.

## Ergonomics: Interactive vs. Scripting / AI Agents

- **Interactive terminal (TTY)**:
  - Omitting required flags or resource IDs opens minimal inline arrow-key pickers (`↑`/`↓` + `Enter`, e.g., `kibo ws create`, `kibo ws stop`, `kibo exp create`) — no heavy full-screen TUI.
  - Async mutations (`workspace create/start/stop/delete/network`, `volume create/resize/delete`) **wait by default** with a live stderr spinner; pass `--no-wait` to return immediately.
  - Destructive commands (`delete`, `revoke`, `stop --force`) ask `[y/N]` confirmation in interactive terminals (skip with `-y` / `--yes`).
  - Supports human-friendly units (`--memory 4Gi`, `--disk 20Gi`, `--size 50Gi`, `--timeout 1h30m`, `--template python-3.12@r42`) and unique workspace/volume names in place of IDs.
  - Colored status output via ANSI (automatically disabled on non-TTY or when `NO_COLOR` / `--no-color` is set).
- **Scripting & AI Agents (Non-Interactive)**:
  - Automatically enters non-interactive mode when stdin/stderr is not a TTY, or when `--json`, `-q`/`--quiet`, `--no-input`, `CI=true`, or `KIBO_NO_INPUT=1` is set — **never blocks on stdin**.
  - Pass `--json` (or `-o json`, or set `KIBO_OUTPUT=json`) for JSON on `stdout` and structured JSON errors (with actionable `hint` / `details.missing_flags`) on `stderr`.
  - Pass `-q` / `--quiet` to print only resource IDs to `stdout` (ideal for `WS=$(kibo ws create -t python-3.12 -q)`).
  - Exit codes:
    - `0`: Success
    - `1`: API, runtime, or cancelled error
    - `2`: Usage, missing flag, ambiguous name, or validation error
    - `3`: Unauthenticated (no credentials or HTTP `401`)

For AI coding agents driving `kibo`, see the paired [`use-kibo`](../skills/use-kibo/SKILL.md) skill.
