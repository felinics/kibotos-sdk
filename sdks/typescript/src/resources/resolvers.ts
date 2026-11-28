import type { HttpClient } from '../http';
import { buildQueryParams } from '../pagination';
import type {
  CreateResolverRequest,
  Resolver,
  ListResolversParams,
  MultiZonePage,
  ReplaceResolverRequest,
  RequestOptions,
  UpdateResolverRequest,
} from '../types';

export class ResolversResource {
  constructor(private readonly http: HttpClient) {}

  create(body: CreateResolverRequest, opts?: RequestOptions): Promise<Resolver> {
    return this.http.request({ method: 'POST', path: '/resolvers', body, opts });
  }

  list(params?: ListResolversParams, opts?: RequestOptions): Promise<MultiZonePage<Resolver>> {
    return this.http.request({
      method: 'GET',
      path: '/resolvers',
      query: buildQueryParams({ ...params }),
      opts,
    });
  }

  get(id: string, opts?: RequestOptions): Promise<Resolver> {
    return this.http.request({ method: 'GET', path: `/resolvers/${encodeURIComponent(id)}`, opts });
  }

  replace(id: string, body: ReplaceResolverRequest, opts?: RequestOptions): Promise<Resolver> {
    return this.http.request({ method: 'PUT', path: `/resolvers/${encodeURIComponent(id)}`, body, opts });
  }

  update(id: string, body: UpdateResolverRequest, opts?: RequestOptions): Promise<Resolver> {
    return this.http.request({ method: 'PATCH', path: `/resolvers/${encodeURIComponent(id)}`, body, opts });
  }

  delete(id: string, opts?: RequestOptions): Promise<void> {
    return this.http.request({ method: 'DELETE', path: `/resolvers/${encodeURIComponent(id)}`, opts });
  }
}
