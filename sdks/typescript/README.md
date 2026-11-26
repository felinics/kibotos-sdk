# `@kibotos/sdk`

Official zero-dependency TypeScript and JavaScript SDK for the **Kibotos** v1 API.

Built exclusively on standard Web APIs (`fetch`, `URL`, `URLSearchParams`, `AbortController`, `AbortSignal.timeout`, `crypto.randomUUID`), `@kibotos/sdk` runs seamlessly in **Node 20+**, **Bun**, **Deno**, and **Cloudflare Workers**.

## Installation

```bash
npm install @kibotos/sdk
# or
pnpm add @kibotos/sdk
# or
bun add @kibotos/sdk
```

## Quick Start

```ts
import { Kibotos } from '@kibotos/sdk';

// Reads KIBOTOS_API_KEY and KIBOTOS_API_URL (default: https://api.kibo.to/v1)
const client = new Kibotos();

// 1. Create a workspace
const created = await client.workspaces.create({
  name: 'agent-runner',
  template: { id: 'python-3.12' },
  resources: { vcpus: 2, memory_mib: 4096 },
  timeout: { seconds: 3600, action: 'stop' },
  labels: { env: 'dev' },
});

// 2. Wait until available (supports timeouts > 60s via automatic chunking)
const workspace = await client.workspaces.wait(created.id, {
  status: ['available', 'failed'],
  timeoutSeconds: 120,
});
console.log('Workspace ready:', workspace.id, workspace.status);

// 3. Expose a port over HTTPS
const exposure = await client.exposures.create(workspace.id, {
  port: 8080,
  ttl_seconds: 3600,
});
console.log('Public URL:', exposure.url);

// 4. Clean up
await client.exposures.revoke(workspace.id, exposure.id);
await client.workspaces.delete(workspace.id);
```

## Configuration

```ts
import { Kibotos } from '@kibotos/sdk';

const client = new Kibotos({
  apiKey: 'kb_...',                  // Defaults to process.env.KIBOTOS_API_KEY
  baseUrl: 'https://api.kibo.to/v1', // Defaults to process.env.KIBOTOS_API_URL ?? 'https://api.kibo.to/v1'
  maxRetries: 3,                     // Defaults to 3
  timeoutMs: 30_000,                 // Defaults to 30000 (30s)
  defaultHeaders: { 'X-App': 'my-service' },
  fetch: customFetch,                // Optional fetch override
});
```

In edge runtimes without `process.env` (such as Cloudflare Workers), pass `apiKey` explicitly from your environment bindings.

## Resources

| Namespace | Methods |
| --- | --- |
| `client.zones` | `list`, `iterate` |
| `client.templates` | `list`, `iterate`, `get`, `listRevisions`, `getRevision` |
| `client.workspaces` | `create`, `list`, `iterate`, `get`, `update`, `delete`, `start`, `stop`, `setTimeout`, `setNetwork`, `attachVolume`, `detachVolume`, `wait` |
| `client.volumes` | `create`, `list`, `iterate`, `get`, `update`, `resize`, `delete`, `wait` |
| `client.exposures` | `create`, `list`, `iterate`, `get`, `revoke` |

## Auto-Pagination & Label Filtering

Every listable resource exposes an `iterate(...)` method returning an `AsyncIterable<T>`:

```ts
for await (const ws of client.workspaces.iterate({
  status: 'available',
  labels: { team: 'ml', env: 'prod' }, // Serialized as label.team=ml&label.env=prod
  limit: 50,
})) {
  console.log(ws.id, ws.name);
}
```

## Retries, Idempotency & Cancellation

- **Automatic idempotency**: `POST` mutations automatically generate a UUID v4 `Idempotency-Key` header if omitted and reuse the exact same key across retries. You may also pass `idempotencyKey` in `RequestOptions`.
- **Retries**: Exponential backoff with jitter for `retryable: true` API errors, HTTP `429` (honoring `Retry-After` seconds or HTTP-date), `500`/`502`/`503`/`504`, and transient network failures.
- **Cancellation**: Pass an `AbortSignal` via `{ signal }` on any method to cancel in-flight requests, retry delays, and `wait()` loops.

## Error Handling

All SDK errors inherit from `KibotosError`:

```ts
import {
  APIConnectionError,
  AuthenticationError,
  ConflictError,
  KibotosAPIError,
  NotFoundError,
  PermissionDeniedError,
  RateLimitError,
  UnexpectedStatusError,
  WaitTimeoutError,
} from '@kibotos/sdk';

try {
  await client.workspaces.get('wks_missing');
} catch (err) {
  if (err instanceof NotFoundError) {
    console.error('Not found:', err.code, err.requestId);
  } else if (err instanceof RateLimitError) {
    console.error('Rate limited; retry after ms:', err.retryAfterMs);
  } else if (err instanceof UnexpectedStatusError) {
    console.error('Wait stopped on unexpected status:', err.details);
  } else if (err instanceof WaitTimeoutError) {
    console.error('Wait timed out:', err.details);
  } else if (err instanceof KibotosAPIError) {
    console.error('API error:', err.status, err.code, err.message);
  } else if (err instanceof APIConnectionError) {
    console.error('Connection error:', err.message);
  }
}
```

## License

MIT
