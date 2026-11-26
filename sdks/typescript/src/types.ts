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

export interface WorkspaceNetwork {
  mode: NetworkMode;
  applied: boolean;
}

export interface WorkspaceNetworkInput {
  mode: NetworkMode;
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

export interface SetWorkspaceNetworkRequest {
  mode: NetworkMode;
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

export type ExposureStatus = 'active' | 'revoked' | 'expired';

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
