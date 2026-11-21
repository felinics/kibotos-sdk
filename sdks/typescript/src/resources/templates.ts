import type { HttpClient } from '../http.js';
import { buildQueryParams, paginate } from '../pagination.js';
import type {
  ListTemplateRevisionsParams,
  ListTemplatesParams,
  MultiZonePage,
  Page,
  RequestOptions,
  Template,
  TemplateRevision,
} from '../types.js';

export class TemplatesResource {
  constructor(private readonly http: HttpClient) {}

  /**
   * Lists templates across zones.
   * `GET /v1/templates`
   */
  list(params?: ListTemplatesParams, opts?: RequestOptions): Promise<MultiZonePage<Template>> {
    return this.http.request<MultiZonePage<Template>>({
      method: 'GET',
      path: '/templates',
      query: buildQueryParams({
        owner: params?.owner,
        zone: params?.zone,
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Auto-paginates across all templates matching `params`.
   */
  iterate(params?: ListTemplatesParams, opts?: RequestOptions): AsyncIterable<Template> {
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
   * Retrieves a template by ID.
   * `GET /v1/templates/{id}`
   */
  get(id: string, opts?: RequestOptions): Promise<Template> {
    return this.http.request<Template>({
      method: 'GET',
      path: `/templates/${encodeURIComponent(id)}`,
      opts,
    });
  }

  /**
   * Lists revisions for a template.
   * `GET /v1/templates/{id}/revisions`
   */
  listRevisions(
    id: string,
    params?: ListTemplateRevisionsParams,
    opts?: RequestOptions,
  ): Promise<Page<TemplateRevision>> {
    return this.http.request<Page<TemplateRevision>>({
      method: 'GET',
      path: `/templates/${encodeURIComponent(id)}/revisions`,
      query: buildQueryParams({
        limit: params?.limit,
        page_token: params?.page_token,
      }),
      opts,
    });
  }

  /**
   * Retrieves a specific template revision.
   * `GET /v1/templates/{id}/revisions/{revision_id}`
   */
  getRevision(id: string, revisionId: string, opts?: RequestOptions): Promise<TemplateRevision> {
    return this.http.request<TemplateRevision>({
      method: 'GET',
      path: `/templates/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}`,
      opts,
    });
  }
}
