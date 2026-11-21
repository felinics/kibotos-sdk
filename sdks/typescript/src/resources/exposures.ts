import type { HttpClient } from '../http.js';
import { buildQueryParams, paginate } from '../pagination.js';
import type {
  CreateExposureRequest,
  Exposure,
  ListExposuresParams,
  Page,
  RequestOptions,
} from '../types.js';

export class ExposuresResource {
  constructor(private readonly http: HttpClient) {}

  /**
   * Creates a public HTTPS exposure for a workspace port (`201 Created`).
   * `POST /v1/workspaces/{id}/exposures`
   */
  create(
    workspaceId: string,
    body: CreateExposureRequest,
    opts?: RequestOptions,
  ): Promise<Exposure> {
    return this.http.request<Exposure>({
      method: 'POST',
      path: `/workspaces/${encodeURIComponent(workspaceId)}/exposures`,
      body,
      opts,
    });
  }

  /**
   * Lists exposures for a workspace.
   * `GET /v1/workspaces/{id}/exposures`
   */
  list(
    workspaceId: string,
    params?: ListExposuresParams,
    opts?: RequestOptions,
  ): Promise<Page<Exposure>> {
    return this.http.request<Page<Exposure>>({
      method: 'GET',
      path: `/workspaces/${encodeURIComponent(workspaceId)}/exposures`,
      query: buildQueryParams({
        include_revoked: params?.include_revoked,
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Auto-paginates across all exposures for a workspace.
   */
  iterate(
    workspaceId: string,
    params?: ListExposuresParams,
    opts?: RequestOptions,
  ): AsyncIterable<Exposure> {
    return paginate(
      (pageToken) =>
        this.list(
          workspaceId,
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
   * Retrieves a specific exposure by ID.
   * `GET /v1/workspaces/{id}/exposures/{exposure_id}`
   */
  get(workspaceId: string, exposureId: string, opts?: RequestOptions): Promise<Exposure> {
    return this.http.request<Exposure>({
      method: 'GET',
      path: `/workspaces/${encodeURIComponent(workspaceId)}/exposures/${encodeURIComponent(exposureId)}`,
      opts,
    });
  }

  /**
   * Revokes an active exposure.
   * `DELETE /v1/workspaces/{id}/exposures/{exposure_id}`
   */
  revoke(workspaceId: string, exposureId: string, opts?: RequestOptions): Promise<Exposure> {
    return this.http.request<Exposure>({
      method: 'DELETE',
      path: `/workspaces/${encodeURIComponent(workspaceId)}/exposures/${encodeURIComponent(exposureId)}`,
      opts,
    });
  }
}
