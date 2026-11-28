import { afterEach, describe, expect, it } from 'vitest';
import {
  AuthenticationError,
  ConflictError,
  DEFAULT_BASE_URL,
  Kibotos,
  KibotosAPIError,
  KibotosError,
  NotFoundError,
  PermissionDeniedError,
  RateLimitError,
  UnexpectedStatusError,
  WaitTimeoutError,
} from '../src/index';
import { normalizeBaseUrl } from '../src/http';
import { createMockFetch, errorResponse, jsonResponse, sampleZone } from './helpers';

describe('Kibotos client configuration & error mapping', () => {
  const originalApiKey = process.env.KIBOTOS_API_KEY;
  const originalApiUrl = process.env.KIBOTOS_API_URL;

  afterEach(() => {
    if (originalApiKey === undefined) {
      delete process.env.KIBOTOS_API_KEY;
    } else {
      process.env.KIBOTOS_API_KEY = originalApiKey;
    }
    if (originalApiUrl === undefined) {
      delete process.env.KIBOTOS_API_URL;
    } else {
      process.env.KIBOTOS_API_URL = originalApiUrl;
    }
  });

  it('normalizes baseUrl cleanly with or without /v1 and trailing slashes', () => {
    expect(normalizeBaseUrl('https://api.kibo.to/v1')).toBe('https://api.kibo.to/v1');
    expect(normalizeBaseUrl('https://api.kibo.to/v1/')).toBe('https://api.kibo.to/v1');
    expect(normalizeBaseUrl('https://api.kibo.to')).toBe('https://api.kibo.to/v1');
    expect(normalizeBaseUrl('https://api.kibo.to///')).toBe('https://api.kibo.to/v1');
    expect(normalizeBaseUrl('http://localhost:8080')).toBe('http://localhost:8080/v1');
    expect(normalizeBaseUrl('http://localhost:8080/proxy/v1/')).toBe('http://localhost:8080/proxy/v1');
    expect(normalizeBaseUrl('   ')).toBe(DEFAULT_BASE_URL);
  });

  it('reads KIBOTOS_API_KEY and KIBOTOS_API_URL from process.env by default', async () => {
    process.env.KIBOTOS_API_KEY = 'kb_from_env';
    process.env.KIBOTOS_API_URL = 'https://staging.kibo.to/';

    const { fetch, requests } = createMockFetch(() =>
      jsonResponse({ data: [sampleZone], next_page_token: null }),
    );
    const client = new Kibotos({ fetch });
    expect(client.baseUrl).toBe('https://staging.kibo.to/v1');

    await client.zones.list();
    expect(requests).toHaveLength(1);
    expect(requests[0]?.url).toBe('https://staging.kibo.to/v1/zones');
    expect(requests[0]?.headers.get('Authorization')).toBe('Bearer kb_from_env');
    expect(requests[0]?.headers.get('Accept')).toBe('application/json');
  });

  it('works safely in non-Node runtimes where globalThis.process is undefined', async () => {
    const desc = Object.getOwnPropertyDescriptor(globalThis, 'process');
    Object.defineProperty(globalThis, 'process', {
      value: undefined,
      configurable: true,
      writable: true,
    });

    try {
      const { fetch, requests } = createMockFetch(() =>
        jsonResponse({ data: [], next_page_token: null }),
      );
      const client = new Kibotos({ apiKey: 'kb_worker', fetch });
      expect(client.baseUrl).toBe(DEFAULT_BASE_URL);
      await client.zones.list();
      expect(requests[0]?.headers.get('Authorization')).toBe('Bearer kb_worker');
    } finally {
      if (desc) {
        Object.defineProperty(globalThis, 'process', desc);
      }
    }
  });

  it('omits Authorization header when no apiKey is configured and merges default + per-request headers', async () => {
    delete process.env.KIBOTOS_API_KEY;
    const { fetch, requests } = createMockFetch(() =>
      jsonResponse({ data: [], next_page_token: null }),
    );
    const client = new Kibotos({
      fetch,
      defaultHeaders: { 'X-Client': 'default-val', 'X-Override': 'from-default' },
    });

    await client.zones.list(undefined, {
      headers: { 'X-Override': 'from-request', 'X-Extra': 'yes' },
    });

    const headers = requests[0]!.headers;
    expect(headers.has('Authorization')).toBe(false);
    expect(headers.get('X-Client')).toBe('default-val');
    expect(headers.get('X-Override')).toBe('from-request');
    expect(headers.get('X-Extra')).toBe('yes');
  });

  it('maps API error responses to the full error hierarchy', async () => {
    const cases: Array<{
      status: number;
      code: string;
      headers?: Record<string, string>;
      details?: Record<string, unknown>;
      expectedClass: new (...args: never[]) => KibotosAPIError;
    }> = [
      { status: 400, code: 'invalid_request', expectedClass: KibotosAPIError },
      { status: 401, code: 'unauthenticated', expectedClass: AuthenticationError },
      { status: 403, code: 'quota_exceeded', expectedClass: PermissionDeniedError },
      { status: 404, code: 'workspace_not_found', expectedClass: NotFoundError },
      {
        status: 408,
        code: 'wait_timeout',
        details: { workspace: { id: 'wks_1', status: 'creating' } },
        expectedClass: WaitTimeoutError,
      },
      { status: 409, code: 'invalid_state', expectedClass: ConflictError },
      {
        status: 409,
        code: 'unexpected_status',
        details: { status: 'failed' },
        expectedClass: UnexpectedStatusError,
      },
      {
        status: 429,
        code: 'rate_limited',
        headers: { 'Retry-After': '12' },
        expectedClass: RateLimitError,
      },
    ];

    for (const c of cases) {
      const { fetch } = createMockFetch(() =>
        errorResponse(c.status, c.code, `Failed with ${c.code}`, {
          retryable: false,
          requestId: `req_${c.code}`,
          details: c.details,
          headers: c.headers,
        }),
      );
      const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 0 });

      let caught: unknown;
      try {
        await client.zones.list();
      } catch (err) {
        caught = err;
      }

      expect(caught).toBeInstanceOf(KibotosError);
      expect(caught).toBeInstanceOf(KibotosAPIError);
      expect(caught).toBeInstanceOf(c.expectedClass);

      const apiErr = caught as KibotosAPIError;
      expect(apiErr.status).toBe(c.status);
      expect(apiErr.code).toBe(c.code);
      expect(apiErr.message).toBe(`Failed with ${c.code}`);
      expect(apiErr.requestId).toBe(`req_${c.code}`);
      expect(apiErr.details).toEqual(c.details);

      if (c.expectedClass === UnexpectedStatusError) {
        expect(caught).toBeInstanceOf(ConflictError);
      }
      if (c.expectedClass === RateLimitError) {
        expect((caught as RateLimitError).retryAfterMs).toBe(12_000);
      }
    }
  });

  it('handles non-JSON error responses gracefully using Request-Id header', async () => {
    const { fetch } = createMockFetch(
      () =>
        new Response('<html>Bad Gateway</html>', {
          status: 502,
          statusText: 'Bad Gateway',
          headers: { 'Content-Type': 'text/html', 'Request-Id': 'req_proxy_502' },
        }),
    );
    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 0 });

    await expect(client.zones.list()).rejects.toMatchObject({
      name: 'KibotosAPIError',
      status: 502,
      code: 'http_error',
      message: 'Bad Gateway',
      requestId: 'req_proxy_502',
      retryable: true,
    });
  });
});
