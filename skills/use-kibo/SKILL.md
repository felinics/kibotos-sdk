---
name: use-kibo
description: Drive the official `kibo` CLI for Kibo (Kibotos) — fast, lightweight cloud microVM workspaces built for AI agents. Use whenever creating, inspecting, starting, stopping, networking, timing out, or deleting Kibo workspaces, managing persistent Kibo volumes, exposing workspace ports over temporary HTTPS URLs, checking Kibo zones/templates, or diagnosing `kibo` authentication and profiles.
---

# `use-kibo` — Operating Kibo via the `kibo` CLI

**Kibo** (Kibotos) gives AI agents instant, isolated cloud microVM workspaces with clean Linux templates, durable persistent volumes, fine-grained outbound network controls, and one-command temporary HTTPS port exposures (`*.kibo.run`).

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

1. **Always pass `--json` (or `-o json`)**:
   - In `--json` mode, `stdout` is strictly valid JSON.
   - Progress updates (e.g., waiting messages), interactive login URLs/codes, multi-zone warnings, and structured error envelopes (`{"error": {...}}`) are always written to `stderr`.
2. **Always pass `--wait` on async lifecycle commands**:
   - Creation, start, stop, deletion, live network changes, and volume creation/resize/deletion return asynchronously (`202`). Pass `--wait` (and `--wait-timeout <sec>` if needed, default `300`) so the resource finishes transitioning before your next step.
3. **Check exit codes & `retryable` errors**:
   - `0` = success
   - `1` = API or runtime error (parse stderr JSON for `error.code`, `error.retryable`, `error.request_id`)
   - `2` = invalid usage, arguments, or flag values — fix the command rather than retrying
   - `3` = unauthenticated — prompt the user to run `kibo login` or set `KIBOTOS_API_KEY`
   - When `error.retryable` is `true` (e.g., `operation_in_progress`, `wait_timeout`, `rate_limited`, `capacity_unavailable`), back off briefly and retry.
4. **Protect secrets**:
   - Never print raw `kb_...` API keys or read `~/.config/kibo/config.json` into logs or chat responses. `kibo whoami` and `kibo profile list` always mask keys (`kb_...xxxx`).
5. **Confirm destructive operations**:
   - Confirm with the user before deleting workspaces (`kibo workspace delete`), deleting volumes (`kibo volume delete`), force-stopping (`kibo workspace stop --force`), or revoking live exposures (`kibo exposure revoke`), unless the user explicitly asked for cleanup.

## 3. High-Frequency Kibo Recipes

### Discover Zones and Templates

```bash
kibo zone list --json
kibo template list --json
kibo template get python-3.12 --json
kibo template revisions python-3.12 --json
```

### Spin Up a Kibo Workspace (and Wait Until Available)

```bash
kibo workspace create \
  --template python-3.12 \
  --name agent-sandbox \
  --vcpus 2 --memory 4096 --disk 20480 \
  --timeout 3600 --timeout-action stop \
  --label purpose=agent-run \
  --wait --json
```

### Expose a Service Port over Temporary HTTPS

```bash
kibo exposure create wks_... --port 8080 --ttl 3600 --json
# Returns an active https://<token>.kibo.run URL
```

### Provision and Attach a Persistent Volume

Volumes must be in the same zone as the workspace and attached while the workspace is stopped (or attached at creation time with `--volume`):

```bash
# Option A: Create volume, then create workspace with it mounted
kibo volume create --size 50 --name workspace-data --zone global-1 --wait --json
kibo workspace create --template python-3.12 --zone global-1 \
  --volume vol_... --mount-path /data --wait --json

# Option B: Attach to an existing workspace
kibo workspace stop wks_... --wait --json
kibo workspace volume attach wks_... --volume vol_... --mount-path /data --json
kibo workspace start wks_... --wait --json
```

### Extend Timeout, Toggle Network, Stop, or Clean Up

```bash
# Extend available workspace timeout by 2 hours
kibo workspace timeout wks_... --seconds 7200 --action stop --json

# Lock down outbound internet access (and wait until applied)
kibo workspace network wks_... --mode deny_all --wait --json

# Stop or delete cleanly
kibo workspace stop wks_... --wait --json
kibo workspace delete wks_... --wait --json
```

## 4. Detailed References

Load these references only when you need full flag tables, multi-step patterns, or error recovery details:

- [CLI Reference](references/cli-reference.md): Complete `kibo` command tree, short aliases (`ws`, `vol`, `exp`, `tpl`), every flag, environment variables & precedence, exit codes, and API error codes.
- [Workflows & Troubleshooting](references/workflows.md): End-to-end Kibo agent recipes (ephemeral sandboxes, persistent volumes, air-gapped workspaces, multi-profile CI) and error recovery playbooks.
