# `@kibotos/sdk`

Official zero-dependency TypeScript and JavaScript SDK for the **Kibotos** v1 API.

The HTTP client uses built-in Web APIs including `fetch`, `ReadableStream`, and `AbortController` on **Node 20+**, **Bun**, **Deno**, and **Cloudflare Workers**.

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
| `client.workspaces` | `create`, `list`, `iterate`, `get`, `update`, `delete`, `start`, `stop`, `setTimeout`, `setNetwork`, `setVolumes`, `setPacks`, `getMetrics`, `attachVolume`, `detachVolume`, `wait` |
| `client.volumes` | `create`, `list`, `iterate`, `get`, `update`, `resize`, `delete`, `wait` |
| `client.exposures` | `create`, `list`, `iterate`, `get`, `revoke` |
| `client.packs` | `list`, `get`, `getRevision` |
| `client.egresses`, `client.resolvers` | `create`, `list`, `get`, `replace` (configuration), `update` (name and labels), `delete` |

Workspace creation accepts `volumes`, Pack IDs in `packs`, and a `network` policy with `allow`, `deny`, `egresses`, and `resolvers`. Use `update(id, { resources: { vcpus, memory_mib, disk_mib } })` to resize a stopped or failed workspace. The root disk can only grow. `setVolumes` and `setPacks` take the complete list, and an empty list clears it. Pack changes take effect on the next start. The legacy `volume` field and single-volume methods remain available.

`setNetwork` also accepts `drain_seconds`. The response's `network.applied` is `null` while the workspace is inactive. `getMetrics(id, { start, end })` accepts RFC 3339 strings or `Date` values and returns host and guest time series.

## Commands and Files

```ts
const ws = client.workspaces;
const result = await ws.exec(workspaceId, { argv: ['python3', '--version'] });
console.log(result.exit_code, result.stdout);

for await (const { event, data } of ws.execStream(workspaceId, { argv: ['ls', '-l'] })) {
  console.log(event, data);
}

await ws.makeDir(workspaceId, { path: '/tmp/example', mode: '0750' });
await ws.writeFile(workspaceId, '/tmp/example/hello.txt', 'hello', {
  mode: '0640', createOnly: true,
});
const response = await ws.readFile(workspaceId, '/tmp/example/hello.txt', {
  range: 'bytes=0-2',
});
console.log(response.status, response.headers.get('Content-Range'), await response.text());
```

`execStream` yields `started`, `stdout`, `stderr`, `heartbeat`, `exited`, and `error` events. It decodes JSON event data when present. `exec` defaults to the command's `timeout_seconds` (30 seconds if omitted) plus 15 seconds. `execStream`, `readFile`, and `writeFile` have no default client deadline. Pass `timeoutMs` to set a per-request deadline or `signal` to cancel a request.

`readFile` returns a `Response` by default. Pass `as: 'bytes'` for a `Uint8Array` or `as: 'stream'` for its readable body. Use the response form to check `206` and `Content-Range`, or `304` when using `ifNoneMatch`. `writeFile` also accepts a `Uint8Array`, `Blob`, or `ReadableStream<Uint8Array>`. The directory and metadata methods are `listFiles`, `statFile`, `makeDir`, `moveFile`, and `deleteFile`. All seven file methods accept a workspace user override.

## WebSocket Connections

```ts
import { WorkspaceChannel } from '@kibotos/sdk';

const connection = client.workspaces.connectWS(workspaceId);
connection.onFrame((message) => {
  if (message.type === 'binary' && message.frame.channel === WorkspaceChannel.stdout) {
    console.log(message.frame.stream_id, message.frame.payload);
  }
  if (message.type === 'control') console.log(message.frame);
});
connection.socket.addEventListener('open', () => {
  connection.open(1, 'process', { action: 'start', mode: 'pipe', argv: ['printf', 'hello'] });
});
```

`WorkspaceSocket` supports `process`, `fs.watch`, and `tunnel` streams on one connection. Use `sendControl` for JSON controls and `sendBinary` for stream payloads. The exported `encodeBinaryFrame` and `decodeBinaryFrame` use a version byte (`0x01`), a big-endian 32-bit stream ID, and a channel byte (`0` stdin, `1` stdout, `2` stderr, `3` PTY, `4` tunnel).

`connectTunnel`, `connectDesktop`, and `connectProxy` return raw WebSockets. Use `tunnelWSUrl`, `desktopVNCUrl`, `proxyWSUrl`, `wsUrl`, and `wsHeaders` to build an upgrade for your own transport. `proxyUrl` builds the HTTP(S) URL.

Authenticated native upgrades use Node's global `WebSocket` with handshake headers. Node 20 exposes the global from 20.10 with `--experimental-websocket`. For a runtime whose global lacks handshake header support, pass a `factory` that accepts the URL, headers, and requested subprotocols.

## Auto-Pagination & Label Filtering

Resources with an `iterate(...)` method return an `AsyncIterable<T>`. For Packs, egresses, and resolvers, pass `next_page_token` from the previous list response as `page_token`.

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

- **Automatic idempotency**: Control-plane `POST` mutations automatically generate a UUID v4 `Idempotency-Key` header if omitted and reuse the same key across retries. You may also pass `idempotencyKey` in `RequestOptions`.
- **Retries**: Exponential backoff with jitter for `retryable: true` API errors, HTTP `429` (honoring `Retry-After` seconds or HTTP-date), `500`/`502`/`503`/`504`, and transient network failures.
- **One-shot data-plane requests**: `exec`, `execStream`, `writeFile`, `makeDir`, `moveFile`, and `deleteFile` make a single attempt.
- **Cancellation**: Pass an `AbortSignal` via `{ signal }` to HTTP methods to cancel in-flight requests, retry delays, and `wait()` loops.

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
