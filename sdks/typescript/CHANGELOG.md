# Changelog

All notable changes to `@kibotos/sdk` are documented in this file.

## [0.1.0] - 2026-11-21

### Added

- Initial release of `@kibotos/sdk`, the official zero-dependency TypeScript and JavaScript SDK for the Kibotos v1 API.
- Standard Web API transport (`fetch`, `AbortSignal`, `crypto.randomUUID`) compatible with Node 20+, Bun, Deno, and Cloudflare Workers.
- Full typed resource coverage:
  - `client.zones`: `list`, `iterate`
  - `client.templates`: `list`, `iterate`, `get`, `listRevisions`, `getRevision`
  - `client.workspaces`: `create`, `list`, `iterate`, `get`, `update`, `delete`, `start`, `stop`, `setTimeout`, `setNetwork`, `attachVolume`, `detachVolume`, `wait`
  - `client.volumes`: `create`, `list`, `iterate`, `get`, `update`, `resize`, `delete`, `wait`
  - `client.exposures`: `create`, `list`, `iterate`, `get`, `revoke`
- Automatic `AsyncIterable` pagination via `iterate()` and multi-chunk long polling in `wait()` for timeouts exceeding 60 seconds.
- Automatic UUID v4 `Idempotency-Key` headers on `POST` mutations, exponential backoff retries honoring `Retry-After`, and typed `KibotosError` hierarchy.
