---
name: use-kibo
description: Drive the official `kibo` CLI for Kibotos — fast, lightweight cloud microVM workspaces built for AI agents. Use whenever creating, inspecting, starting, stopping, networking, timing out, or deleting Kibotos workspaces, managing persistent Kibotos volumes, exposing workspace ports over temporary HTTPS URLs, checking Kibotos zones, templates or Packs, running workspace commands, transferring files, forwarding ports, managing egresses or resolvers, or diagnosing `kibo` authentication and profiles.
---

# `use-kibo` — Operating Kibotos via the `kibo` CLI

**Kibotos** gives AI agents instant, isolated cloud microVM workspaces with clean Linux templates, durable persistent volumes, fine-grained outbound network controls, and one-command temporary HTTPS port exposures (`*.kibo.run`).

This skill teaches you how to drive the official **`kibo`** CLI reliably, safely, and with clean JSON output.

## 1. Preflight: Verify `kibo` & Authentication

Before performing any resource action, verify that `kibo` is installed and authenticated:

```bash
kibo whoami --json
```

- **Exit `0`**: Authenticated and connected. Inspect `organization_name`, `api_url`, `profile`, and `zones` (note the default zone).
- **Exit `3` (`unauthenticated`)**: No valid credentials are configured. Guide the user to authenticate — do **not** fabricate keys:
  - **Interactive (recommended for humans)**: Ask the user to run `kibo login` (opens the browser Device Authorization Flow and saves a profile to `~/.config/kibo/config.json`). In remote/SSH terminals, use `kibo login --no-browser`.
  - **Headless / CI**: Set `KIBOTOS_API_KEY=kb_...` in the environment, or save it with `kibo login --api-key kb_...`.
- Use `kibo whoami --offline --json` only when you need to inspect local profile metadata without making a network call.

## 2. Agent Operating Discipline

Follow these rules on every `kibo` invocation:

1. **Use `--json` for structured results.**
   - In `--json` mode, `stdout` carries JSON and interactive prompts are disabled. `exec --stream --json` emits one JSON event per line. Plain `files get` streams file bytes to stdout. Use a local destination to get metadata in JSON mode.
   - Progress updates, interactive login URLs/codes, multi-zone warnings, and structured error envelopes (`{"error": {...}}`) are always written to `stderr`.
   - For simple shell capture where you only need the resource ID, `-q` (`--quiet`) prints bare IDs to `stdout`.
2. **Async lifecycle commands wait by default (`--no-wait` for parallel fan-out)**:
   - Creation, start, stop, deletion, live network changes, and volume creation/resize/deletion **wait until completion by default** (`--wait-timeout` defaults to `5m` / `300s`).
   - Pass `--no-wait` only when you intentionally want to trigger multiple operations in parallel and wait on them afterwards.
3. **Check exit codes & `retryable` errors**:
   - `0` = success
   - `1` = API or runtime error (parse stderr JSON for `error.code`, `error.retryable`, `error.request_id`)
   - `2` = invalid usage, missing flag (`error.details.missing_flags`), ambiguous name (`error.details.candidates`), or flag validation error — inspect `error.hint` and fix the command rather than retrying
   - `3` = unauthenticated — prompt the user to run `kibo login` or set `KIBOTOS_API_KEY`
   - `exec` propagates the remote process status. Inspect its result or exit event before interpreting a nonzero status as a CLI error.
   - Retry safe requests after a short backoff when `error.retryable` is true. Check the outcome of execution or a file mutation before issuing it again.
4. **Protect secrets**:
   - Never print raw `kb_...` API keys or read `~/.config/kibo/config.json` into logs or chat responses. `kibo whoami` and `kibo profile list` always mask keys (`kb_...xxxx`).
5. **Confirm destructive operations**:
   - Confirm with the user before deleting workspaces (`kibo workspace delete`), deleting volumes (`kibo volume delete`), force-stopping (`kibo workspace stop --force`), or revoking live exposures (`kibo exposure revoke`), unless the user explicitly asked for cleanup.

## 3. High-Frequency Kibotos Recipes

### Discover Zones and Templates

```bash
kibo zone list --json
kibo template list --json
kibo template get python-3.12 --json
kibo template revisions python-3.12 --json
```

### Spin Up a Kibotos Workspace (Waits Until Available by Default)

```bash
kibo workspace create \
  --template python-3.12 \
  --name agent-sandbox \
  --vcpus 2 --memory 4Gi --disk 20Gi \
  --timeout 1h --timeout-action stop \
  --label purpose=agent-run \
  --json
```

### Expose a Service Port over Temporary HTTPS

```bash
kibo exposure create wks_... --port 8080 --ttl 1h --json
# Returns an active https://<token>.kibo.run URL
```

### Provision and Attach a Persistent Volume

Volumes must be in the same zone as the workspace and attached while the workspace is stopped (or attached at creation time with `--volume`):

```bash
# Option A: Create volume, then create workspace with it mounted
kibo volume create --size 50Gi --name workspace-data --zone global-1 --json
kibo workspace create --template python-3.12 --zone global-1 \
  --volume vol_... --mount-path /data --json

# Option B: Attach to an existing workspace
kibo workspace stop wks_... --json
kibo workspace volume attach wks_... vol_... --mount /data --json
kibo workspace start wks_... --json
```

### Execute commands, transfer files, and forward ports

```bash
kibo exec wks_... --json --workdir /app --timeout 60 -- python3 main.py
kibo exec wks_... --stream --timeout 900 --json -- python3 -u main.py
kibo files put wks_... ./main.py /app/main.py --create-only --json
kibo files get wks_... /app/output.log ./output.log --json
kibo tunnel wks_... --remote 8080 --listen 127.0.0.1:18080 --json
```

Pass `--stream --timeout <seconds>` for commands that may run past the default 30 seconds. Interactive `exec -it` requires a Unix terminal. Use the noninteractive forms for scripts and agents. A tunnel stays open until canceled.

### Extend Timeout, Toggle Network, Stop, or Clean Up

```bash
# Extend available workspace timeout by 2 hours
kibo workspace timeout wks_... --seconds 2h --action stop --json

# Lock down outbound internet access (waits until applied by default)
kibo workspace network wks_... --mode deny_all --json

# Stop or delete cleanly (waits until complete by default; pass --no-wait to return immediately)
kibo workspace stop wks_... --json
kibo workspace delete wks_... --json
```

## 4. Detailed References

Load these references only when you need full flag tables, multi-step patterns, or error recovery details:

- [CLI Reference](references/cli-reference.md): Complete `kibo` command tree, short aliases (`ws`, `vol`, `exp`, `tpl`), every flag, environment variables & precedence, exit codes, and API error codes.
- [Workflows & Troubleshooting](references/workflows.md): End-to-end Kibotos agent recipes (ephemeral sandboxes, persistent volumes, air-gapped workspaces, multi-profile CI) and error recovery playbooks.
