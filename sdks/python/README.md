# Kibotos Python SDK

Official synchronous and asynchronous Python client for the **Kibotos** v1 API, built on `httpx` and Pydantic v2.

## Installation

```bash
pip install kibotos
```

Requires Python 3.10+.

## Quick Start

Set your API key in the environment (`KIBOTOS_API_KEY=kb_...`), or pass `api_key="..."` directly:

```python
from kibotos import Kibotos

with Kibotos() as client:
    # Discover zones and templates
    zones = client.zones.list()
    print("Zones:", [z.id for z in zones.data])

    # Create a workspace and wait until it is available
    wks = client.workspaces.create(
        template="python-3.12",
        name="agent-runner",
        resources={"vcpus": 2, "memory_mib": 4096},
        labels={"team": "ml"},
    )
    wks = client.workspaces.wait(wks.id, status=["available", "failed"], timeout=120)
    print(f"Workspace {wks.id} is {wks.status}")

    # Expose a port over HTTPS
    exp = client.exposures.create(wks.id, port=8080, ttl_seconds=3600)
    print("Preview URL:", exp.url)

    # Clean up
    client.workspaces.delete(wks.id)
```

## Async Client

Every resource and pagination iterator is available asynchronously via `AsyncKibotos`:

```python
import asyncio
from kibotos import AsyncKibotos

async def main() -> None:
    async with AsyncKibotos() as client:
        async for wks in client.workspaces.iterate(status="available", labels={"team": "ml"}):
            print(wks.id, wks.name, wks.status)

asyncio.run(main())
```

## Configuration

| Option | Env Fallback | Default | Description |
| --- | --- | --- | --- |
| `api_key` | `KIBOTOS_API_KEY` | *(required)* | Organization API key (`kb_...`) |
| `base_url` | `KIBOTOS_API_URL` | `https://api.kibo.to/v1` | API base URL (automatically normalized to `/v1`) |
| `timeout` | — | `30.0` | Default HTTP request timeout in seconds |
| `max_retries` | — | `3` | Automatic retries for retryable errors (`429`, `5xx`, `retryable: true`, network errors) |
| `http_client` | — | `None` | Custom `httpx.Client` / `httpx.AsyncClient` |
| `transport` | — | `None` | Custom `httpx.BaseTransport` / `httpx.AsyncBaseTransport` |

### Reliability Features

- **Idempotency**: All `POST` mutations automatically include a UUID v4 `Idempotency-Key` header (unless you pass `idempotency_key=...`) and reuse the exact same key across retries.
- **Retries & `Retry-After`**: Exponential backoff with jitter automatically honors server `Retry-After` headers on `429 RateLimitError`.
- **Long Waits (`>60s`)**: `workspaces.wait()` and `volumes.wait()` automatically chunk timeouts longer than the server's 60-second long-poll maximum.
- **Forward Compatibility**: All Pydantic models use `extra="ignore"` so additive v1 API fields never break deserialization.

## Error Handling

All exceptions inherit from `KibotosError`:

- `APIError` (`status`, `code`, `request_id`, `retryable`, `details`)
  - `AuthenticationError` (401)
  - `PermissionDeniedError` (403)
  - `NotFoundError` (404)
  - `ConflictError` (409)
    - `UnexpectedStatusError` (409 `unexpected_status`)
  - `RateLimitError` (429, includes `retry_after`)
  - `WaitTimeoutError` (408 `wait_timeout`)
- `APIConnectionError` (network/transport failures)

## Running Tests

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -v
```
