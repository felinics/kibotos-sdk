# Changelog

All notable changes to the `kibotos` Python SDK are documented in this file.

## [0.1.0] - 2026-11-21

### Added

- Initial release of `kibotos`, the official Python SDK for the Kibotos v1 API (Python 3.10+).
- Synchronous (`Kibotos`) and asynchronous (`AsyncKibotos`) clients with context manager support (`with` / `async with`), built on `httpx` and Pydantic v2 (`py.typed` included).
- Full resource coverage for both sync and async clients:
  - `client.zones`: `list`, `iterate`
  - `client.templates`: `list`, `iterate`, `get`, `list_revisions`, `get_revision`
  - `client.workspaces`: `create`, `list`, `iterate`, `get`, `update`, `delete`, `start`, `stop`, `set_timeout`, `set_network`, `attach_volume`, `detach_volume`, `wait`
  - `client.volumes`: `create`, `list`, `iterate`, `get`, `update`, `resize`, `delete`, `wait`
  - `client.exposures`: `create`, `list`, `iterate`, `get`, `revoke`
- `UNSET` sentinel PATCH semantics on `update()` so omitted fields remain untouched while explicit `None` clears values.
- Automatic `iterate()` pagination, chunked `wait()` for timeouts longer than 60 seconds, automatic UUID v4 `Idempotency-Key` headers on `POST` mutations, exponential backoff retries honoring `Retry-After`, and typed `KibotosError` exceptions.
