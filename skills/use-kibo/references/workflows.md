# Kibo Agent Workflows & Troubleshooting

Concrete end-to-end recipes for driving Kibo via `kibo`.

## Workflow 1: Ephemeral Sandbox with HTTPS Preview

Provision a fast workspace with an auto-stop timeout, expose a web app port, and clean up afterwards:

```bash
# 1. Verify auth and check default zone
kibo whoami --json

# 2. Pick a ready template
kibo template list --json

# 3. Create workspace and block until running
WS_ID=$(kibo workspace create \
  --template python-3.12 \
  --name preview-sandbox \
  --timeout 1800 --timeout-action stop \
  --label task=preview \
  --wait --json | jq -r '.id')

# 4. Expose port 8080 over HTTPS
kibo exposure create "$WS_ID" --port 8080 --ttl 1800 --json

# 5. Revoke exposure and delete workspace when finished (confirm with user first)
kibo exposure revoke "$WS_ID" exp_... --json
kibo workspace delete "$WS_ID" --wait --json
```

## Workflow 2: Stateful Workspace with a Persistent Volume

Keep datasets, caches, or build artifacts across workspace lifecycles:

```bash
# 1. Create a 50 GiB volume in global-1 and wait until available
VOL_ID=$(kibo volume create \
  --size 50 \
  --name project-cache \
  --zone global-1 \
  --label project=alpha \
  --wait --json | jq -r '.id')

# 2. Create a workspace in the SAME zone mounting that volume at /data
WS_ID=$(kibo workspace create \
  --template python-3.12 \
  --zone global-1 \
  --name alpha-worker \
  --volume "$VOL_ID" \
  --mount-path /data \
  --wait --json | jq -r '.id')

# 3. Later, grow the volume to 100 GiB
kibo volume resize "$VOL_ID" --size 100 --wait --json

# 4. Detach the volume before deleting it or moving it to another workspace
kibo workspace stop "$WS_ID" --wait --json
kibo workspace volume detach "$WS_ID" --json
```

## Workflow 3: Air-Gapped / Restricted Network Execution

Run untrusted code with outbound internet disabled, or toggle outbound access dynamically:

```bash
# Start with no outbound internet
WS_ID=$(kibo workspace create \
  --template python-3.12 \
  --name isolated-eval \
  --network deny_all \
  --wait --json | jq -r '.id')

# Temporarily enable internet to fetch dependencies, waiting until applied
kibo workspace network "$WS_ID" --mode public_internet --wait --json

# Lock back down
kibo workspace network "$WS_ID" --mode deny_all --wait --json
```

## Workflow 4: Managing Multiple Environments / Profiles

```bash
# Sign in to separate profiles
kibo login --profile prod
kibo login --profile staging --console-url https://staging-console.kibo.to

# Inspect and switch profiles
kibo profile list --json
kibo profile use staging --json

# Override profile for a single command without changing active profile
kibo workspace list --profile prod --json
```

## Troubleshooting & Recovery

| Symptom / Error | Cause | Fix |
| --- | --- | --- |
| Exit `3` (`unauthenticated`) | Missing or revoked API key | Ask user to run `kibo login` or set `KIBOTOS_API_KEY` |
| `zone_mismatch` (`400`) | Volume is in a different zone than the workspace | Inspect `kibo volume get <vol_id> --json` and pass matching `--zone` |
| `template_unavailable_in_zone` (`400`) | Template is not replicated to the requested zone | Check `kibo template get <id> --json` `.zones` and pick a supported zone |
| `invalid_state` (`409`) | Operation requires another status (e.g. attach/detach volume while running) | Check `kibo workspace get <id> --json`, stop or start with `--wait` first |
| `operation_in_progress` (`409`, retryable) | Workspace/volume is mid-transition | Wait for current transition (`kibo workspace wait <id> --status running,stopped,failed`) then retry |
| `volume_attached` (`409`) | Deleting a volume still attached to a workspace | Stop the workspace, run `kibo workspace volume detach <ws_id>`, then delete the volume |
| `wait_timeout` (`408`, retryable) | Operation took longer than wait window | Inspect current state with `get` and re-run `kibo workspace wait` / `kibo volume wait` with a larger `--timeout` |
| `unexpected_status` (`409`) | Resource transitioned to `failed` during `--wait` | Run `kibo workspace get <id> --json` (or `volume get`) and inspect `.failure.code` and `.failure.message` |
| Non-empty `unavailable_zones` in list | One or more zones were temporarily unreachable during fan-out | Inform the user which zones were unavailable; retry or query a specific `--zone` |
