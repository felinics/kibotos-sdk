# Kibotos v1 API & CLI Device Auth Contract (Frozen Phase 0)

This contract is the shared source of truth across `kibotos-cloud` (`cmd/openapi`, `region-api`, `console-api`) and `kibotos-sdk` (Go SDK, TypeScript SDK, Python SDK, `kibo` CLI, and `use-kibo` skill).

## 1. Base URL, Auth & Common Conventions

- Default production API base URL: `https://api.kibo.to/v1` (configurable via `KIBOTOS_API_URL` or client option; trailing `/v1` or base URL normalized cleanly).
- Default console URL (for CLI login): `https://console.kibo.to` (configurable via `KIBOTOS_CONSOLE_URL` or `--console-url`).
- Authentication header: `Authorization: Bearer kb_<40 base62><6 base62 crc32>` (49 chars total).
- Content-Type / Accept: `application/json`.
- Response header on every response: `Request-Id`.
- Idempotency header on `POST` mutations: `Idempotency-Key: <1..255 chars>` (clients auto-generate a UUID v4 if caller omits it on retryable POST requests).
- Timestamps: RFC 3339 UTC strings (e.g. `2026-11-21T12:00:00Z`).
- Nullable fields: serialized as `null` (never omitted for defined object keys).

### Pagination
- Query params: `limit` (default `20`, max `100`), `page_token` (opaque string).
- Standard page envelope:
  ```json
  { "data": [...], "next_page_token": "..." | null }
  ```
- Multi-zone page envelope (for `GET /v1/workspaces`, `GET /v1/volumes`, `GET /v1/templates`):
  ```json
  { "data": [...], "unavailable_zones": ["..."], "next_page_token": "..." | null }
  ```

### Error Envelope
```json
{
  "error": {
    "code": "invalid_state",
    "message": "Human-readable message",
    "status": 409,
    "request_id": "req_...",
    "retryable": false,
    "details": {}
  }
}
```
Retryable errors (`retryable: true`, HTTP 408/429/500/502/503/504): clients retry with exponential backoff + jitter (respecting `Retry-After` on 429) reusing the same `Idempotency-Key`.

Key error codes:
- 400: `invalid_request`, `template_unavailable_in_zone`, `zone_mismatch`
- 401: `unauthenticated`
- 403: `zone_not_permitted`, `permission_denied`, `organization_suspended`, `quota_exceeded`
- 404: `workspace_not_found`, `volume_not_found`, `exposure_not_found`, `template_not_found`, `template_revision_not_found`, `not_found`
- 408: `wait_timeout` (`retryable: true`, `details.workspace` or `details.volume`)
- 409: `invalid_state`, `operation_in_progress` (`retryable: true`), `unexpected_status`, `volume_attached`, `template_not_ready`, `zone_draining`, `idempotency_key_reused`
- 429: `rate_limited` (`retryable: true`)
- 500: `internal_error` (`retryable: true`)
- 503: `capacity_unavailable`, `zone_unavailable`, `authentication_unavailable`, `outcome_unknown` (all `retryable: true`)

---

## 2. v1 Endpoints & Schemas

### Zones
- `GET /v1/zones?limit=&page_token=` -> `200` `{ data: Zone[], next_page_token: string | null }`
- `Zone`:
  ```json
  { "id": "global-1", "display_name": "Global 1", "region": "global", "status": "available", "default": true }
  ```
  - `status`: `"available" | "draining"`

### Templates (Platform read-only in v1 initial; organization templates reserved)
- `GET /v1/templates?owner=&zone=&limit=&page_token=` -> `200` `{ data: Template[], unavailable_zones: string[], next_page_token: string | null }`
- `GET /v1/templates/{id}` -> `200` `Template`
- `GET /v1/templates/{id}/revisions?limit=&page_token=` -> `200` `{ data: TemplateRevision[], next_page_token: string | null }`
- `GET /v1/templates/{id}/revisions/{revision_id}` -> `200` `TemplateRevision`
- `Template`:
  ```json
  {
    "id": "python-3.12",
    "owner": "platform",
    "display_name": "Python 3.12",
    "description": "Python 3.12 Linux environment",
    "default_revision": "r42",
    "latest_revision": null,
    "zones": ["global-1"],
    "created_at": null,
    "updated_at": null
  }
  ```
  - `owner`: `"platform" | "organization"`
  - `latest_revision`: `{ "id": string, "status": string } | null`
- `TemplateRevision`:
  ```json
  {
    "id": "r42",
    "template_id": "python-3.12",
    "status": "ready",
    "failure": null,
    "source": null,
    "process": null,
    "size_mib": null,
    "zones": ["global-1"],
    "created_at": null,
    "ready_at": null
  }
  ```

### Workspaces
- `POST /v1/workspaces` -> `202` `Workspace`
  - Request body:
    ```json
    {
      "name": "agent-runner",
      "zone": "global-1",
      "template": { "id": "python-3.12", "revision": "r42" },
      "resources": { "vcpus": 2, "memory_mib": 4096, "disk_mib": 20480 },
      "network": { "mode": "public_internet" },
      "volume": { "id": "vol_...", "mount_path": "/data", "delete_with_workspace": false },
      "timeout": { "seconds": 3600, "action": "stop" },
      "labels": { "team": "ml" }
    }
    ```
    Only `template.id` is required; all other fields optional.
- `GET /v1/workspaces?status=&zone=&label.<k>=<v>&limit=&page_token=` -> `200` `{ data: Workspace[], unavailable_zones: string[], next_page_token: string | null }`
- `GET /v1/workspaces/{id}` -> `200` `Workspace`
- `PATCH /v1/workspaces/{id}` -> `200` `Workspace`
  - Request body: `{ "name": "new-name" | null, "labels": { "k": "v", "del_me": null } }`
- `DELETE /v1/workspaces/{id}` -> `202` `Workspace`
- `POST /v1/workspaces/{id}/start` -> `202` `Workspace`
  - Optional body: `{ "timeout": { "seconds": 3600, "action": "stop" } }`
- `POST /v1/workspaces/{id}/stop` -> `202` `Workspace`
  - Optional body: `{ "force": false }`
- `POST /v1/workspaces/{id}/timeout` -> `200` `Workspace`
  - Body: `{ "seconds": 3600, "action": "stop" }`
- `PUT /v1/workspaces/{id}/network` -> `200` (when stopped) or `202` (when running) `Workspace`
  - Body: `{ "mode": "public_internet" | "deny_all" }`
- `PUT /v1/workspaces/{id}/volume` -> `200` `Workspace`
  - Body: `{ "id": "vol_...", "mount_path": "/data", "delete_with_workspace": false }`
- `DELETE /v1/workspaces/{id}/volume` -> `200` `Workspace`
- `GET /v1/workspaces/{id}/wait?status=running&status=failed&timeout=30` -> `200` `Workspace`
  - `status` repeatable, required; `timeout` default `30`, max `60`.
- `Workspace`:
  ```json
  {
    "id": "wks_...",
    "name": "agent-runner",
    "zone": "global-1",
    "template": { "id": "python-3.12", "revision": "r42" },
    "status": "running",
    "failure": null,
    "resources": { "vcpus": 2, "memory_mib": 4096, "disk_mib": 20480 },
    "network": { "mode": "public_internet", "applied": true },
    "volume": { "id": "vol_...", "mount_path": "/data", "delete_with_workspace": false },
    "timeout": { "expires_at": "2026-11-21T13:00:00Z", "action": "stop" },
    "labels": { "team": "ml" },
    "created_at": "2026-11-21T12:00:00Z",
    "updated_at": "2026-11-21T12:00:05Z"
  }
  ```
  - `status`: `"creating" | "running" | "stopping" | "stopped" | "starting" | "deleting" | "failed" | "unknown"`
  - `failure`: `{ "code": string, "message": string, "retryable": boolean } | null`

### Exposures
- `POST /v1/workspaces/{id}/exposures` -> `201` `Exposure`
  - Body: `{ "port": 8080, "ttl_seconds": 3600 }` (`ttl_seconds` optional, default `3600`)
- `GET /v1/workspaces/{id}/exposures?include_revoked=false&limit=&page_token=` -> `200` `{ data: Exposure[], next_page_token: string | null }`
- `GET /v1/workspaces/{id}/exposures/{exposure_id}` -> `200` `Exposure`
- `DELETE /v1/workspaces/{id}/exposures/{exposure_id}` -> `200` `Exposure`
- `Exposure`:
  ```json
  {
    "id": "exp_...",
    "workspace_id": "wks_...",
    "port": 8080,
    "url": "https://k7x2n4p6r8t0w2y6.kibo.run",
    "status": "active",
    "expires_at": "2026-11-21T13:00:00Z",
    "revoked_at": null,
    "created_at": "2026-11-21T12:00:00Z"
  }
  ```
  - `status`: `"active" | "revoked" | "expired"`

### Volumes
- `POST /v1/volumes` -> `202` `Volume`
  - Body: `{ "name": "datasets", "zone": "global-1", "size_gib": 50, "labels": {} }` (`size_gib` required; others optional)
- `GET /v1/volumes?zone=&attached=&label.<k>=<v>&limit=&page_token=` -> `200` `{ data: Volume[], unavailable_zones: string[], next_page_token: string | null }`
- `GET /v1/volumes/{id}` -> `200` `Volume`
- `PATCH /v1/volumes/{id}` -> `200` `Volume`
  - Body: `{ "name": "new-name" | null, "labels": { "k": "v", "del_me": null } }`
- `POST /v1/volumes/{id}/resize` -> `202` `Volume`
  - Body: `{ "size_gib": 100 }`
- `DELETE /v1/volumes/{id}` -> `202` `Volume`
- `GET /v1/volumes/{id}/wait?status=available&timeout=30` -> `200` `Volume`
- `Volume`:
  ```json
  {
    "id": "vol_...",
    "name": "datasets",
    "zone": "global-1",
    "size_gib": 50,
    "status": "available",
    "failure": null,
    "workspace_id": null,
    "labels": {},
    "created_at": "2026-11-21T12:00:00Z",
    "updated_at": "2026-11-21T12:00:00Z"
  }
  ```
  - `status`: `"creating" | "available" | "attached" | "resizing" | "deleting" | "failed" | "unknown"`

---

## 3. CLI Device Authorization Flow (on Console API)

Base URL: `<console_url>/api` (e.g. `https://console.kibo.to/api` or `http://localhost:5173/api`).

1. **Start device session** (unauthenticated):
   - `POST /api/auth/device/start`
   - Body: `{ "client_name": "kibo", "device_name": "MacBook Pro" }` (both optional)
   - Response `201`:
     ```json
     {
       "device_code": "opaque_secret_device_code",
       "user_code": "ABCD-EFGH",
       "verification_url": "https://console.kibo.to/device",
       "verification_url_complete": "https://console.kibo.to/device?user_code=ABCD-EFGH",
       "expires_in": 600,
       "interval": 5
     }
     ```
2. **Browser authorization** (console web `/device`, authenticated session cookie):
   - `GET /api/auth/device/info?user_code=ABCD-EFGH` -> `200`:
     ```json
     {
       "user_code": "ABCD-EFGH",
       "client_name": "kibotos-cli",
       "device_name": "MacBook Pro",
       "expires_at": "2026-11-21T12:10:00Z",
       "status": "pending"
     }
     ```
   - `POST /api/auth/device/approve` -> `200` `{ "status": "approved" }`
     - Body: `{ "user_code": "ABCD-EFGH", "organization_id": "org_...", "device_name": "CLI · MacBook Pro", "scopes": ["workspaces:read", ...], "zone_scope": "all", "zone_ids": [], "expires_in_days": 90 }`
     - Backend verifies user has `api_keys:write` in that organization, creates an organization API key via `region-api`, encrypts the one-time `kb_...` secret onto the device auth session, marks it `approved`.
   - `POST /api/auth/device/deny` -> `200` `{ "status": "denied" }`
     - Body: `{ "user_code": "ABCD-EFGH" }`
3. **CLI token polling** (unauthenticated):
   - `POST /api/auth/device/token`
   - Body: `{ "device_code": "..." }`
   - Responses:
     - Still waiting: `202` `{ "status": "authorization_pending" }`
     - Approved (one-time consume, wipes stored secret): `200`:
       ```json
       {
         "status": "approved",
         "api_key": "kb_...",
         "key_id": "key_...",
         "organization_id": "org_...",
         "organization_name": "Acme",
         "user_email": "alice@acme.com",
         "api_url": "https://api.kibo.to/v1"
       }
       ```
     - Denied: `403` error code `access_denied`
     - Expired / invalid / already consumed: `410` error code `expired_token`
