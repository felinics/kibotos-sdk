import type { HttpClient } from '../http';
import { buildQueryParams } from '../pagination';
import type {
  CreateEgressRequest,
  Egress,
  ListEgressesParams,
  MultiZonePage,
  ReplaceEgressRequest,
  RequestOptions,
  UpdateEgressRequest,
} from '../types';

export class EgressesResource {
  constructor(private readonly http: HttpClient) {}

  create(body: CreateEgressRequest, opts?: RequestOptions): Promise<Egress> {
    return this.http.request({ method: 'POST', path: '/egresses', body, opts });
  }

  list(params?: ListEgressesParams, opts?: RequestOptions): Promise<MultiZonePage<Egress>> {
    return this.http.request({
      method: 'GET',
      path: '/egresses',
      query: buildQueryParams({ ...params }),
      opts,
    });
  }

  get(id: string, opts?: RequestOptions): Promise<Egress> {
    return this.http.request({ method: 'GET', path: `/egresses/${encodeURIComponent(id)}`, opts });
  }

  replace(id: string, body: ReplaceEgressRequest, opts?: RequestOptions): Promise<Egress> {
    return this.http.request({ method: 'PUT', path: `/egresses/${encodeURIComponent(id)}`, body, opts });
  }

  update(id: string, body: UpdateEgressRequest, opts?: RequestOptions): Promise<Egress> {
    return this.http.request({ method: 'PATCH', path: `/egresses/${encodeURIComponent(id)}`, body, opts });
  }

  delete(id: string, opts?: RequestOptions): Promise<void> {
    return this.http.request({ method: 'DELETE', path: `/egresses/${encodeURIComponent(id)}`, opts });
  }
}
