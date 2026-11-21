import type { Exposure, Template, TemplateRevision, Volume, Workspace, Zone } from '../src/types.js';

export interface RecordedRequest {
  url: string;
  parsedUrl: URL;
  method: string;
  headers: Headers;
  body: unknown;
  rawBody: string | undefined;
  signal: AbortSignal | null | undefined;
}

export function jsonResponse(
  body: unknown,
  init: { status?: number; statusText?: string; headers?: Record<string, string> } = {},
): Response {
  const headers = new Headers({
    'Content-Type': 'application/json',
    'Request-Id': 'req_test_123',
    ...(init.headers ?? {}),
  });
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    statusText: init.statusText,
    headers,
  });
}

export function errorResponse(
  status: number,
  code: string,
  message: string,
  options: {
    retryable?: boolean;
    requestId?: string;
    details?: Record<string, unknown>;
    headers?: Record<string, string>;
  } = {},
): Response {
  const requestId = options.requestId ?? 'req_err_123';
  return jsonResponse(
    {
      error: {
        code,
        message,
        status,
        request_id: requestId,
        retryable: options.retryable ?? false,
        ...(options.details !== undefined ? { details: options.details } : {}),
      },
    },
    {
      status,
      headers: {
        'Request-Id': requestId,
        ...(options.headers ?? {}),
      },
    },
  );
}

export function createMockFetch(
  handler: (req: RecordedRequest, index: number) => Response | Promise<Response>,
): {
  fetch: typeof globalThis.fetch;
  requests: RecordedRequest[];
} {
  const requests: RecordedRequest[] = [];

  const mockFetch: typeof globalThis.fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    const rawBody = typeof init?.body === 'string' ? init.body : undefined;
    let body: unknown;
    if (rawBody !== undefined && rawBody.length > 0) {
      try {
        body = JSON.parse(rawBody);
      } catch {
        body = rawBody;
      }
    }

    const record: RecordedRequest = {
      url,
      parsedUrl: new URL(url),
      method,
      headers,
      body,
      rawBody,
      signal: init?.signal,
    };
    const index = requests.length;
    requests.push(record);
    return handler(record, index);
  };

  return { fetch: mockFetch, requests };
}

export const sampleZone: Zone = {
  id: 'global-1',
  display_name: 'Global 1',
  region: 'global',
  status: 'available',
  default: true,
};

export const sampleTemplate: Template = {
  id: 'python-3.12',
  owner: 'platform',
  display_name: 'Python 3.12',
  description: 'Python 3.12 Linux environment',
  default_revision: 'r42',
  latest_revision: null,
  zones: ['global-1'],
  created_at: null,
  updated_at: null,
};

export const sampleTemplateRevision: TemplateRevision = {
  id: 'r42',
  template_id: 'python-3.12',
  status: 'ready',
  failure: null,
  source: null,
  process: null,
  size_mib: null,
  zones: ['global-1'],
  created_at: null,
  ready_at: null,
};

export const sampleWorkspace: Workspace = {
  id: 'wks_123',
  name: 'agent-runner',
  zone: 'global-1',
  template: { id: 'python-3.12', revision: 'r42' },
  status: 'running',
  failure: null,
  resources: { vcpus: 2, memory_mib: 4096, disk_mib: 20480 },
  network: { mode: 'public_internet', applied: true },
  volume: { id: 'vol_123', mount_path: '/data', delete_with_workspace: false },
  timeout: { expires_at: '2026-11-21T13:00:00Z', action: 'stop' },
  labels: { team: 'ml' },
  created_at: '2026-11-21T12:00:00Z',
  updated_at: '2026-11-21T12:00:05Z',
};

export const sampleExposure: Exposure = {
  id: 'exp_123',
  workspace_id: 'wks_123',
  port: 8080,
  url: 'https://k7x2n4p6r8t0w2y6.kibo.run',
  status: 'active',
  expires_at: '2026-11-21T13:00:00Z',
  revoked_at: null,
  created_at: '2026-11-21T12:00:00Z',
};

export const sampleVolume: Volume = {
  id: 'vol_123',
  name: 'datasets',
  zone: 'global-1',
  size_gib: 50,
  status: 'available',
  failure: null,
  workspace_id: null,
  labels: {},
  created_at: '2026-11-21T12:00:00Z',
  updated_at: '2026-11-21T12:00:00Z',
};
