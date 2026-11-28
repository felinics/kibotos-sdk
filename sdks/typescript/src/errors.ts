import type { ErrorEnvelope } from './types';

export interface KibotosAPIErrorInit {
  status: number;
  code: string;
  message: string;
  requestId?: string;
  retryable: boolean;
  details?: Record<string, unknown>;
  headers?: Headers;
  cause?: unknown;
}

/**
 * Base error class for all errors thrown by `@kibotos/sdk`.
 */
export class KibotosError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = new.target.name;
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/**
 * Thrown when a request fails to reach the Kibotos API due to network errors or timeouts.
 */
export class APIConnectionError extends KibotosError {
  constructor(message = 'Failed to connect to the Kibotos API', options?: { cause?: unknown }) {
    super(message, options);
  }
}

/**
 * Thrown when the Kibotos API returns a non-2xx HTTP response.
 */
export class KibotosAPIError extends KibotosError {
  readonly status: number;
  readonly code: string;
  readonly requestId: string | undefined;
  readonly retryable: boolean;
  readonly details: Record<string, unknown> | undefined;
  readonly headers: Headers | undefined;

  constructor(init: KibotosAPIErrorInit) {
    super(init.message, init.cause !== undefined ? { cause: init.cause } : undefined);
    this.status = init.status;
    this.code = init.code;
    this.requestId = init.requestId;
    this.retryable = init.retryable;
    this.details = init.details;
    this.headers = init.headers;
  }
}

/**
 * Thrown on HTTP 401 responses (e.g. `unauthenticated`).
 */
export class AuthenticationError extends KibotosAPIError {}

/**
 * Thrown on HTTP 403 responses (e.g. `permission_denied`, `zone_not_permitted`, `quota_exceeded`).
 */
export class PermissionDeniedError extends KibotosAPIError {}

/**
 * Thrown on HTTP 404 responses (e.g. `workspace_not_found`, `volume_not_found`).
 */
export class NotFoundError extends KibotosAPIError {}

/**
 * Thrown on HTTP 408 responses (e.g. `wait_timeout`).
 */
export class WaitTimeoutError extends KibotosAPIError {}

/**
 * Thrown on HTTP 409 responses (e.g. `invalid_state`, `operation_in_progress`, `volume_attached`).
 */
export class ConflictError extends KibotosAPIError {}

/**
 * Thrown on HTTP 409 responses with error code `unexpected_status` during `wait()` calls.
 */
export class UnexpectedStatusError extends ConflictError {}

/**
 * Thrown on HTTP 429 responses (`rate_limited`).
 */
export class RateLimitError extends KibotosAPIError {
  /** Parsed `Retry-After` delay in milliseconds, if provided by the server. */
  readonly retryAfterMs: number | undefined;

  constructor(init: KibotosAPIErrorInit & { retryAfterMs?: number }) {
    super(init);
    this.retryAfterMs = init.retryAfterMs;
  }
}

const DEFAULT_RETRYABLE_STATUSES = new Set([429, 500, 502, 503, 504]);

/**
 * Parses an HTTP `Retry-After` header (delta-seconds or HTTP-date) into milliseconds.
 * Returns `undefined` if the header is missing or unparseable.
 */
export function parseRetryAfterMs(headerValue: string | null | undefined, nowMs = Date.now()): number | undefined {
  if (!headerValue) {
    return undefined;
  }
  const trimmed = headerValue.trim();
  if (trimmed.length === 0) {
    return undefined;
  }

  // Delta-seconds format (e.g. "2" or "1.5")
  const seconds = Number(trimmed);
  if (Number.isFinite(seconds) && seconds >= 0) {
    return Math.round(seconds * 1000);
  }

  // HTTP-date format (e.g. "Wed, 21 Oct 2026 07:28:00 GMT")
  const dateMs = Date.parse(trimmed);
  if (!Number.isNaN(dateMs)) {
    return Math.max(0, dateMs - nowMs);
  }

  return undefined;
}

function isErrorEnvelope(value: unknown): value is ErrorEnvelope {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const err = (value as { error?: unknown }).error;
  return typeof err === 'object' && err !== null;
}

/**
 * Constructs the most specific {@link KibotosAPIError} subclass from a failed HTTP response.
 */
export function createAPIError(
  status: number,
  headers: Headers,
  body: unknown,
  fallbackMessage?: string,
): KibotosAPIError {
  const headerRequestId = headers.get('Request-Id') ?? undefined;
  let code = 'http_error';
  let message = fallbackMessage || `Kibotos API request failed with status ${status}`;
  let requestId = headerRequestId;
  let retryable = DEFAULT_RETRYABLE_STATUSES.has(status);
  let details: Record<string, unknown> | undefined;

  if (isErrorEnvelope(body)) {
    const err = body.error;
    if (typeof err.code === 'string' && err.code.length > 0) {
      code = err.code;
    }
    if (typeof err.message === 'string' && err.message.length > 0) {
      message = err.message;
    }
    if (typeof err.request_id === 'string' && err.request_id.length > 0) {
      requestId = err.request_id;
    }
    if (typeof err.retryable === 'boolean') {
      retryable = err.retryable;
    }
    if (typeof err.details === 'object' && err.details !== null && !Array.isArray(err.details)) {
      details = err.details;
    }
  }

  const init: KibotosAPIErrorInit = {
    status,
    code,
    message,
    requestId,
    retryable,
    details,
    headers,
  };

  if (status === 401) {
    return new AuthenticationError(init);
  }
  if (status === 403) {
    return new PermissionDeniedError(init);
  }
  if (status === 404) {
    return new NotFoundError(init);
  }
  if (status === 408) {
    return new WaitTimeoutError(init);
  }
  if (status === 409) {
    if (code === 'unexpected_status') {
      return new UnexpectedStatusError(init);
    }
    return new ConflictError(init);
  }
  if (status === 429) {
    const retryAfterMs = parseRetryAfterMs(headers.get('Retry-After'));
    return new RateLimitError({ ...init, retryAfterMs });
  }

  return new KibotosAPIError(init);
}
