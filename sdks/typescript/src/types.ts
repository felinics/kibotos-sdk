/**
 * Configuration options for initializing a {@link Kibotos} client.
 */
export interface KibotosClientOptions {
  /**
   * API key (`kb_...`).
   * Defaults to `process.env.KIBOTOS_API_KEY` when running in a runtime with `process.env`.
   */
  apiKey?: string;

  /**
   * Base API URL. Trailing slashes are trimmed and `/v1` is normalized automatically.
   * Defaults to `process.env.KIBOTOS_API_URL ?? "https://api.kibo.to/v1"`.
   */
  baseUrl?: string;

  /**
   * Maximum number of retry attempts for retryable API errors, HTTP 429/5xx, and network errors.
   * Defaults to `3`. Set to `0` to disable retries.
   */
  maxRetries?: number;

  /**
   * Default per-request timeout in milliseconds.
   * Defaults to `30000` (30 seconds).
   */
  timeoutMs?: number;

  /**
   * Custom `fetch` implementation (useful for testing or custom runtimes).
   * Defaults to `globalThis.fetch`.
   */
  fetch?: typeof globalThis.fetch;

  /**
   * Additional headers merged into every request.
   */
  defaultHeaders?: Record<string, string>;
}

/**
 * Per-request options supported by all resource methods.
 */
export interface RequestOptions {
  /**
   * Explicit `Idempotency-Key` header value (1..255 characters).
   * For `POST` mutations, a UUID v4 is automatically generated and reused across retries if omitted.
   */
  idempotencyKey?: string;

  /**
   * Optional `AbortSignal` to cancel the request and any retry backoff delay.
   */
  signal?: AbortSignal;

  /**
   * Per-request timeout override in milliseconds.
   */
  timeoutMs?: number;

  /**
   * Additional per-request HTTP headers.
   */
  headers?: Record<string, string>;
}

/**
 * Standard paginated response envelope.
 */
export interface Page<T> {
  data: T[];
  next_page_token: string | null;
}

/**
 * Multi-zone paginated response envelope returned by cross-zone list endpoints.
 */
export interface MultiZonePage<T> extends Page<T> {
  unavailable_zones: string[];
}

/**
 * Common pagination query parameters.
 */
export interface PaginationParams {
  /** Maximum number of items per page (default 20, max 100). */
  limit?: number;
  /** Opaque pagination cursor returned in `next_page_token`. */
  page_token?: string;
}

/**
 * Structured failure metadata on asynchronous resources.
 */
export interface Failure {
  code: string;
  message: string;
  retryable: boolean;
}

/**
 * Structured error body inside the v1 API error envelope.
 */
export interface APIErrorBody {
  code: string;
  message: string;
  status: number;
  request_id?: string;
  retryable?: boolean;
  details?: Record<string, unknown>;
}

/**
 * Standard v1 API error envelope.
 */
export interface ErrorEnvelope {
  error: APIErrorBody;
}

// ---------------------------------------------------------------------------
// Zones
// ---------------------------------------------------------------------------

export type ZoneStatus = 'available' | 'draining';

export interface Zone {
  id: string;
  display_name: string;
  /** Optional human-readable description (up to 200 characters), or `null` when unspecified. */
  description?: string | null;
  region: string;
  /** ISO 3166-1 alpha-2 uppercase country/region code (e.g. `"JP"`, `"US"`), or `null` when unspecified. */
  country: string | null;
  status: ZoneStatus;
  default: boolean;
}

export type ListZonesParams = PaginationParams;

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

export type TemplateOwner = 'platform' | 'organization';

export interface TemplateLatestRevision {
  id: string;
  status: string;
}

export interface Template {
  id: string;
  owner: TemplateOwner;
  display_name: string;
  description: string;
  default_revision: string;
  latest_revision: TemplateLatestRevision | null;
  zones: string[];
  created_at: string | null;
  updated_at: string | null;
}

export interface TemplateRevision {
  id: string;
  template_id: string;
  status: string;
  failure: Failure | null;
  source: Record<string, unknown> | null;
  process: Record<string, unknown> | null;
  size_mib: number | null;
  zones: string[];
  created_at: string | null;
  ready_at: string | null;
}

export interface ListTemplatesParams extends PaginationParams {
  owner?: TemplateOwner;
  zone?: string;
}

export type ListTemplateRevisionsParams = PaginationParams;

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

/**
 * Workspace lifecycle status. Maps 1:1 to native Kibotos WorkspaceState in lowercase.
 */
export type WorkspaceStatus =
  | 'creating'
  | 'available'
  | 'stopping'
  | 'stopped'
  | 'starting'
  | 'deleting'
  | 'failed'
  | 'unknown';

export type NetworkMode = 'public_internet' | 'deny_all';

export type TimeoutAction = 'stop' | 'delete';

export interface WorkspaceTemplateRef {
  id: string;
  revision: string;
}

export interface WorkspaceTemplateInput {
  id: string;
  revision?: string;
}

export interface WorkspaceResources {
  vcpus: number;
  memory_mib: number;
  disk_mib: number;
}

export interface WorkspaceResourcesInput {
  vcpus?: number;
  memory_mib?: number;
  disk_mib?: number;
}

export interface NetworkEgressRule {
  egress_id: string;
  destinations: string[];
}

export type NetworkResolverPath =
  | { type: 'direct' }
  | { type: 'egress'; egress_id: string };

export interface NetworkResolverRule {
  resolver_id: string;
  path: NetworkResolverPath;
}

export interface WorkspaceNetworkInput {
  mode: NetworkMode;
  allow?: string[];
  deny?: string[];
  egresses?: NetworkEgressRule[];
  resolvers?: NetworkResolverRule[];
}

export interface WorkspaceNetwork extends WorkspaceNetworkInput {
  /** Null while the workspace is stopped. */
  applied: boolean | null;
}

export interface WorkspaceVolumeAttachment {
  id: string;
  mount_path: string;
  delete_with_workspace: boolean;
}

export interface WorkspaceVolumeAttachmentInput {
  id: string;
  mount_path?: string;
  delete_with_workspace?: boolean;
}

export interface WorkspaceTimeout {
  expires_at: string;
  action: TimeoutAction;
}

export interface WorkspaceTimeoutInput {
  seconds: number;
  action?: TimeoutAction;
}

export interface WorkspacePack {
  id: string;
  revision: string;
  provided_capabilities: string[];
}

export interface Workspace {
  id: string;
  name: string | null;
  zone: string;
  template: WorkspaceTemplateRef;
  status: WorkspaceStatus;
  failure: Failure | null;
  resources: WorkspaceResources;
  network: WorkspaceNetwork;
  volume: WorkspaceVolumeAttachment | null;
  volumes?: WorkspaceVolumeAttachment[];
  packs?: WorkspacePack[];
  timeout: WorkspaceTimeout | null;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkspaceRequest {
  name?: string;
  zone?: string;
  template: WorkspaceTemplateInput;
  resources?: WorkspaceResourcesInput;
  network?: WorkspaceNetworkInput;
  volume?: WorkspaceVolumeAttachmentInput;
  volumes?: WorkspaceVolumeAttachmentInput[];
  packs?: string[];
  timeout?: WorkspaceTimeoutInput;
  labels?: Record<string, string>;
}

export interface ListWorkspacesParams extends PaginationParams {
  status?: WorkspaceStatus | WorkspaceStatus[];
  zone?: string;
  /** Label filters serialized as `label.<key>=<value>` query parameters. */
  labels?: Record<string, string>;
}

export interface UpdateWorkspaceRequest {
  name?: string | null;
  /** Set a key to `null` to delete that label. */
  labels?: Record<string, string | null>;
  resources?: WorkspaceResourcesInput;
}

export interface StartWorkspaceRequest {
  timeout?: WorkspaceTimeoutInput;
}

export interface StopWorkspaceRequest {
  force?: boolean;
}

export interface SetWorkspaceTimeoutRequest {
  seconds: number;
  action?: TimeoutAction;
}

export interface SetWorkspaceNetworkRequest extends WorkspaceNetworkInput {
  drain_seconds?: number;
}

export interface SetWorkspaceVolumesRequest {
  volumes: WorkspaceVolumeAttachmentInput[];
}

export interface SetWorkspacePacksRequest {
  packs: string[];
}

export interface WorkspaceMetricsParams {
  start?: string | Date;
  end?: string | Date;
}

export interface WorkspaceGuestMetricsPoint {
  time: string;
  cpu_count: number;
  cpu_used_percent: number;
  disk_total_bytes: number;
  disk_used_bytes: number;
  memory_cache_bytes: number;
  memory_total_bytes: number;
  memory_used_bytes: number;
}

export interface WorkspaceHostMetricsPoint {
  time: string;
  cpu_cores?: number;
  cpu_pressure_some_ratio?: number;
  io_pressure_full_ratio?: number;
  io_pressure_some_ratio?: number;
  memory_bytes?: number;
  memory_pressure_full_ratio?: number;
  memory_pressure_some_ratio?: number;
  network_receive_bytes_per_second?: number;
  network_receive_dropped_per_second?: number;
  network_receive_packets_per_second?: number;
  network_transmit_bytes_per_second?: number;
  network_transmit_dropped_per_second?: number;
  network_transmit_packets_per_second?: number;
}

export interface WorkspaceMetrics {
  earliest_time: string | null;
  guest: WorkspaceGuestMetricsPoint[];
  host: WorkspaceHostMetricsPoint[];
  step_seconds: number;
}

export interface AttachVolumeRequest {
  id: string;
  mount_path?: string;
  delete_with_workspace?: boolean;
}

export interface WaitWorkspaceParams {
  /** One or more target statuses to wait for. */
  status: WorkspaceStatus | WorkspaceStatus[];
  /**
   * Total wait timeout in seconds. Defaults to `30`.
   * Values greater than `60` automatically loop across `<= 60s` server wait calls.
   */
  timeoutSeconds?: number;
}

// ---------------------------------------------------------------------------
// Exposures
// ---------------------------------------------------------------------------

export type ExposureStatus = 'active' | 'revoked';

export interface Exposure {
  id: string;
  workspace_id: string;
  port: number;
  url: string;
  status: ExposureStatus;
  expires_at: string;
  revoked_at: string | null;
  created_at: string;
}

export interface CreateExposureRequest {
  port: number;
  ttl_seconds?: number;
}

export interface ListExposuresParams extends PaginationParams {
  include_revoked?: boolean;
}

// ---------------------------------------------------------------------------
// Volumes
// ---------------------------------------------------------------------------

/**
 * Volume lifecycle status. Maps 1:1 to native Kibotos VolumeState in lowercase:
 * `workspace_id` expresses workspace binding; `available` means ready and not
 * mounted to a running workspace; `attached` means mounted to a running workspace;
 * `lost` means the underlying storage pool was lost.
 */
export type VolumeStatus =
  | 'creating'
  | 'available'
  | 'attached'
  | 'resizing'
  | 'deleting'
  | 'failed'
  | 'lost'
  | 'unknown';

export interface Volume {
  id: string;
  name: string | null;
  zone: string;
  size_gib: number;
  status: VolumeStatus;
  failure: Failure | null;
  workspace_id: string | null;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface CreateVolumeRequest {
  name?: string;
  zone?: string;
  size_gib: number;
  labels?: Record<string, string>;
}

export interface ListVolumesParams extends PaginationParams {
  zone?: string;
  attached?: boolean;
  /** Label filters serialized as `label.<key>=<value>` query parameters. */
  labels?: Record<string, string>;
}

export interface UpdateVolumeRequest {
  name?: string | null;
  /** Set a key to `null` to delete that label. */
  labels?: Record<string, string | null>;
}

export interface ResizeVolumeRequest {
  size_gib: number;
}

export interface WaitVolumeParams {
  /** One or more target statuses to wait for. */
  status: VolumeStatus | VolumeStatus[];
  /**
   * Total wait timeout in seconds. Defaults to `30`.
   * Values greater than `60` automatically loop across `<= 60s` server wait calls.
   */
  timeoutSeconds?: number;
}

// Packs and network resources

interface PackRelease {
  id: string;
  architecture: string;
  guest_requirements: string[];
  provided_capabilities: string[];
  published_at: string;
  version: string;
  zones: string[];
}

export interface Pack extends PackRelease {
  default_revision: string;
  description: string;
  display_name: string;
}

export interface PackRevision extends PackRelease {
  pack_id: string;
}

export interface ListPacksParams extends PaginationParams {
  zone?: string;
}

export interface ListEgressesParams extends ListPacksParams {
  labels?: Record<string, string>;
}

export type ListResolversParams = ListEgressesParams;

export interface EgressTLS {
  ca_cert?: string | null;
  server_name?: string | null;
}

export interface EgressSocks5 {
  address: string;
  tls?: EgressTLS | null;
  username?: string | null;
}

export interface EgressSocks5Input extends EgressSocks5 {
  /** Write-only. Supply again when replacing the configuration. */
  password?: string | null;
}

interface ZonedNetworkResource {
  id: string;
  name: string | null;
  zone: string;
  labels: Record<string, string>;
  revision: number;
  created_at: string;
  updated_at: string;
}

interface CreateNetworkResourceRequest {
  name?: string;
  zone?: string;
  labels?: Record<string, string>;
}

export interface Egress extends ZonedNetworkResource {
  protocol: 'socks5';
  socks5: EgressSocks5;
  udp_enabled: boolean;
}

export interface CreateEgressRequest extends CreateNetworkResourceRequest {
  protocol: 'socks5';
  socks5: EgressSocks5Input;
  udp_enabled?: boolean;
}

export interface ReplaceEgressRequest {
  protocol: 'socks5';
  socks5: EgressSocks5Input;
  udp_enabled?: boolean;
  drain_seconds?: number;
}

export interface UpdateEgressRequest {
  name?: string | null;
  labels?: Record<string, string | null>;
}

export interface ResolverAddress {
  address: string;
}

export interface ResolverHTTPS {
  url: string;
}

interface ResolverConfig {
  protocol: 'udp' | 'tcp' | 'tls' | 'https';
  udp?: ResolverAddress;
  tcp?: ResolverAddress;
  tls?: ResolverAddress;
  https?: ResolverHTTPS;
}

export interface Resolver extends ZonedNetworkResource, ResolverConfig {}

export interface CreateResolverRequest extends CreateNetworkResourceRequest, ResolverConfig {}

export type ReplaceResolverRequest = ResolverConfig;
export type UpdateResolverRequest = UpdateEgressRequest;

// Workspace data plane

export interface ExecRequest {
  argv: string[];
  env?: Record<string, string>;
  max_output_bytes?: number;
  stdin?: string | null;
  timeout_seconds?: number;
  user?: string | null;
  working_dir?: string | null;
}

export interface ExecResponse {
  duration_ms: number;
  exit_code: number | null;
  signal: number | null;
  stdout: string;
  stderr: string;
  termination_reason:
    | 'exited'
    | 'signaled'
    | 'timed_out'
    | 'canceled'
    | 'failed'
    | 'memory_saturated'
    | 'out_of_memory';
  truncated: boolean;
}

export interface ExecEvent {
  event: 'started' | 'stdout' | 'stderr' | 'heartbeat' | 'exited' | 'error';
  /** The decoded JSON payload, or the original text when the payload is not JSON. */
  data: unknown;
}

export interface FileEntry {
  path: string;
  name: string;
  type: string;
  mode: string;
  modified_at: string | null;
  size_bytes: number;
}

export interface FileList {
  path: string;
  entries: FileEntry[];
}

export interface FileWriteResult {
  path: string;
  size_bytes: number;
  sha256: string;
}

export interface FileOptions extends RequestOptions {
  user?: string;
}

export interface ListFilesOptions extends FileOptions {
  depth?: number;
}

export interface ReadFileOptions extends FileOptions {
  range?: string;
  ifNoneMatch?: string;
  as?: 'response' | 'bytes' | 'stream';
}

export interface WriteFileOptions extends FileOptions {
  mode?: string;
  createOnly?: boolean;
}

export type FileContent = Uint8Array | ReadableStream<Uint8Array> | string | Blob;

export interface MakeDirectoryRequest {
  path: string;
  mode?: string;
  user?: string | null;
}

export interface MoveFileRequest {
  source: string;
  destination: string;
  user?: string | null;
}
