import { describe, expect, it } from 'vitest';
import { Kibotos } from '../src/index.js';
import {
  createMockFetch,
  jsonResponse,
  sampleExposure,
  sampleTemplate,
  sampleTemplateRevision,
  sampleVolume,
  sampleWorkspace,
  sampleZone,
} from './helpers.js';

describe('Resource namespaces', () => {
  it('zones.list calls GET /v1/zones with pagination params', async () => {
    const { fetch, requests } = createMockFetch(() =>
      jsonResponse({ data: [sampleZone], next_page_token: 'tok_2' }),
    );
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const page = await client.zones.list({ limit: 10, page_token: 'tok_1' });
    expect(page.data).toEqual([sampleZone]);
    expect(page.next_page_token).toBe('tok_2');

    expect(requests[0]?.method).toBe('GET');
    expect(requests[0]?.parsedUrl.pathname).toBe('/v1/zones');
    expect(requests[0]?.parsedUrl.searchParams.get('limit')).toBe('10');
    expect(requests[0]?.parsedUrl.searchParams.get('page_token')).toBe('tok_1');
  });

  it('templates resource covers list, get, listRevisions, and getRevision', async () => {
    const { fetch, requests } = createMockFetch((req) => {
      if (req.parsedUrl.pathname === '/v1/templates') {
        return jsonResponse({
          data: [sampleTemplate],
          unavailable_zones: ['eu-1'],
          next_page_token: null,
        });
      }
      if (req.parsedUrl.pathname === '/v1/templates/python-3.12') {
        return jsonResponse(sampleTemplate);
      }
      if (req.parsedUrl.pathname === '/v1/templates/python-3.12/revisions') {
        return jsonResponse({ data: [sampleTemplateRevision], next_page_token: null });
      }
      if (req.parsedUrl.pathname === '/v1/templates/python-3.12/revisions/r42') {
        return jsonResponse(sampleTemplateRevision);
      }
      throw new Error(`Unexpected path: ${req.parsedUrl.pathname}`);
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    const listPage = await client.templates.list({ owner: 'platform', zone: 'global-1', limit: 25 });
    expect(listPage.data).toEqual([sampleTemplate]);
    expect(listPage.unavailable_zones).toEqual(['eu-1']);
    expect(requests[0]?.parsedUrl.searchParams.get('owner')).toBe('platform');
    expect(requests[0]?.parsedUrl.searchParams.get('zone')).toBe('global-1');
    expect(requests[0]?.parsedUrl.searchParams.get('limit')).toBe('25');

    const tpl = await client.templates.get('python-3.12');
    expect(tpl.id).toBe('python-3.12');

    const revs = await client.templates.listRevisions('python-3.12', { limit: 5 });
    expect(revs.data).toEqual([sampleTemplateRevision]);
    expect(requests[2]?.parsedUrl.searchParams.get('limit')).toBe('5');

    const rev = await client.templates.getRevision('python-3.12', 'r42');
    expect(rev.id).toBe('r42');
  });

  it('workspaces resource covers every CRUD and lifecycle method plus label.<k> query params', async () => {
    const { fetch, requests } = createMockFetch((req) => {
      if (req.method === 'GET' && req.parsedUrl.pathname === '/v1/workspaces') {
        return jsonResponse({
          data: [sampleWorkspace],
          unavailable_zones: [],
          next_page_token: null,
        });
      }
      return jsonResponse(sampleWorkspace, { status: req.method === 'POST' ? 202 : 200 });
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    // create
    await client.workspaces.create({
      name: 'agent-runner',
      zone: 'global-1',
      template: { id: 'python-3.12', revision: 'r42' },
      resources: { vcpus: 2, memory_mib: 4096, disk_mib: 20480 },
      network: { mode: 'public_internet' },
      volume: { id: 'vol_123', mount_path: '/data', delete_with_workspace: false },
      timeout: { seconds: 3600, action: 'stop' },
      labels: { team: 'ml' },
    });
    expect(requests[0]?.method).toBe('POST');
    expect(requests[0]?.parsedUrl.pathname).toBe('/v1/workspaces');
    expect(requests[0]?.headers.get('Idempotency-Key')).toMatch(/^[0-9a-f-]{36}$/i);
    expect(requests[0]?.body).toMatchObject({
      name: 'agent-runner',
      template: { id: 'python-3.12', revision: 'r42' },
    });

    // list with labels & multi-status
    await client.workspaces.list({
      status: ['running', 'starting'],
      zone: 'global-1',
      labels: { team: 'ml', env: 'prod' },
      limit: 50,
    });
    expect(requests[1]?.method).toBe('GET');
    expect(requests[1]?.parsedUrl.searchParams.getAll('status')).toEqual(['running', 'starting']);
    expect(requests[1]?.parsedUrl.searchParams.get('zone')).toBe('global-1');
    expect(requests[1]?.parsedUrl.searchParams.get('label.team')).toBe('ml');
    expect(requests[1]?.parsedUrl.searchParams.get('label.env')).toBe('prod');
    expect(requests[1]?.parsedUrl.searchParams.get('limit')).toBe('50');

    // get
    await client.workspaces.get('wks_123');
    expect(requests[2]?.method).toBe('GET');
    expect(requests[2]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123');

    // update
    await client.workspaces.update('wks_123', {
      name: null,
      labels: { team: 'core', old_key: null },
    });
    expect(requests[3]?.method).toBe('PATCH');
    expect(requests[3]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123');
    expect(requests[3]?.body).toEqual({ name: null, labels: { team: 'core', old_key: null } });

    // start (with optional body)
    await client.workspaces.start('wks_123', { timeout: { seconds: 1800, action: 'stop' } });
    expect(requests[4]?.method).toBe('POST');
    expect(requests[4]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/start');
    expect(requests[4]?.body).toEqual({ timeout: { seconds: 1800, action: 'stop' } });

    // stop (without body)
    await client.workspaces.stop('wks_123');
    expect(requests[5]?.method).toBe('POST');
    expect(requests[5]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/stop');
    expect(requests[5]?.rawBody).toBeUndefined();

    // stop (with force: true)
    await client.workspaces.stop('wks_123', { force: true });
    expect(requests[6]?.body).toEqual({ force: true });

    // setTimeout
    await client.workspaces.setTimeout('wks_123', { seconds: 7200, action: 'stop' });
    expect(requests[7]?.method).toBe('POST');
    expect(requests[7]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/timeout');
    expect(requests[7]?.body).toEqual({ seconds: 7200, action: 'stop' });

    // setNetwork
    await client.workspaces.setNetwork('wks_123', { mode: 'deny_all' });
    expect(requests[8]?.method).toBe('PUT');
    expect(requests[8]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/network');
    expect(requests[8]?.body).toEqual({ mode: 'deny_all' });

    // attachVolume
    await client.workspaces.attachVolume('wks_123', {
      id: 'vol_123',
      mount_path: '/data',
      delete_with_workspace: false,
    });
    expect(requests[9]?.method).toBe('PUT');
    expect(requests[9]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/volume');
    expect(requests[9]?.body).toEqual({
      id: 'vol_123',
      mount_path: '/data',
      delete_with_workspace: false,
    });

    // detachVolume
    await client.workspaces.detachVolume('wks_123');
    expect(requests[10]?.method).toBe('DELETE');
    expect(requests[10]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/volume');

    // delete
    await client.workspaces.delete('wks_123');
    expect(requests[11]?.method).toBe('DELETE');
    expect(requests[11]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123');
  });

  it('volumes resource covers create, list (with attached + labels), get, update, resize, delete', async () => {
    const { fetch, requests } = createMockFetch((req) => {
      if (req.method === 'GET' && req.parsedUrl.pathname === '/v1/volumes') {
        return jsonResponse({
          data: [sampleVolume],
          unavailable_zones: [],
          next_page_token: null,
        });
      }
      return jsonResponse(sampleVolume, { status: req.method === 'POST' ? 202 : 200 });
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    await client.volumes.create({
      name: 'datasets',
      zone: 'global-1',
      size_gib: 50,
      labels: { tier: 'fast' },
    });
    expect(requests[0]?.method).toBe('POST');
    expect(requests[0]?.parsedUrl.pathname).toBe('/v1/volumes');
    expect(requests[0]?.body).toEqual({
      name: 'datasets',
      zone: 'global-1',
      size_gib: 50,
      labels: { tier: 'fast' },
    });

    await client.volumes.list({
      zone: 'global-1',
      attached: false,
      labels: { tier: 'fast', project: 'alpha' },
      limit: 20,
    });
    expect(requests[1]?.method).toBe('GET');
    expect(requests[1]?.parsedUrl.searchParams.get('zone')).toBe('global-1');
    expect(requests[1]?.parsedUrl.searchParams.get('attached')).toBe('false');
    expect(requests[1]?.parsedUrl.searchParams.get('label.tier')).toBe('fast');
    expect(requests[1]?.parsedUrl.searchParams.get('label.project')).toBe('alpha');

    await client.volumes.get('vol_123');
    expect(requests[2]?.method).toBe('GET');
    expect(requests[2]?.parsedUrl.pathname).toBe('/v1/volumes/vol_123');

    await client.volumes.update('vol_123', { name: 'datasets-v2', labels: { old: null } });
    expect(requests[3]?.method).toBe('PATCH');
    expect(requests[3]?.body).toEqual({ name: 'datasets-v2', labels: { old: null } });

    await client.volumes.resize('vol_123', { size_gib: 100 });
    expect(requests[4]?.method).toBe('POST');
    expect(requests[4]?.parsedUrl.pathname).toBe('/v1/volumes/vol_123/resize');
    expect(requests[4]?.body).toEqual({ size_gib: 100 });
    expect(requests[4]?.headers.get('Idempotency-Key')).toBeTruthy();

    await client.volumes.delete('vol_123');
    expect(requests[5]?.method).toBe('DELETE');
    expect(requests[5]?.parsedUrl.pathname).toBe('/v1/volumes/vol_123');
  });

  it('exposures resource covers create, list, get, and revoke with URL encoding', async () => {
    const { fetch, requests } = createMockFetch((req) => {
      if (req.method === 'GET' && req.parsedUrl.pathname.endsWith('/exposures')) {
        return jsonResponse({ data: [sampleExposure], next_page_token: null });
      }
      return jsonResponse(sampleExposure, { status: req.method === 'POST' ? 201 : 200 });
    });
    const client = new Kibotos({ apiKey: 'kb_test', fetch });

    await client.exposures.create('wks_123', { port: 8080, ttl_seconds: 3600 });
    expect(requests[0]?.method).toBe('POST');
    expect(requests[0]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/exposures');
    expect(requests[0]?.body).toEqual({ port: 8080, ttl_seconds: 3600 });
    expect(requests[0]?.headers.get('Idempotency-Key')).toBeTruthy();

    await client.exposures.list('wks_123', { include_revoked: true, limit: 15 });
    expect(requests[1]?.method).toBe('GET');
    expect(requests[1]?.parsedUrl.searchParams.get('include_revoked')).toBe('true');
    expect(requests[1]?.parsedUrl.searchParams.get('limit')).toBe('15');

    await client.exposures.get('wks_123', 'exp_123');
    expect(requests[2]?.method).toBe('GET');
    expect(requests[2]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/exposures/exp_123');

    await client.exposures.revoke('wks_123', 'exp_123');
    expect(requests[3]?.method).toBe('DELETE');
    expect(requests[3]?.parsedUrl.pathname).toBe('/v1/workspaces/wks_123/exposures/exp_123');
  });
});
