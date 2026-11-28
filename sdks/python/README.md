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

## Workspace Configuration and Catalogs

Workspace creation accepts `volumes`, Pack IDs in `packs`, and a `network` policy with `allow`, `deny`, `egresses`, and `resolvers`. Use `workspaces.update(workspace_id, resources={...})` to change CPU, memory, or grow the root disk of a stopped or failed workspace. Use `set_volumes` and `set_packs` to replace the complete lists. An empty list clears the corresponding bindings. A workspace binds up to four Packs, and Pack changes take effect on the next start. The legacy `volume` field and single-volume methods remain available. `attach_volume` leaves only the given volume attached and `detach_volume` removes every attachment, so use `set_volumes` on multi-volume workspaces.

`set_network` replaces the whole policy and also accepts `drain_seconds`. The response's `network.applied` is `None` while the workspace is not running. `get_metrics(workspace_id, start=..., end=...)` accepts RFC 3339 strings or timezone-aware datetimes and returns host and guest time series.

| Namespace | Methods |
| --- | --- |
| `client.zones` | `list`, `iterate` |
| `client.templates` | `list`, `iterate`, `get`, `list_revisions`, `get_revision` |
| `client.workspaces` | `create`, `list`, `iterate`, `get`, `update`, `delete`, `start`, `stop`, `set_timeout`, `set_network`, `set_volumes`, `set_packs`, `get_metrics`, `attach_volume`, `detach_volume`, `wait` |
| `client.workspaces` (data plane) | `exec`, `exec_stream`, `list_files`, `stat_file`, `read_file`, `write_file`, `make_dir`, `move_file`, `delete_file`, `ws_url`, `tunnel_ws_url`, `desktop_vnc_url`, `proxy_url`, `proxy_ws_url`, `ws_headers` |
| `client.volumes` | `create`, `list`, `iterate`, `get`, `update`, `resize`, `delete`, `wait` |
| `client.exposures` | `create`, `list`, `iterate`, `get`, `revoke` |
| `client.packs` | `list`, `get`, `get_revision` |
| `client.egresses`, `client.resolvers` | `create`, `list`, `get`, `replace` (configuration), `update` (name and labels), `delete` |

Packs, egresses, and resolvers have no `iterate` helper. Pass the response's `next_page_token` as `page_token` to fetch another page.

## Commands and Files

```python
from contextlib import closing

from kibotos import ExecRequest

ws = client.workspaces
result = ws.exec(workspace_id, ExecRequest(argv=["python3", "--version"]))
print(result.exit_code, result.stdout)

with closing(ws.exec_stream(workspace_id, {"argv": ["ls", "-l"]})) as events:
    for event in events:
        print(event.event, event.data)

ws.make_dir(workspace_id, "/tmp/example", mode="0750")
ws.write_file(workspace_id, "/tmp/example/hello.txt", b"hello", mode="0640", create_only=True)
with ws.read_file(workspace_id, "/tmp/example/hello.txt", stream=True, range="bytes=0-2") as response:
    print(response.status_code, response.headers.get("Content-Range"))
    for chunk in response.iter_bytes():
        print(chunk)
```

`exec_stream` yields `started`, `stdout`, `stderr`, `heartbeat`, `exited`, and `error` events and decodes JSON event data when present. Use `async for` with `AsyncKibotos`, and use `contextlib.aclosing` when you may exit an async iterator early. `exec` defaults to an HTTP timeout of the command's `timeout_seconds` (30 seconds if omitted) plus 15 seconds. `exec_stream`, `read_file`, and `write_file` use an HTTP timeout only when you pass `timeout` on the request.

`read_file` returns bytes by default. Use `stream=True` to access the response status, ETag, and byte iterator. Pass `if_none_match` to revalidate an ETag and check for `304` on the response. With `AsyncKibotos`, use `async with await ws.read_file(..., stream=True)` and `response.aiter_bytes()`. `write_file` accepts either bytes or an iterable of byte chunks, using an async iterable with `AsyncKibotos`. The other filesystem methods are `list_files`, `stat_file`, `make_dir`, `move_file`, and `delete_file`. All seven methods accept a workspace `user`.

## WebSocket URLs and Framing

Use `ws_url` for multiplexed workspace streams, `tunnel_ws_url` for a raw TCP or Unix socket tunnel, `desktop_vnc_url` for the VNC stream, and `proxy_ws_url` for a WebSocket upgrade to a workspace service. `proxy_url` returns the HTTP(S) service URL. Pass the headers from `ws_headers()` to your WebSocket implementation for the authenticated handshake.

```python
from kibotos import WorkspaceChannel, decode_binary_frame, encode_binary_frame, encode_open_frame

url = client.workspaces.ws_url(workspace_id)
headers = client.workspaces.ws_headers()
control = encode_open_frame(1, "process", {
    "action": "start", "mode": "pipe", "argv": ["cat"],
})
frame = encode_binary_frame(1, WorkspaceChannel.STDIN, b"hello\n")
stream_id, channel, payload = decode_binary_frame(frame)
```

Send `control` as a WebSocket text message to open the stream. The JSON envelope is `{"type":"open","stream_id":1,"kind":"process","params":{"action":"start","mode":"pipe","argv":["cat"]}}`. Use a stream ID from 1 through `0xFFFFFFFF` for stream opens and binary frames.

Binary frames have a six-byte header with version `0x01`, a big-endian 32-bit stream ID, and a channel byte. The channel values are `0` stdin, `1` stdout, `2` stderr, `3` PTY, and `4` tunnel.

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

- **Idempotency**: Control-plane `POST` mutations automatically include a UUID v4 `Idempotency-Key` header (unless you pass `idempotency_key=...`) and reuse the same key across retries. `exec`, `make_dir`, and `move_file` never send one.
- **Retries & `Retry-After`**: Exponential backoff with jitter automatically honors server `Retry-After` headers on `429 RateLimitError`.
- **One-shot data-plane requests**: `exec`, `exec_stream`, `read_file`, `write_file`, `make_dir`, `move_file`, and `delete_file` make a single attempt.
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

## Documentation

Reference guides live in [`docs/`](./docs) and are published at [docs.kibo.to](https://docs.kibo.to): workspaces, volumes, exposures, exec / files / tunnels, Packs and networking, templates and zones, and errors and retries.

## Running Tests

```bash
PYTHONPATH=src python3 -m unittest discover -s tests -v
```
