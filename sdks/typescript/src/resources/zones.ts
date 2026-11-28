import type { HttpClient } from '../http';
import { buildQueryParams, paginate } from '../pagination';
import type { ListZonesParams, Page, RequestOptions, Zone } from '../types';

export class ZonesResource {
  constructor(private readonly http: HttpClient) {}

  /**
   * Lists zones available to the authenticated organization.
   * `GET /v1/zones`
   */
  list(params?: ListZonesParams, opts?: RequestOptions): Promise<Page<Zone>> {
    return this.http.request<Page<Zone>>({
      method: 'GET',
      path: '/zones',
      query: buildQueryParams({
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Auto-paginates across all zones matching `params`.
   */
  iterate(params?: ListZonesParams, opts?: RequestOptions): AsyncIterable<Zone> {
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
}
