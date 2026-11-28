import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  APIConnectionError,
  ConflictError,
  Kibotos,
  NotFoundError,
  RateLimitError,
} from '../src/index';
import { parseRetryAfterMs } from '../src/errors';
import { createMockFetch, errorResponse, jsonResponse, sampleWorkspace } from './helpers';

describe('Retries, Retry-After, Idempotency-Key reuse, and AbortSignal', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('parses Retry-After headers in delta-seconds and HTTP-date formats', () => {
    expect(parseRetryAfterMs('5')).toBe(5_000);
    expect(parseRetryAfterMs('0.25')).toBe(250);
    expect(parseRetryAfterMs('0')).toBe(0);

    const now = Date.UTC(2026, 10, 21, 12, 0, 0);
    expect(parseRetryAfterMs('Sat, 21 Nov 2026 12:00:10 GMT', now)).toBe(10_000);
    expect(parseRetryAfterMs('Sat, 21 Nov 2026 11:59:50 GMT', now)).toBe(0);
    expect(parseRetryAfterMs('not-a-valid-value', now)).toBeUndefined();
    expect(parseRetryAfterMs(null, now)).toBeUndefined();
  });

  it('auto-generates an Idempotency-Key on POST mutations and reuses the exact same key on retries', async () => {
    vi.useFakeTimers();

    const { fetch, requests } = createMockFetch((_req, idx) => {
      if (idx === 0) {
        return errorResponse(503, 'capacity_unavailable', 'Try again', { retryable: true });
      }
      if (idx === 1) {
        return errorResponse(429, 'rate_limited', 'Slow down', {
          retryable: true,
          headers: { 'Retry-After': '2' },
        });
      }
      return jsonResponse(sampleWorkspace, { status: 202 });
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 3 });

    const promise = client.workspaces.create({ template: { id: 'python-3.12' } });
    await vi.runAllTimersAsync();
    const ws = await promise;

    expect(ws.id).toBe('wks_123');
    expect(requests).toHaveLength(3);

    const key0 = requests[0]?.headers.get('Idempotency-Key');
    const key1 = requests[1]?.headers.get('Idempotency-Key');
    const key2 = requests[2]?.headers.get('Idempotency-Key');
    expect(key0).toMatch(/^[0-9a-f-]{36}$/i);
    expect(key1).toBe(key0);
    expect(key2).toBe(key0);
  });

  it('honors custom idempotencyKey and reuses it across retries', async () => {
    vi.useFakeTimers();

    const { fetch, requests } = createMockFetch((_req, idx) => {
      if (idx === 0) {
        return errorResponse(500, 'internal_error', 'Oops', { retryable: true });
      }
      return jsonResponse(sampleWorkspace, { status: 202 });
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 2 });
    const promise = client.workspaces.create(
      { template: { id: 'python-3.12' } },
      { idempotencyKey: 'my-custom-key-001' },
    );
    await vi.runAllTimersAsync();
    await promise;

    expect(requests).toHaveLength(2);
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe('my-custom-key-001');
    expect(requests[1]?.headers.get('Idempotency-Key')).toBe('my-custom-key-001');
  });

  it('retries 409 operation_in_progress when retryable: true, but does NOT retry non-retryable 404/409', async () => {
    vi.useFakeTimers();

    const { fetch: retryFetch, requests: retryReqs } = createMockFetch((_req, idx) => {
      if (idx === 0) {
        return errorResponse(409, 'operation_in_progress', 'Busy', { retryable: true });
      }
      return jsonResponse(sampleWorkspace);
    });
    const retryClient = new Kibotos({ apiKey: 'kb_test', fetch: retryFetch, maxRetries: 2 });
    const p1 = retryClient.workspaces.start('wks_123');
    await vi.runAllTimersAsync();
    expect((await p1).id).toBe('wks_123');
    expect(retryReqs).toHaveLength(2);

    const { fetch: noRetryFetch, requests: noRetryReqs } = createMockFetch(() =>
      errorResponse(404, 'workspace_not_found', 'Missing', { retryable: false }),
    );
    const noRetryClient = new Kibotos({ apiKey: 'kb_test', fetch: noRetryFetch, maxRetries: 3 });
    await expect(noRetryClient.workspaces.get('wks_missing')).rejects.toBeInstanceOf(NotFoundError);
    expect(noRetryReqs).toHaveLength(1);

    const { fetch: conflictFetch, requests: conflictReqs } = createMockFetch(() =>
      errorResponse(409, 'invalid_state', 'Cannot start', { retryable: false }),
    );
    const conflictClient = new Kibotos({ apiKey: 'kb_test', fetch: conflictFetch, maxRetries: 3 });
    await expect(conflictClient.workspaces.start('wks_123')).rejects.toBeInstanceOf(ConflictError);
    expect(conflictReqs).toHaveLength(1);
  });

  it('honors Retry-After header delay on 429 and throws RateLimitError when retries exhausted', async () => {
    vi.useFakeTimers();

    const { fetch, requests } = createMockFetch(() =>
      errorResponse(429, 'rate_limited', 'Rate limit exceeded', {
        retryable: true,
        headers: { 'Retry-After': '3' },
      }),
    );

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 1 });
    const promise = client.workspaces.get('wks_123');
    // Attach rejection handler before advancing fake timers to avoid unhandled rejection warning
    const assertion = expect(promise).rejects.toSatisfy((err: unknown) => {
      return err instanceof RateLimitError && err.retryAfterMs === 3_000;
    });

    // Advance just short of Retry-After (3s): second attempt should not have fired yet
    await vi.advanceTimersByTimeAsync(2_999);
    expect(requests).toHaveLength(1);

    // Advance the remaining 1ms: second attempt fires and fails with RateLimitError
    await vi.advanceTimersByTimeAsync(1);
    await assertion;
    expect(requests).toHaveLength(2);
  });

  it('honors HTTP-date Retry-After header on 429', async () => {
    vi.useFakeTimers();
    const baseNow = Date.UTC(2026, 10, 21, 12, 0, 0);
    vi.setSystemTime(baseNow);

    const { fetch, requests } = createMockFetch((_req, idx) => {
      if (idx === 0) {
        return errorResponse(429, 'rate_limited', 'Rate limited', {
          retryable: true,
          headers: { 'Retry-After': 'Sat, 21 Nov 2026 12:00:05 GMT' },
        });
      }
      return jsonResponse(sampleWorkspace);
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 1 });
    const promise = client.workspaces.get('wks_123');

    await vi.advanceTimersByTimeAsync(4_999);
    expect(requests).toHaveLength(1);

    await vi.advanceTimersByTimeAsync(1);
    const ws = await promise;
    expect(ws.id).toBe('wks_123');
    expect(requests).toHaveLength(2);
  });

  it('retries when a per-request timeoutMs fires and surfaces APIConnectionError if exhausted', async () => {
    const { fetch, requests } = createMockFetch(
      (req) =>
        new Promise<Response>((_resolve, reject) => {
          req.signal?.addEventListener(
            'abort',
            () => reject(req.signal?.reason ?? new Error('aborted')),
            { once: true },
          );
        }),
    );

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 0, timeoutMs: 25 });
    await expect(client.workspaces.get('wks_123')).rejects.toSatisfy(
      (err: unknown) => err instanceof APIConnectionError && err.message.includes('timed out'),
    );
    expect(requests).toHaveLength(1);
  });

  it('retries transient network failures and throws APIConnectionError once exhausted', async () => {
    vi.useFakeTimers();

    const { fetch, requests } = createMockFetch(() => {
      throw new TypeError('fetch failed: ECONNRESET');
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 2 });
    const promise = client.workspaces.get('wks_123');
    const assertion = expect(promise).rejects.toBeInstanceOf(APIConnectionError);

    await vi.runAllTimersAsync();
    await assertion;
    expect(requests).toHaveLength(3);
  });

  it('cancels immediately when AbortSignal fires during retry backoff', async () => {
    const controller = new AbortController();

    const { fetch, requests } = createMockFetch(() => {
      // Trigger abort shortly after returning 429 with long Retry-After
      setTimeout(() => controller.abort(new Error('user-cancelled')), 15);
      return errorResponse(429, 'rate_limited', 'Wait a long time', {
        retryable: true,
        headers: { 'Retry-After': '60' },
      });
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 3 });

    const start = Date.now();
    await expect(
      client.workspaces.get('wks_123', { signal: controller.signal }),
    ).rejects.toThrow('user-cancelled');
    const elapsed = Date.now() - start;

    expect(requests).toHaveLength(1);
    expect(elapsed).toBeLessThan(1_000);
  });

  it('does not send request if AbortSignal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort(new Error('pre-aborted'));

    const { fetch, requests } = createMockFetch(() => jsonResponse(sampleWorkspace));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    await expect(
      client.workspaces.get('wks_123', { signal: controller.signal }),
    ).rejects.toThrow('pre-aborted');
    expect(requests).toHaveLength(0);
  });
});
