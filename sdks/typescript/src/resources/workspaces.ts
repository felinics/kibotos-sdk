import type { HttpClient } from '../http.js';
import { buildQueryParams, paginate } from '../pagination.js';
import type {
  AttachVolumeRequest,
  CreateWorkspaceRequest,
  ListWorkspacesParams,
  MultiZonePage,
  RequestOptions,
  SetWorkspaceNetworkRequest,
  SetWorkspaceTimeoutRequest,
  StartWorkspaceRequest,
  StopWorkspaceRequest,
  UpdateWorkspaceRequest,
  WaitWorkspaceParams,
  Workspace,
  WorkspaceStatus,
} from '../types.js';
import { executeWaitLoop } from '../wait.js';

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
   * Updates workspace metadata (`name` and/or `labels`).
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
   * Sets the network mode for a workspace.
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
