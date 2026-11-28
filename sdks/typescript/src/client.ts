import { HttpClient } from './http';
import {
  EgressesResource,
  PacksResource,
  ResolversResource,
  ExposuresResource,
  TemplatesResource,
  VolumesResource,
  WorkspacesResource,
  ZonesResource,
} from './resources/index';
import type { KibotosClientOptions } from './types';

/**
 * Official Kibotos v1 API client.
 *
 * @example
 * ```ts
 * import { Kibotos } from '@kibotos/sdk';
 *
 * const client = new Kibotos();
 * const ws = await client.workspaces.create({ template: { id: 'python-3.12' } });
 * await client.workspaces.wait(ws.id, { status: ['available', 'failed'] });
 * ```
 */
export class Kibotos {
  readonly zones: ZonesResource;
  readonly templates: TemplatesResource;
  readonly workspaces: WorkspacesResource;
  readonly volumes: VolumesResource;
  readonly exposures: ExposuresResource;

  readonly egresses: EgressesResource;

  readonly packs: PacksResource;

  readonly resolvers: ResolversResource;

  private readonly http: HttpClient;

  constructor(options: KibotosClientOptions = {}) {
    this.http = new HttpClient(options);
    this.zones = new ZonesResource(this.http);
    this.templates = new TemplatesResource(this.http);
    this.workspaces = new WorkspacesResource(this.http);
    this.volumes = new VolumesResource(this.http);
    this.egresses = new EgressesResource(this.http);
    this.packs = new PacksResource(this.http);
    this.resolvers = new ResolversResource(this.http);
    this.exposures = new ExposuresResource(this.http);
  }

  /** Normalized base URL (always ending in `/v1`). */
  get baseUrl(): string {
    return this.http.baseUrl;
  }
}
