import { describe, expect, it, vi } from 'vitest';
import { Kibotos, NotFoundError } from '../src/index';
import { createMockFetch, errorResponse, jsonResponse, sampleWorkspace } from './helpers';

const id = 'wks/a';
const workspacePath = '/v1/workspaces/wks%2Fa';
const filePath = '/%data/a b+?.txt';
const attachment = { id: 'vol_123', mount_path: '/data', delete_with_workspace: false };
const network = {
  mode: 'deny_all' as const,
  allow: ['203.0.113.0/24'],
  deny: ['198.51.100.0/24'],
  egresses: [{ egress_id: 'egr', destinations: ['10.0.0.0/8'] }],
  resolvers: [{ resolver_id: 'rsv', path: { type: 'egress' as const, egress_id: 'egr' } }],
};
const file = {
  path: filePath, name: 'a b+?.txt', type: 'file', mode: '0640', modified_at: null, size_bytes: 2,
};

describe('Wave 3 control plane', () => {
  it('creates and updates workspaces with plural bindings, resources and network policy', async () => {
    const workspace = {
      ...sampleWorkspace,
      volume: attachment,
      volumes: [attachment],
      packs: [{ id: 'desktop', revision: 'r1', provided_capabilities: ['desktop.v1'] }],
      network: { ...network, applied: null },
    };
    const metrics = {
      earliest_time: null,
      guest: [],
      host: [{ time: '2026-11-21T12:00:00Z', cpu_cores: 0.5 }],
      step_seconds: 60,
    };
    const { fetch, requests } = createMockFetch(({ parsedUrl }) =>
      jsonResponse(parsedUrl.pathname.endsWith('/metrics') ? metrics : workspace),
    );
    const workspaces = new Kibotos({ apiKey: 'kb_test', fetch }).workspaces;
    const create = { template: { id: 'python' }, volumes: [attachment], packs: ['desktop'], network };
    expect(await workspaces.create(create)).toEqual(workspace);

    const resources = { vcpus: 4, memory_mib: 8192, disk_mib: 40960 };
    await workspaces.update(id, { name: null, labels: { old: null }, resources });
    await workspaces.setVolumes(id, { volumes: [] });
    await workspaces.setPacks(id, { packs: [] });
    await workspaces.setNetwork(id, { ...network, allow: [], drain_seconds: 0 });
    const start = new Date('2026-11-21T12:00:00Z');
    const end = '2026-11-21T12:30:00Z';
    expect(await workspaces.getMetrics(id, { start, end })).toEqual(metrics);

    expect(requests.map(({ method, parsedUrl, body }) => [method, parsedUrl.pathname, body])).toEqual([
      ['POST', '/v1/workspaces', create],
      ['PATCH', workspacePath, { name: null, labels: { old: null }, resources }],
      ['PUT', workspacePath + '/volumes', { volumes: [] }],
      ['PUT', workspacePath + '/packs', { packs: [] }],
      ['PUT', workspacePath + '/network', { ...network, allow: [], drain_seconds: 0 }],
      ['GET', workspacePath + '/metrics', undefined],
    ]);
    expect(Object.fromEntries(requests[5]!.parsedUrl.searchParams)).toEqual({ start: start.toISOString(), end });
  });

  it('lists packs and fetches encoded pack and revision IDs', async () => {
    const pack = { id: 'pack/a', default_revision: 'r?1' };
    const page = { data: [pack], unavailable_zones: ['global-2'], next_page_token: 'next' };
    const revision = { id: 'r?1', pack_id: pack.id };
    const { fetch, requests } = createMockFetch((_req, index) => jsonResponse([page, pack, revision][index]));
    const packs = new Kibotos({ apiKey: 'kb_test', fetch }).packs;
    expect(await packs.list({ zone: 'global-1', limit: 3, page_token: 'x +' })).toEqual(page);
    expect(await packs.get(pack.id)).toEqual(pack);
    expect(await packs.getRevision(pack.id, revision.id)).toEqual(revision);
    expect(requests.map((r) => r.parsedUrl.pathname)).toEqual([
      '/v1/packs', '/v1/packs/pack%2Fa', '/v1/packs/pack%2Fa/revisions/r%3F1',
    ]);
    expect(Object.fromEntries(requests[0]!.parsedUrl.searchParams)).toEqual({
      zone: 'global-1', limit: '3', page_token: 'x +',
    });
  });

  it.each(['egresses', 'resolvers'] as const)('implements all %s operations', async (resource) => {
    const result = { id: 'resource/a', revision: 2 };
    const page = { data: [result], unavailable_zones: [], next_page_token: 'next' };
    const { fetch, requests } = createMockFetch((request) => {
      if (request.method === 'DELETE') return new Response(null, { status: 204 });
      return jsonResponse(request.parsedUrl.pathname.endsWith(resource) && request.method === 'GET' ? page : result);
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    const egress = { protocol: 'socks5' as const, socks5: { address: 'proxy.example:1080', password: 'example' } };
    const resolver = { protocol: 'https' as const, https: { url: 'https://dns.example/dns-query' } };
    const create = resource === 'egresses' ? egress : resolver;
    const replacement = resource === 'egresses' ? { ...egress, drain_seconds: 0 } : resolver;
    if (resource === 'egresses') {
      expect(await client.egresses.create(egress)).toEqual(result);
    } else {
      expect(await client.resolvers.create(resolver)).toEqual(result);
    }
    const api = client[resource];
    expect(await api.list({ zone: 'global-1', limit: 2, page_token: 'a +', labels: { team: 'a/b' } })).toEqual(page);
    expect(await api.get(result.id)).toEqual(result);
    if (resource === 'egresses') {
      expect(await client.egresses.replace(result.id, { ...egress, drain_seconds: 0 })).toEqual(result);
    } else {
      expect(await client.resolvers.replace(result.id, resolver)).toEqual(result);
    }
    const patch = { name: null, labels: { old: null } };
    expect(await api.update(result.id, patch)).toEqual(result);
    expect(await api.delete(result.id)).toBeUndefined();

    const url = '/v1/' + resource;
    const itemUrl = url + '/resource%2Fa';
    expect(requests.map((r) => [r.method, r.parsedUrl.pathname, r.body])).toEqual([
      ['POST', url, create], ['GET', url, undefined], ['GET', itemUrl, undefined],
      ['PUT', itemUrl, replacement], ['PATCH', itemUrl, patch], ['DELETE', itemUrl, undefined],
    ]);
    expect(requests[0]!.headers.has('Idempotency-Key')).toBe(true);
    expect(Object.fromEntries(requests[1]!.parsedUrl.searchParams)).toEqual({
      zone: 'global-1', limit: '2', page_token: 'a +', 'label.team': 'a/b',
    });
  });
});

describe('Wave 3 data plane', () => {
  const execRequest = { argv: ['echo', '日本語'], env: { LANG: 'C.UTF-8' }, timeout_seconds: 45 };

  it('sends the exec JSON request and parses the result', async () => {
    const result = {
      duration_ms: 5, exit_code: 0, signal: null, stdout: 'hello', stderr: '',
      termination_reason: 'exited', truncated: false,
    };
    const { fetch, requests } = createMockFetch(() => jsonResponse(result));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    expect(await client.workspaces.exec(id, execRequest)).toEqual(result);
    expect(requests[0]).toMatchObject({ method: 'POST', body: execRequest });
    expect(requests[0]!.parsedUrl.pathname).toBe(workspacePath + '/exec');
    expect(requests[0]!.headers.get('Accept')).toBe('application/json');
    expect(requests[0]!.headers.has('Idempotency-Key')).toBe(false);
  });

  it('gives exec a 15-second margin and uses deadlines on streams only when requested', async () => {
    const timeout = vi.spyOn(AbortSignal, 'timeout');
    try {
      const { fetch, requests } = createMockFetch(() => jsonResponse({}));
      const ws = new Kibotos({ apiKey: 'kb_test', fetch }).workspaces;
      await ws.exec(id, { argv: ['true'] });
      await ws.exec(id, execRequest);
      await ws.exec(id, execRequest, { timeoutMs: 1234 });
      const requestStream = () => new ReadableStream<Uint8Array>({
        start(controller) { controller.close(); },
      });
      await ws.execStream(id, execRequest).next();
      await ws.readFile(id, filePath, { as: 'bytes' });
      await ws.writeFile(id, filePath, requestStream());
      expect(requests.slice(3).map(({ signal }) => signal)).toEqual([undefined, undefined, undefined]);

      await ws.execStream(id, execRequest, { timeoutMs: 2345 }).next();
      await ws.readFile(id, filePath, { as: 'bytes', timeoutMs: 3456 });
      await ws.writeFile(id, filePath, requestStream(), { timeoutMs: 4567 });
      expect(timeout.mock.calls.map(([ms]) => ms)).toEqual([45_000, 60_000, 1234, 2345, 3456, 4567]);
    } finally {
      timeout.mockRestore();
    }
  });

  it('parses all named SSE events across CRLF, UTF-8 and arbitrary chunk boundaries', async () => {
    const text = '\ufeff: keepalive\r\nevent: started\r\ndata: {"pid":1}\r\n\r\n'
      + 'event: stdout\ndata: {"data":"日本語"}\n\nevent: stderr\rdata: {\rdata: "data":"first\\nsecond"}\r\r'
      + 'event: heartbeat\ndata: {}\n\nevent: exited\ndata: {"exit_code":2}\n\n'
      + 'event: error\ndata: {"code":"internal"}\n\nevent: future\ndata: {}\n\n'
      + 'event: stdout\ndata: {"data":"incomplete"}\n';
    const encoded = new TextEncoder().encode(text);
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const value of encoded) controller.enqueue(Uint8Array.of(value));
        controller.close();
      },
    });
    const { fetch, requests } = createMockFetch(() => new Response(stream));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    const events = [];
    for await (const event of client.workspaces.execStream(id, execRequest)) events.push(event);
    expect(events).toEqual([
      { event: 'started', data: { pid: 1 } },
      { event: 'stdout', data: { data: '日本語' } },
      { event: 'stderr', data: { data: 'first\nsecond' } },
      { event: 'heartbeat', data: {} },
      { event: 'exited', data: { exit_code: 2 } },
      { event: 'error', data: { code: 'internal' } },
    ]);
    expect(requests[0]!.parsedUrl.pathname).toBe(workspacePath + '/exec');
    expect(requests[0]!.headers.get('Accept')).toBe('text/event-stream');
    expect(requests[0]!.headers.has('Idempotency-Key')).toBe(false);
  });

  it('cancels the unread body when the exec iterator is closed', async () => {
    const cancel = vi.fn();
    const body = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('event: started\ndata: {}\n\n'));
      },
      cancel,
    });
    const { fetch } = createMockFetch(() => new Response(body));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    for await (const event of client.workspaces.execStream(id, execRequest)) {
      expect(event.event).toBe('started');
      break;
    }
    expect(cancel).toHaveBeenCalledOnce();
    expect(body.locked).toBe(false);
  });

  it.each(['exec', 'execStream'] as const)('does not replay %s after a retryable HTTP failure', async (method) => {
    const { fetch, requests } = createMockFetch(() => errorResponse(503, 'unavailable', 'Try again', { retryable: true }));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    const call = method === 'exec'
      ? client.workspaces.exec(id, execRequest)
      : client.workspaces.execStream(id, execRequest).next();
    await expect(call).rejects.toMatchObject({ status: 503, code: 'unavailable' });
    expect(requests).toHaveLength(1);
  });

  it('lists, stats, creates, moves and removes files with correct path, query and body', async () => {
    const { fetch, requests } = createMockFetch((req) => {
      if (req.method === 'DELETE') return new Response(null, { status: 204 });
      return jsonResponse(req.parsedUrl.pathname.endsWith('/files') ? { path: '/%data', entries: [file] } : file);
    });
    const workspaces = new Kibotos({ apiKey: 'kb_test', fetch }).workspaces;
    expect((await workspaces.listFiles(id, '/%data', { depth: 1, user: 'root' })).entries).toEqual([file]);
    expect(await workspaces.statFile(id, filePath, { user: 'root' })).toEqual(file);
    expect(await workspaces.makeDir(id, { path: '/%data', mode: '0750', user: 'root' })).toEqual(file);
    expect(await workspaces.moveFile(id, { source: filePath, destination: '/new', user: 'root' })).toEqual(file);
    expect(await workspaces.deleteFile(id, filePath, { user: 'root' })).toBeUndefined();

    expect(requests.map((r) => [r.method, r.parsedUrl.pathname])).toEqual([
      ['GET', workspacePath + '/files'], ['GET', workspacePath + '/files/stat'],
      ['POST', workspacePath + '/files/mkdir'], ['POST', workspacePath + '/files/move'],
      ['DELETE', workspacePath + '/files'],
    ]);
    expect(Object.fromEntries(requests[0]!.parsedUrl.searchParams)).toEqual({ path: '/%data', depth: '1', user: 'root' });
    for (const index of [1, 4]) {
      expect(Object.fromEntries(requests[index]!.parsedUrl.searchParams)).toEqual({ path: filePath, user: 'root' });
    }
    expect(requests[2]!.body).toEqual({ path: '/%data', mode: '0750', user: 'root' });
    expect(requests[3]!.body).toEqual({ source: filePath, destination: '/new', user: 'root' });
  });

  it('does not send an idempotency key or retry file mutations after a dropped response', async () => {
    const { fetch, requests } = createMockFetch(() => { throw new Error('response dropped'); });
    const ws = new Kibotos({
      apiKey: 'kb_test', fetch, maxRetries: 1, defaultHeaders: { 'Idempotency-Key': 'default-key' },
    }).workspaces;
    const opts = { idempotencyKey: 'request-key' };
    await expect(ws.makeDir(id, { path: '/%data' }, opts)).rejects.toThrow('response dropped');
    await expect(ws.moveFile(id, { source: filePath, destination: '/new' }, opts)).rejects.toThrow('response dropped');
    await expect(ws.deleteFile(id, filePath, opts)).rejects.toThrow('response dropped');
    expect(requests.map(({ method, headers }) => [method, headers.get('Idempotency-Key')])).toEqual([
      ['POST', null], ['POST', null], ['DELETE', null],
    ]);
  });

  it('exposes range metadata, bytes, a stream, a 304 response and structured errors', async () => {
    const { fetch, requests } = createMockFetch((_r, i) => {
      if (i === 3) return new Response(null, { status: 304 });
      if (i === 4) return errorResponse(404, 'file_not_found', 'Missing');
      return new Response(Uint8Array.of(0, 255), {
        status: 206, headers: { ETag: '"etag"', 'Content-Range': 'bytes 1-2/3' },
      });
    });
    const workspaces = new Kibotos({ apiKey: 'kb_test', fetch }).workspaces;
    const response = await workspaces.readFile(id, filePath, { range: 'bytes=1-2', user: 'root' });
    expect(response.status).toBe(206);
    expect(response.headers.get('Content-Range')).toBe('bytes 1-2/3');
    expect(await workspaces.readFile(id, filePath, { as: 'bytes' })).toEqual(Uint8Array.of(0, 255));
    const stream = await workspaces.readFile(id, filePath, { as: 'stream' });
    expect(await new Response(stream).arrayBuffer()).toEqual(Uint8Array.of(0, 255).buffer);
    expect((await workspaces.readFile(id, filePath, { ifNoneMatch: '"etag"' })).status).toBe(304);
    await expect(workspaces.readFile(id, filePath)).rejects.toBeInstanceOf(NotFoundError);
    expect(requests[0]!.parsedUrl.pathname).toBe(workspacePath + '/files/content');
    expect(Object.fromEntries(requests[0]!.parsedUrl.searchParams)).toEqual({ path: filePath, user: 'root' });
    expect(requests[0]!.headers.get('Range')).toBe('bytes=1-2');
    expect(requests[3]!.headers.get('If-None-Match')).toBe('"etag"');
  });

  it('keeps the caller signal active while a file response is consumed', async () => {
    const controller = new AbortController();
    const { fetch } = createMockFetch(({ signal }) => new Response(new ReadableStream({
      start(body) {
        signal!.addEventListener('abort', () => body.error(signal!.reason), { once: true });
      },
    })));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    const response = await client.workspaces.readFile(id, filePath, { signal: controller.signal });
    controller.abort(new Error('download stopped'));
    await expect(response.arrayBuffer()).rejects.toThrow('download stopped');
  });

  it('writes strings, binary arrays, blobs and streams with the expected raw bytes', async () => {
    const result = { path: filePath, size_bytes: 2, sha256: 'hash' };
    const fetch: typeof globalThis.fetch = vi.fn(async (input, init?: RequestInit & { duplex?: string }) => {
      const url = new URL(String(input));
      expect(url.pathname).toBe(workspacePath + '/files/content');
      expect(Object.fromEntries(url.searchParams)).toEqual({
        path: filePath, mode: '0640', create_only: 'false', user: 'root',
      });
      expect(init?.method).toBe('PUT');
      expect(new Headers(init?.headers).get('Content-Type')).toBe('application/octet-stream');
      expect(init?.duplex).toBe(init?.body instanceof ReadableStream ? 'half' : undefined);
      expect(await new Response(init?.body).text()).toBe('ok');
      return jsonResponse(result);
    });
    const workspaces = new Kibotos({ apiKey: 'kb_test', fetch }).workspaces;
    const bytes = new TextEncoder().encode('ok');
    const stream = new ReadableStream<Uint8Array>({
      start(c) { c.enqueue(bytes); c.close(); },
    });
    for (const body of ['ok', bytes, new Blob([bytes]), stream]) {
      expect(await workspaces.writeFile(id, filePath, body, { mode: '0640', createOnly: false, user: 'root' }))
        .toEqual(result);
    }
    expect(fetch).toHaveBeenCalledTimes(4);
  });

  it('does not replay a streaming file write after a retryable response', async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(Uint8Array.of(1));
        controller.close();
      },
    });
    const fetch: typeof globalThis.fetch = vi.fn(async (_input, init) => {
      await new Response(init?.body).arrayBuffer();
      return errorResponse(503, 'unavailable', 'Try again', { retryable: true });
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });
    await expect(client.workspaces.writeFile(id, filePath, stream, { createOnly: true }))
      .rejects.toMatchObject({ status: 503, code: 'unavailable' });
    expect(fetch).toHaveBeenCalledOnce();
  });
});
