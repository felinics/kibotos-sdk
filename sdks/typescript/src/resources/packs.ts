import type { HttpClient } from '../http';
import { buildQueryParams } from '../pagination';
import type { ListPacksParams, MultiZonePage, Pack, PackRevision, RequestOptions } from '../types';

export class PacksResource {
  constructor(private readonly http: HttpClient) {}

  list(params?: ListPacksParams, opts?: RequestOptions): Promise<MultiZonePage<Pack>> {
    return this.http.request({ method: 'GET', path: '/packs', query: buildQueryParams({ ...params }), opts });
  }

  get(id: string, opts?: RequestOptions): Promise<Pack> {
    return this.http.request({ method: 'GET', path: `/packs/${encodeURIComponent(id)}`, opts });
  }

  getRevision(id: string, revisionId: string, opts?: RequestOptions): Promise<PackRevision> {
    return this.http.request({
      method: 'GET',
      path: `/packs/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}`,
      opts,
    });
  }
}
