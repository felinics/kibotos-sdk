import { describe, expect, it } from 'vitest';
import { Kibotos } from '../src/index.js';
import {
  createMockFetch,
  jsonResponse,
  sampleExposure,
  sampleTemplate,
  sampleVolume,
  sampleWorkspace,
  sampleZone,
} from './helpers.js';

describe('AsyncIterable auto-pagination (iterate)', () => {
  it('iterates seamlessly across multiple pages for workspaces and preserves filters', async () => {
    const pages = [
      {
        data: [
          { ...sampleWorkspace, id: 'wks_1' },
          { ...sampleWorkspace, id: 'wks_2' },
        ],
        unavailable_zones: [],
        next_page_token: 'cursor_2',
      },
      {
        data: [{ ...sampleWorkspace, id: 'wks_3' }],
        unavailable_zones: [],
        next_page_token: 'cursor_3',
      },
      {
        data: [],
        unavailable_zones: [],
        next_page_token: null,
      },
    ];

    const { fetch, requests } = createMockFetch((_req, idx) => jsonResponse(pages[idx]));
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const ids: string[] = [];
    for await (const ws of client.workspaces.iterate({
      zone: 'global-1',
      labels: { env: 'prod' },
      limit: 2,
    })) {
      ids.push(ws.id);
    }

    expect(ids).toEqual(['wks_1', 'wks_2', 'wks_3']);
    expect(requests).toHaveLength(3);
    expect(requests[0]?.parsedUrl.searchParams.get('page_token')).toBeNull();
    expect(requests[0]?.parsedUrl.searchParams.get('label.env')).toBe('prod');
    expect(requests[1]?.parsedUrl.searchParams.get('page_token')).toBe('cursor_2');
    expect(requests[1]?.parsedUrl.searchParams.get('label.env')).toBe('prod');
    expect(requests[2]?.parsedUrl.searchParams.get('page_token')).toBe('cursor_3');
  });

  it('fetches lazily and stops immediately when the consumer breaks early', async () => {
    const { fetch, requests } = createMockFetch((_req, idx) =>
      jsonResponse({
        data: [{ ...sampleZone, id: `zone-${idx}` }],
        next_page_token: `cursor_${idx + 1}`,
      }),
    );
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const collected: string[] = [];
    for await (const zone of client.zones.iterate({ limit: 1 })) {
      collected.push(zone.id);
      if (collected.length === 2) {
        break;
      }
    }

    expect(collected).toEqual(['zone-0', 'zone-1']);
    expect(requests).toHaveLength(2);
  });

  it('guards against repeated next_page_token cycles', async () => {
    const { fetch, requests } = createMockFetch(() =>
      jsonResponse({
        data: [sampleTemplate],
        unavailable_zones: [],
        next_page_token: 'stuck_token',
      }),
    );
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const items = [];
    for await (const tpl of client.templates.iterate()) {
      items.push(tpl.id);
    }

    expect(items).toEqual(['python-3.12', 'python-3.12']);
    expect(requests).toHaveLength(2);
  });

  it('supports iterate() on volumes and exposures', async () => {
    const { fetch } = createMockFetch((req) => {
      if (req.parsedUrl.pathname === '/v1/volumes') {
        const tok = req.parsedUrl.searchParams.get('page_token');
        if (!tok) {
          return jsonResponse({
            data: [{ ...sampleVolume, id: 'vol_1' }],
            unavailable_zones: [],
            next_page_token: 'v2',
          });
        }
        return jsonResponse({
          data: [{ ...sampleVolume, id: 'vol_2' }],
          unavailable_zones: [],
          next_page_token: null,
        });
      }
      if (req.parsedUrl.pathname === '/v1/workspaces/wks_123/exposures') {
        const tok = req.parsedUrl.searchParams.get('page_token');
        if (!tok) {
          return jsonResponse({
            data: [{ ...sampleExposure, id: 'exp_1' }],
            next_page_token: 'e2',
          });
        }
        return jsonResponse({
          data: [{ ...sampleExposure, id: 'exp_2' }],
          next_page_token: null,
        });
      }
      throw new Error(`Unexpected path: ${req.parsedUrl.pathname}`);
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const volIds: string[] = [];
    for await (const v of client.volumes.iterate({ attached: true })) {
      volIds.push(v.id);
    }
    expect(volIds).toEqual(['vol_1', 'vol_2']);

    const expIds: string[] = [];
    for await (const e of client.exposures.iterate('wks_123', { include_revoked: true })) {
      expIds.push(e.id);
    }
    expect(expIds).toEqual(['exp_1', 'exp_2']);
  });
});
