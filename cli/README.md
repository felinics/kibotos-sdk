# `kibo` — Official Kibotos CLI

`kibo` is the official command-line interface for **Kibotos**. Manage microVM workspaces, run commands, transfer files, open tunnels, and configure volumes, Packs, exposures, and networking from your terminal, CI pipeline, or AI agent.

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

# Choose a template and optionally name the workspace
kibo ws create

# Choose an available workspace and enter a command
kibo exec

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

## Guided commands

Use these short forms in a terminal. Supply any known target or value to skip its prompt.

| Command | Guided flow |
| --- | --- |
| `kibo exec` / `kibo exec -- uname -a` / `kibo exec -it` | Pick an available workspace. Enter a shell command when no argv or PTY is requested. `-it` opens a login shell |
| `kibo files ls` / `stat` / `get` / `put` / `mkdir` / `mv` / `rm` | Pick an available workspace. Supply only missing required paths. `ls` defaults to `.`, `get` defaults to stdout, and `rm` confirms deletion |
| `kibo tunnel` | Pick an available workspace and enter a guest TCP port. Enter accepts `8080` |
| `kibo pack get` | Pick a Pack by ID |
| `kibo egress create` / `get` / `update` / `delete` | Create from a SOCKS5 address and optional name. For other actions, pick the egress. Flagless updates prompt for a name and deletion asks for confirmation |
| `kibo resolver create` / `get` / `update` / `delete` | Create with a protocol, upstream, and optional name. For other actions, pick the resolver. Flagless updates prompt for a name and deletion asks for confirmation |
| `kibo ws resize` | Pick a stopped or failed workspace. Enter resource changes or keep current values |
| `kibo ws volumes set` / `kibo ws volume attach` / `kibo ws volume detach` | Pick an eligible workspace. Clearing bindings asks for confirmation. Attach offers same-zone available volumes and asks for a mount path when required |
| `kibo ws packs set` / `kibo ws metrics` | Pick a workspace. With `packs set`, enter the ordered Pack IDs. Metrics uses the default interval |

## Explicit commands for scripts and agents

Send everything required as arguments and flags. Prefer canonical IDs for stable references.

```bash
kibo exec wks_123 --no-input --json --workdir /app --env MODE=test --timeout 60 -- python3 main.py
printf 'hello' | kibo exec wks_123 --no-input --json --stdin - -- cat

kibo files ls wks_123 /app --depth 2 --no-input --json
kibo files stat wks_123 /app/main.py --no-input --json
kibo files get wks_123 /app/main.py ./main.py --no-input --json
kibo files put wks_123 ./main.py /app/main.py --mode 0644 --create-only --no-input --json
kibo files mkdir wks_123 /app/archive --no-input --json
kibo files mv wks_123 /app/main.py /app/archive/main.py --no-input --json
kibo files rm wks_123 /app/archive/main.py --no-input --json

kibo tunnel wks_123 8080 --listen 127.0.0.1:18080 --no-input --json
kibo tunnel wks_123 --unix-path /run/app.sock --listen 127.0.0.1:18081 --no-input --json
```

`exec` forwards the remote exit status. Use `--stream --json` for one JSON event per line. Download to a local file with `files get --json` to receive metadata, or omit JSON and the destination to stream raw bytes. A tunnel prints its local address at startup and stays open until cancelled. [Exec, files, and tunnels](docs/exec-files-and-tunnels.mdx)

```bash
kibo pack list --zone global-1 --no-input --json
kibo pack get node-22 --revision r42 --no-input --json
kibo ws resize wks_123 --vcpus 4 --memory-mib 8192 --disk-mib 40960 --no-input --json
kibo ws metrics wks_123 --start 2026-11-16T10:00:00Z --end 2026-11-16T11:00:00Z --no-input --json
kibo ws volume attach wks_123 vol_1 --mount /data --no-input --json
kibo ws volume detach wks_123 vol_1 --no-input --json
kibo ws volumes set wks_123 vol_1:/data vol_2:/cache --no-input --json
kibo ws packs set wks_123 node-22 kibotos-desktop-env --no-input --json
```

Resources can be resized while a workspace is stopped or failed. The root disk can only grow. Pack changes take effect on the next start. In scripts, passing only the workspace to `volumes set` or `packs set` clears the corresponding list. [Workspace configuration](docs/workspaces.mdx)

```bash
kibo egress create --name proxy --address proxy.example.com:1080 --username agent \
  --password-env PROXY_PASSWORD --tls --udp-enabled --no-input --json
kibo egress list --no-input --json
kibo egress get egr_123 --no-input --json
kibo egress update egr_123 --name proxy-east --no-input --json
kibo resolver create --name dns --protocol udp --address 10.0.0.53:53 --no-input --json
kibo resolver list --no-input --json
kibo resolver get rsv_123 --no-input --json
kibo resolver update rsv_123 --protocol https --url https://dns.example/dns-query --no-input --json
kibo ws network wks_123 --mode public_internet \
  --egress egr_123=10.0.0.0/8 --resolver rsv_123=egr_123 --no-input --json
```

After removing references from workspace policies, delete the resources with `kibo egress delete egr_123 --no-input --json` and `kibo resolver delete rsv_123 --no-input --json`.

Both networking resources support `list`, `get`, `create`, `update`, and `delete`. Updates accept metadata flags or a complete replacement configuration. Workspace network policy updates replace the whole policy. [Packs and networking](docs/packs-and-networking.mdx)

## Ergonomics: Interactive vs. Scripting / AI Agents

- **Interactive terminal (TTY)**:
  - Omitting required flags or resource IDs opens minimal inline arrow-key pickers (`↑`/`↓` + `Enter`, e.g., `kibo ws create`, `kibo ws stop`, `kibo exp create`) — no heavy full-screen TUI.
  - Async mutations (`workspace create/start/stop/delete/network`, `volume create/resize/delete`) **wait by default** with a live stderr spinner; pass `--no-wait` to return immediately.
  - Destructive commands (`delete`, `revoke`, `stop --force`) ask `[y/N]` confirmation in interactive terminals (skip with `-y` / `--yes`).
  - Supports human-friendly units (`--memory 4Gi`, `--disk 20Gi`, `--size 50Gi`, `--timeout 1h30m`, `--template python-3.12@r42`) and unique workspace, volume, egress, and resolver names in place of IDs.
  - Colored status output via ANSI (automatically disabled on non-TTY or when `NO_COLOR` / `--no-color` is set).
- **Scripting & AI Agents (Non-Interactive)**:
  - Automatically enters non-interactive mode when stdin or stderr is a pipe, `TERM=dumb`, or prompts are disabled by `--json`, `-q`, `--no-input`, `CI=true`, or `KIBO_NO_INPUT=1`. Explicit stdin transfers still read their input.
  - Pass `--json` (or `-o json`, or set `KIBO_OUTPUT=json`) for JSON on `stdout` and structured JSON errors (with actionable `hint` / `details.missing_flags`) on `stderr`.
  - Pass `-q` / `--quiet` to print only resource IDs to `stdout` (ideal for `WS=$(kibo ws create -t python-3.12 -q)`).
  - Missing required input returns exit 2 with a hint before authentication.
  - Exit codes:
    - `0`: Success
    - `1`: API, runtime, or cancelled error
    - `2`: Usage, missing flag, ambiguous name, or validation error
    - `3`: Unauthenticated (no credentials or HTTP `401`)
    - `exec` may return a remote exit code, `124` for a timeout, or `128 + signal`

For AI coding agents driving `kibo`, see the paired [`use-kibo`](../skills/use-kibo/SKILL.md) skill.
