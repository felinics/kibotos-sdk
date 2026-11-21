import { describe, expect, it } from 'vitest';
import {
  Kibotos,
  KibotosError,
  UnexpectedStatusError,
  WaitTimeoutError,
} from '../src/index.js';
import {
  createMockFetch,
  errorResponse,
  jsonResponse,
  sampleVolume,
  sampleWorkspace,
} from './helpers.js';

describe('workspaces.wait() and volumes.wait()', () => {
  it('sends repeatable status query params and default 30s timeout', async () => {
    const { fetch, requests } = createMockFetch(() => jsonResponse(sampleWorkspace));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const ws = await client.workspaces.wait('wks_123', {
      status: ['running', 'failed'],
    });

    expect(ws.status).toBe('running');
    expect(requests).toHaveLength(1);
    expect(requests[0]?.method).toBe('GET');
    expect(requests[0]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/wait');
    expect(requests[0]?.parsedUrl.searchParams.getAll('status')).toEqual(['running', 'failed']);
    expect(requests[0]?.parsedUrl.searchParams.get('timeout')).toBe('30');
  });

  it('automatically loops in <=60s chunks when timeoutSeconds > 60 until target status is reached', async () => {
    const { fetch, requests } = createMockFetch((_req, idx) => {
      if (idx < 2) {
        return errorResponse(408, 'wait_timeout', 'Still creating', {
          retryable: true,
          details: { workspace: { ...sampleWorkspace, status: 'creating' } },
        });
      }
      return jsonResponse({ ...sampleWorkspace, status: 'running' });
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 3 });

    const ws = await client.workspaces.wait('wks_123', {
      status: ['running', 'failed'],
      timeoutSeconds: 150,
    });

    expect(ws.status).toBe('running');
    expect(requests).toHaveLength(3);
    expect(requests[0]?.parsedUrl.searchParams.get('timeout')).toBe('60');
    expect(requests[1]?.parsedUrl.searchParams.get('timeout')).toBe('60');
    expect(requests[2]?.parsedUrl.searchParams.get('timeout')).toBe('30');
  });

  it('throws WaitTimeoutError with details once total timeoutSeconds budget is exhausted', async () => {
    const { fetch, requests } = createMockFetch(() =>
      errorResponse(408, 'wait_timeout', 'Timed out waiting', {
        retryable: true,
        details: { volume: { ...sampleVolume, status: 'creating' } },
      }),
    );

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 3 });

    let caught: unknown;
    try {
      await client.volumes.wait('vol_123', { status: 'available', timeoutSeconds: 125 });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(WaitTimeoutError);
    const waitErr = caught as WaitTimeoutError;
    expect(waitErr.status).toBe(408);
    expect(waitErr.code).toBe('wait_timeout');
    expect(waitErr.details).toMatchObject({ volume: { id: 'vol_123', status: 'creating' } });

    // 60 + 60 + 5 = 125s across 3 chunks
    expect(requests).toHaveLength(3);
    expect(requests.map((r) => r.parsedUrl.searchParams.get('timeout'))).toEqual(['60', '60', '5']);
  });

  it('immediately stops and throws UnexpectedStatusError on 409 unexpected_status', async () => {
    const { fetch, requests } = createMockFetch(() =>
      errorResponse(409, 'unexpected_status', 'Workspace transitioned to failed', {
        retryable: false,
        details: { workspace: { ...sampleWorkspace, status: 'failed' } },
      }),
    );

    const client = new Kibotos({ apiKey: 'kb_test', fetch, maxRetries: 3 });

    await expect(
      client.workspaces.wait('wks_123', { status: 'running', timeoutSeconds: 180 }),
    ).rejects.toBeInstanceOf(UnexpectedStatusError);
    expect(requests).toHaveLength(1);
  });

  it('respects AbortSignal across multi-chunk wait()', async () => {
    const controller = new AbortController();

    const { fetch, requests } = createMockFetch((_req, idx) => {
      if (idx === 0) {
        controller.abort(new Error('stop-waiting'));
        return errorResponse(408, 'wait_timeout', 'Chunk 1 timed out', { retryable: true });
      }
      return jsonResponse(sampleWorkspace);
    });

    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    await expect(
      client.workspaces.wait(
        'wks_123',
        { status: 'running', timeoutSeconds: 120 },
        { signal: controller.signal },
      ),
    ).rejects.toThrow('stop-waiting');

    expect(requests).toHaveLength(1);
  });

  it('validates status and timeoutSeconds arguments', async () => {
    const { fetch } = createMockFetch(() => jsonResponse(sampleWorkspace));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    await expect(
      client.workspaces.wait('wks_123', { status: [] }),
    ).rejects.toBeInstanceOf(KibotosError);

    await expect(
      client.workspaces.wait('wks_123', { status: 'running', timeoutSeconds: 0 }),
    ).rejects.toBeInstanceOf(KibotosError);
  });
});
