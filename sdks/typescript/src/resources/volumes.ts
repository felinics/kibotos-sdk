import type { HttpClient } from '../http.js';
import { buildQueryParams, paginate } from '../pagination.js';
import type {
  CreateVolumeRequest,
  ListVolumesParams,
  MultiZonePage,
  RequestOptions,
  ResizeVolumeRequest,
  UpdateVolumeRequest,
  Volume,
  VolumeStatus,
  WaitVolumeParams,
} from '../types.js';
import { executeWaitLoop } from '../wait.js';

export class VolumesResource {
  constructor(private readonly http: HttpClient) {}

  /**
   * Creates a new persistent volume asynchronously (`202 Accepted`).
   * `POST /v1/volumes`
   */
  create(body: CreateVolumeRequest, opts?: RequestOptions): Promise<Volume> {
    return this.http.request<Volume>({
      method: 'POST',
      path: '/volumes',
      body,
      opts,
    });
  }

  /**
   * Lists volumes across zones.
   * `GET /v1/volumes`
   */
  list(params?: ListVolumesParams, opts?: RequestOptions): Promise<MultiZonePage<Volume>> {
    return this.http.request<MultiZonePage<Volume>>({
      method: 'GET',
      path: '/volumes',
      query: buildQueryParams({
        zone: params?.zone,
        attached: params?.attached,
        labels: params?.labels,
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Auto-paginates across all volumes matching `params`.
   */
  iterate(params?: ListVolumesParams, opts?: RequestOptions): AsyncIterable<Volume> {
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
   * Retrieves a volume by ID.
   * `GET /v1/volumes/{id}`
   */
  get(id: string, opts?: RequestOptions): Promise<Volume> {
    return this.http.request<Volume>({
      method: 'GET',
      path: `/volumes/${encodeURIComponent(id)}`,
      opts,
    });
  }

  /**
   * Updates volume metadata (`name` and/or `labels`).
   * `PATCH /v1/volumes/{id}`
   */
  update(id: string, body: UpdateVolumeRequest, opts?: RequestOptions): Promise<Volume> {
    return this.http.request<Volume>({
      method: 'PATCH',
      path: `/volumes/${encodeURIComponent(id)}`,
      body,
      opts,
    });
  }

  /**
   * Resizes a volume asynchronously (`202 Accepted`).
   * `POST /v1/volumes/{id}/resize`
   */
  resize(id: string, body: ResizeVolumeRequest, opts?: RequestOptions): Promise<Volume> {
    return this.http.request<Volume>({
      method: 'POST',
      path: `/volumes/${encodeURIComponent(id)}/resize`,
      body,
      opts,
    });
  }

  /**
   * Deletes a volume asynchronously (`202 Accepted`).
   * `DELETE /v1/volumes/{id}`
   */
  delete(id: string, opts?: RequestOptions): Promise<Volume> {
    return this.http.request<Volume>({
      method: 'DELETE',
      path: `/volumes/${encodeURIComponent(id)}`,
      opts,
    });
  }

  /**
   * Waits until the volume reaches one of the target statuses.
   * Automatically chunks calls into `<= 60s` windows when `timeoutSeconds > 60`.
   * `GET /v1/volumes/{id}/wait`
   */
  wait(id: string, params: WaitVolumeParams, opts?: RequestOptions): Promise<Volume> {
    return executeWaitLoop<Volume, VolumeStatus>(this.http, {
      path: `/volumes/${encodeURIComponent(id)}/wait`,
      status: params.status,
      timeoutSeconds: params.timeoutSeconds,
      opts,
    });
  }
}
