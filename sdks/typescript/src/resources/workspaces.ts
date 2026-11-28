import type { HttpClient } from '../http';
import { buildQueryParams, paginate } from '../pagination';
import { parseExecEvents } from '../sse';
import type {
  AttachVolumeRequest,
  CreateWorkspaceRequest,
  ExecEvent,
  ExecRequest,
  ExecResponse,
  FileContent,
  FileEntry,
  FileList,
  FileOptions,
  FileWriteResult,
  ListFilesOptions,
  ListWorkspacesParams,
  MakeDirectoryRequest,
  MoveFileRequest,
  MultiZonePage,
  ReadFileOptions,
  RequestOptions,
  SetWorkspaceNetworkRequest,
  SetWorkspacePacksRequest,
  SetWorkspaceTimeoutRequest,
  SetWorkspaceVolumesRequest,
  StartWorkspaceRequest,
  StopWorkspaceRequest,
  UpdateWorkspaceRequest,
  WaitWorkspaceParams,
  Workspace,
  WorkspaceMetrics,
  WorkspaceMetricsParams,
  WorkspaceStatus,
  WriteFileOptions,
} from '../types';
import { executeWaitLoop } from '../wait';
import {
  WorkspaceSocket,
  openWebSocket,
  webSocketUrl,
  type TunnelTarget,
  type WebSocketConnectOptions,
} from '../websocket';

function workspacePath(id: string): string {
  return `/workspaces/${encodeURIComponent(id)}`;
}

export class WorkspacesResource {
  constructor(private readonly http: HttpClient) {}

  /**
   * Creates a new workspace asynchronously (`202 Accepted`).
   * `POST /v1/workspaces`
   */
  create(body: CreateWorkspaceRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'POST',
      path: '/workspaces',
      body,
      opts,
    });
  }

  /**
   * Lists workspaces across zones.
   * `GET /v1/workspaces`
   */
  list(params?: ListWorkspacesParams, opts?: RequestOptions): Promise<MultiZonePage<Workspace>> {
    return this.http.request<MultiZonePage<Workspace>>({
      method: 'GET',
      path: '/workspaces',
      query: buildQueryParams({
        status: params?.status,
        zone: params?.zone,
        labels: params?.labels,
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Auto-paginates across all workspaces matching `params`.
   */
  iterate(params?: ListWorkspacesParams, opts?: RequestOptions): AsyncIterable<Workspace> {
    return paginate(
      (pageToken) =>
        this.list(
          {
            ...params,
            page_token: pageToken,
          },
          opts,
        ),
      params?.page_token,
    );
  }

  /**
   * Retrieves a workspace by ID.
   * `GET /v1/workspaces/{id}`
   */
  get(id: string, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'GET',
      path: `/workspaces/${encodeURIComponent(id)}`,
      opts,
    });
  }

  /**
   * Updates workspace metadata and resizes eligible stopped or failed workspaces.
   * `PATCH /v1/workspaces/{id}`
   */
  update(id: string, body: UpdateWorkspaceRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'PATCH',
      path: `/workspaces/${encodeURIComponent(id)}`,
      body,
      opts,
    });
  }

  /**
   * Deletes a workspace asynchronously (`202 Accepted`).
   * `DELETE /v1/workspaces/{id}`
   */
  delete(id: string, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'DELETE',
      path: `/workspaces/${encodeURIComponent(id)}`,
      opts,
    });
  }

  /**
   * Starts a stopped workspace (`202 Accepted`).
   * `POST /v1/workspaces/{id}/start`
   */
  start(id: string, body?: StartWorkspaceRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'POST',
      path: `/workspaces/${encodeURIComponent(id)}/start`,
      body,
      opts,
    });
  }

  /**
   * Stops an available workspace (`202 Accepted`).
   * `POST /v1/workspaces/{id}/stop`
   */
  stop(id: string, body?: StopWorkspaceRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'POST',
      path: `/workspaces/${encodeURIComponent(id)}/stop`,
      body,
      opts,
    });
  }

  /**
   * Updates the auto-timeout policy for a workspace.
   * `POST /v1/workspaces/{id}/timeout`
   */
  setTimeout(id: string, body: SetWorkspaceTimeoutRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'POST',
      path: `/workspaces/${encodeURIComponent(id)}/timeout`,
      body,
      opts,
    });
  }

  /**
   * Replaces the outbound network policy for a workspace.
   * `PUT /v1/workspaces/{id}/network`
   */
  setNetwork(id: string, body: SetWorkspaceNetworkRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'PUT',
      path: `/workspaces/${encodeURIComponent(id)}/network`,
      body,
      opts,
    });
  }

  setVolumes(id: string, body: SetWorkspaceVolumesRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request({ method: 'PUT', path: `${workspacePath(id)}/volumes`, body, opts });
  }

  setPacks(id: string, body: SetWorkspacePacksRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request({ method: 'PUT', path: `${workspacePath(id)}/packs`, body, opts });
  }

  getMetrics(id: string, params?: WorkspaceMetricsParams, opts?: RequestOptions): Promise<WorkspaceMetrics> {
    return this.http.request({
      method: 'GET',
      path: `${workspacePath(id)}/metrics`,
      query: buildQueryParams({
        start: params?.start instanceof Date ? params.start.toISOString() : params?.start,
        end: params?.end instanceof Date ? params.end.toISOString() : params?.end,
      }),
      opts,
    });
  }

  exec(id: string, body: ExecRequest, opts?: RequestOptions): Promise<ExecResponse> {
    return this.http.request({
      method: 'POST', path: `${workspacePath(id)}/exec`, body,
      opts: { ...opts, timeoutMs: opts?.timeoutMs ?? ((body.timeout_seconds ?? 30) + 15) * 1000 },
      accept: 'application/json', nonReplayable: true, disableIdempotency: true,
    });
  }

  async *execStream(id: string, body: ExecRequest, opts?: RequestOptions): AsyncGenerator<ExecEvent> {
    const response = await this.http.requestResponse({
      method: 'POST', path: `${workspacePath(id)}/exec`, body,
      opts: { ...opts, timeoutMs: opts?.timeoutMs ?? 0 },
      accept: 'text/event-stream', nonReplayable: true, disableIdempotency: true,
    });
    yield* parseExecEvents(response);
  }

  listFiles(id: string, path: string, opts?: ListFilesOptions): Promise<FileList> {
    return this.http.request({
      method: 'GET', path: `${workspacePath(id)}/files`,
      query: buildQueryParams({ path, depth: opts?.depth, user: opts?.user }), opts,
    });
  }

  statFile(id: string, path: string, opts?: FileOptions): Promise<FileEntry> {
    return this.http.request({
      method: 'GET', path: `${workspacePath(id)}/files/stat`,
      query: buildQueryParams({ path, user: opts?.user }), opts,
    });
  }

  readFile(id: string, path: string, opts?: ReadFileOptions & { as?: 'response' }): Promise<Response>;
  readFile(id: string, path: string, opts: ReadFileOptions & { as: 'bytes' }): Promise<Uint8Array>;
  readFile(id: string, path: string, opts: ReadFileOptions & { as: 'stream' }): Promise<ReadableStream<Uint8Array> | null>;
  readFile(id: string, path: string, opts: ReadFileOptions): Promise<Response | Uint8Array | ReadableStream<Uint8Array> | null>;
  async readFile(id: string, path: string, opts?: ReadFileOptions): Promise<Response | Uint8Array | ReadableStream<Uint8Array> | null> {
    const headers = new Headers(opts?.headers);
    if (opts?.range !== undefined) headers.set('Range', opts.range);
    if (opts?.ifNoneMatch !== undefined) headers.set('If-None-Match', opts.ifNoneMatch);
    const response = await this.http.requestResponse({
      method: 'GET', path: `${workspacePath(id)}/files/content`,
      query: buildQueryParams({ path, user: opts?.user }),
      opts: { ...opts, headers: Object.fromEntries(headers), timeoutMs: opts?.timeoutMs ?? 0 },
      accept: 'application/octet-stream', successStatuses: [304],
    });
    if (opts?.as === 'bytes') return new Uint8Array(await response.arrayBuffer());
    if (opts?.as === 'stream') return response.body;
    return response;
  }

  writeFile(id: string, path: string, content: FileContent, opts?: WriteFileOptions): Promise<FileWriteResult> {
    const body = content instanceof Uint8Array
      ? content.buffer instanceof ArrayBuffer
        ? new Uint8Array(content.buffer, content.byteOffset, content.byteLength)
        : new Uint8Array(content)
      : content;
    return this.http.request({
      method: 'PUT', path: `${workspacePath(id)}/files/content`,
      query: buildQueryParams({ path, mode: opts?.mode, create_only: opts?.createOnly, user: opts?.user }),
      opts: { ...opts, timeoutMs: opts?.timeoutMs ?? 0 },
      rawBody: body,
      duplex: body instanceof ReadableStream ? 'half' : undefined,
      nonReplayable: true,
    });
  }

  makeDir(id: string, body: MakeDirectoryRequest, opts?: RequestOptions): Promise<FileEntry> {
    return this.http.request({
      method: 'POST', path: `${workspacePath(id)}/files/mkdir`, body, opts,
      nonReplayable: true, disableIdempotency: true,
    });
  }

  moveFile(id: string, body: MoveFileRequest, opts?: RequestOptions): Promise<FileEntry> {
    return this.http.request({
      method: 'POST', path: `${workspacePath(id)}/files/move`, body, opts,
      nonReplayable: true, disableIdempotency: true,
    });
  }

  deleteFile(id: string, path: string, opts?: FileOptions): Promise<void> {
    return this.http.request({
      method: 'DELETE', path: `${workspacePath(id)}/files`,
      query: buildQueryParams({ path, user: opts?.user }), opts,
      nonReplayable: true, disableIdempotency: true,
    });
  }

  wsUrl(id: string): string {
    return webSocketUrl(this.http.buildUrl(`${workspacePath(id)}/ws`));
  }

  tunnelWSUrl(id: string, target: TunnelTarget): string {
    const url = this.http.buildUrl(`${workspacePath(id)}/tunnels/ws`, buildQueryParams({ ...target }));
    return webSocketUrl(url);
  }

  desktopVNCUrl(id: string): string {
    return webSocketUrl(this.http.buildUrl(`${workspacePath(id)}/desktop/vnc`));
  }

  proxyUrl(id: string, port: number, path = '', query?: URLSearchParams): string {
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new RangeError('port must be between 1 and 65535');
    }
    const segments = path.replace(/^\//, '').split('/');
    if (segments.some((segment) => segment === '.' || segment === '..')) {
      throw new TypeError('Proxy path must not contain dot segments');
    }
    const suffix = path ? `/${segments.map(encodeURIComponent).join('/')}` : '';
    return this.http.buildUrl(`${workspacePath(id)}/proxy/${port}${suffix}`, query);
  }

  proxyWSUrl(id: string, port: number, path?: string, query?: URLSearchParams): string {
    return webSocketUrl(this.proxyUrl(id, port, path, query));
  }

  /** Headers for a WebSocket upgrade or an authenticated proxy request. */
  wsHeaders(overrides?: Record<string, string>): Headers {
    return this.http.buildHeaders(overrides);
  }

  connectWS(id: string, opts?: WebSocketConnectOptions): WorkspaceSocket {
    return new WorkspaceSocket(openWebSocket(this.wsUrl(id), this.wsHeaders(opts?.headers), opts));
  }

  connectTunnel(id: string, target: TunnelTarget, opts?: WebSocketConnectOptions): WebSocket {
    return openWebSocket(this.tunnelWSUrl(id, target), this.wsHeaders(opts?.headers), opts);
  }

  connectDesktop(id: string, opts?: WebSocketConnectOptions): WebSocket {
    return openWebSocket(this.desktopVNCUrl(id), this.wsHeaders(opts?.headers), opts);
  }

  connectProxy(
    id: string,
    port: number,
    opts?: WebSocketConnectOptions & { path?: string; query?: URLSearchParams },
  ): WebSocket {
    const url = this.proxyWSUrl(id, port, opts?.path, opts?.query);
    return openWebSocket(url, this.wsHeaders(opts?.headers), opts);
  }

  /**
   * Attaches a volume to a workspace.
   * `PUT /v1/workspaces/{id}/volume`
   */
  attachVolume(id: string, body: AttachVolumeRequest, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'PUT',
      path: `/workspaces/${encodeURIComponent(id)}/volume`,
      body,
      opts,
    });
  }

  /**
   * Detaches the attached volume from a workspace.
   * `DELETE /v1/workspaces/{id}/volume`
   */
  detachVolume(id: string, opts?: RequestOptions): Promise<Workspace> {
    return this.http.request<Workspace>({
      method: 'DELETE',
      path: `/workspaces/${encodeURIComponent(id)}/volume`,
      opts,
    });
  }

  /**
   * Waits until the workspace reaches one of the target statuses.
   * Automatically chunks calls into `<= 60s` windows when `timeoutSeconds > 60`.
   * `GET /v1/workspaces/{id}/wait`
   */
  wait(id: string, params: WaitWorkspaceParams, opts?: RequestOptions): Promise<Workspace> {
    return executeWaitLoop<Workspace, WorkspaceStatus>(this.http, {
      path: `/workspaces/${encodeURIComponent(id)}/wait`,
      status: params.status,
      timeoutSeconds: params.timeoutSeconds,
      opts,
    });
  }
}
