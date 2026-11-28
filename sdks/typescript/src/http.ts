import {
  APIConnectionError,
  KibotosAPIError,
  RateLimitError,
  createAPIError,
  parseRetryAfterMs,
} from './errors';
import type { KibotosClientOptions, RequestOptions } from './types';

export const DEFAULT_BASE_URL = 'https://api.kibo.to/v1';
export const DEFAULT_MAX_RETRIES = 3;
export const DEFAULT_TIMEOUT_MS = 30_000;
export const SDK_VERSION = '0.1.0';

const INITIAL_RETRY_DELAY_MS = 500;
const MAX_RETRY_DELAY_MS = 8_000;
const RETRYABLE_HTTP_STATUSES = new Set([429, 500, 502, 503, 504]);

export type HttpMethod = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';

export interface InternalRequestConfig {
  method: HttpMethod;
  path: string;
  query?: URLSearchParams;
  body?: unknown;
  rawBody?: BodyInit;
  duplex?: 'half';
  accept?: string;
  successStatuses?: readonly number[];
  nonReplayable?: boolean;
  disableIdempotency?: boolean;
  opts?: RequestOptions;
  /**
   * When true, HTTP 408 `wait_timeout` responses are NOT retried by the low-level transport
   * so that higher-level `wait()` chunk looping controls the timeout budget directly.
   */
  disableWaitTimeoutRetry?: boolean;
}

/**
 * Safely reads an environment variable in Node/Bun/Deno without crashing in
 * Cloudflare Workers or browser runtimes where `process` does not exist.
 */
export function readEnv(name: string): string | undefined {
  try {
    const proc = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process;
    if (proc && typeof proc === 'object' && proc.env && typeof proc.env === 'object') {
      const val = proc.env[name];
      return typeof val === 'string' && val.length > 0 ? val : undefined;
    }
  } catch {
    // Restricted runtime environment; fall back to undefined.
  }
  return undefined;
}

/**
 * Normalizes the base URL so both `https://api.kibo.to` and `https://api.kibo.to/v1/`
 * resolve cleanly to `https://api.kibo.to/v1`.
 */
export function normalizeBaseUrl(rawUrl: string): string {
  const trimmed = rawUrl.trim().replace(/\/+$/, '');
  if (trimmed.length === 0) {
    return DEFAULT_BASE_URL;
  }
  if (trimmed.endsWith('/v1')) {
    return trimmed;
  }
  return `${trimmed}/v1`;
}

function generateUuid(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID();
  }
  // Fallback using Web Crypto getRandomValues for older/custom runtimes
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function createAbortError(signal: AbortSignal): unknown {
  if (signal.reason !== undefined) {
    return signal.reason;
  }
  if (typeof DOMException !== 'undefined') {
    return new DOMException('The operation was aborted.', 'AbortError');
  }
  const err = new Error('The operation was aborted.');
  err.name = 'AbortError';
  return err;
}

function combineSignals(
  userSignal: AbortSignal | undefined,
  timeoutMs: number,
): { signal: AbortSignal | undefined; isTimeout: () => boolean; cleanup: () => void } {
  const hasTimeout = Number.isFinite(timeoutMs) && timeoutMs > 0;
  if (!userSignal && !hasTimeout) {
    return { signal: undefined, isTimeout: () => false, cleanup: () => {} };
  }

  let timedOut = false;

  if (!userSignal && hasTimeout) {
    const timeoutSignal = AbortSignal.timeout(timeoutMs);
    timeoutSignal.addEventListener(
      'abort',
      () => {
        timedOut = true;
      },
      { once: true },
    );
    return {
      signal: timeoutSignal,
      isTimeout: () => timedOut || timeoutSignal.aborted,
      cleanup: () => {},
    };
  }

  if (userSignal && !hasTimeout) {
    return {
      signal: userSignal,
      isTimeout: () => false,
      cleanup: () => {},
    };
  }

  // Both userSignal and timeoutMs are active
  const controller = new AbortController();
  const onUserAbort = (): void => {
    controller.abort(createAbortError(userSignal!));
  };

  if (userSignal!.aborted) {
    controller.abort(createAbortError(userSignal!));
    return {
      signal: controller.signal,
      isTimeout: () => false,
      cleanup: () => {},
    };
  }

  userSignal!.addEventListener('abort', onUserAbort, { once: true });
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort(new Error(`Request timed out after ${timeoutMs}ms`));
  }, timeoutMs);

  return {
    signal: controller.signal,
    isTimeout: () => timedOut,
    cleanup: () => {
      clearTimeout(timer);
      userSignal!.removeEventListener('abort', onUserAbort);
    },
  };
}

function combineResponseSignals(
  userSignal: AbortSignal | undefined,
  timeoutMs: number,
): ReturnType<typeof combineSignals> {
  const hasTimeout = Number.isFinite(timeoutMs) && timeoutMs > 0;
  if (userSignal && hasTimeout && typeof AbortSignal.any !== 'function') {
    // Older Node 20 releases have AbortSignal.timeout but lack AbortSignal.any.
    const combined = combineSignals(userSignal, timeoutMs);
    combined.signal?.addEventListener('abort', combined.cleanup, { once: true });
    return combined;
  }

  // Native signals keep cancellation active while the caller consumes the response.
  const timeoutSignal = hasTimeout ? AbortSignal.timeout(timeoutMs) : undefined;
  return {
    signal: userSignal && timeoutSignal
      ? AbortSignal.any([userSignal, timeoutSignal])
      : userSignal ?? timeoutSignal,
    isTimeout: () => timeoutSignal?.aborted === true,
    cleanup: () => {},
  };
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    return Promise.reject(createAbortError(signal));
  }
  if (ms <= 0) {
    return Promise.resolve();
  }
  return new Promise<void>((resolve, reject) => {
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(createAbortError(signal!));
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

function calculateBackoffMs(attempt: number, retryAfterMs?: number): number {
  if (retryAfterMs !== undefined && Number.isFinite(retryAfterMs) && retryAfterMs >= 0) {
    return retryAfterMs;
  }
  const exponential = Math.min(INITIAL_RETRY_DELAY_MS * 2 ** attempt, MAX_RETRY_DELAY_MS);
  // Apply jitter in [0.5, 1.0) of the exponential delay
  const jitter = 0.5 + Math.random() * 0.5;
  return Math.round(exponential * jitter);
}

/**
 * Internal fetch-based HTTP transport powering all resource namespaces.
 */
export class HttpClient {
  readonly baseUrl: string;
  readonly maxRetries: number;
  readonly timeoutMs: number;
  private readonly apiKey: string | undefined;
  private readonly fetchImpl: typeof globalThis.fetch;
  private readonly defaultHeaders: Record<string, string>;

  constructor(options: KibotosClientOptions = {}) {
    this.apiKey = options.apiKey ?? readEnv('KIBOTOS_API_KEY');
    this.baseUrl = normalizeBaseUrl(options.baseUrl ?? readEnv('KIBOTOS_API_URL') ?? DEFAULT_BASE_URL);
    this.maxRetries = Math.max(0, options.maxRetries ?? DEFAULT_MAX_RETRIES);
    this.timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.fetchImpl = options.fetch ?? globalThis.fetch;
    this.defaultHeaders = { ...(options.defaultHeaders ?? {}) };

    if (typeof this.fetchImpl !== 'function') {
      throw new APIConnectionError(
        'No fetch implementation found. Provide a custom `fetch` option or use a runtime with global `fetch` (Node 20+, Bun, Deno, Cloudflare Workers).',
      );
    }
  }

  buildUrl(path: string, query?: URLSearchParams): string {
    const normalizedPath = path.startsWith('/') ? path : `/${path}`;
    const url = new URL(`${this.baseUrl}${normalizedPath}`);
    if (query) {
      query.forEach((value, key) => {
        url.searchParams.append(key, value);
      });
    }
    return url.toString();
  }

  buildHeaders(overrides?: Record<string, string>): Headers {
    const headers = new Headers();
    if (this.apiKey) {
      headers.set('Authorization', `Bearer ${this.apiKey}`);
    }
    for (const [k, v] of Object.entries(this.defaultHeaders)) {
      headers.set(k, v);
    }
    for (const [k, v] of Object.entries(overrides ?? {})) {
      headers.set(k, v);
    }
    return headers;
  }

  async request<T>(config: InternalRequestConfig): Promise<T> {
    return (await this.perform(config, false)) as T;
  }

  requestResponse(config: InternalRequestConfig): Promise<Response> {
    return this.perform(config, true) as Promise<Response>;
  }

  private async perform(config: InternalRequestConfig, rawResponse: boolean): Promise<unknown> {
    const { method, path, query, body, rawBody, opts, disableWaitTimeoutRetry } = config;
    const url = this.buildUrl(path, query);
    const effectiveTimeoutMs = opts?.timeoutMs ?? this.timeoutMs;
    const userSignal = opts?.signal;
    const maxRetries = config.nonReplayable ? 0 : this.maxRetries;

    // Build headers once so the exact same Idempotency-Key is reused on every retry.
    const headers = this.buildHeaders(opts?.headers);
    headers.set('Accept', config.accept ?? headers.get('Accept') ?? 'application/json');
    if (!headers.has('User-Agent')) {
      headers.set('User-Agent', `@kibotos/sdk/${SDK_VERSION}`);
    }

    const requestBody = rawBody ?? (body === undefined ? undefined : JSON.stringify(body));
    if (requestBody !== undefined && !headers.has('Content-Type')) {
      headers.set('Content-Type', rawBody === undefined ? 'application/json' : 'application/octet-stream');
    }

    if (config.disableIdempotency) {
      headers.delete('Idempotency-Key');
    } else if (opts?.idempotencyKey) {
      headers.set('Idempotency-Key', opts.idempotencyKey);
    } else if (method === 'POST' && !headers.has('Idempotency-Key')) {
      headers.set('Idempotency-Key', generateUuid());
    }

    let attempt = 0;

    while (true) {
      if (userSignal?.aborted) {
        throw createAbortError(userSignal);
      }

      const { signal, isTimeout, cleanup } = rawResponse
        ? combineResponseSignals(userSignal, effectiveTimeoutMs)
        : combineSignals(userSignal, effectiveTimeoutMs);

      let response: Response;
      try {
        const init: RequestInit & { duplex?: 'half' } = {
          method,
          headers,
          body: requestBody,
          signal,
        };
        if (config.duplex) init.duplex = config.duplex;
        response = await this.fetchImpl(url, init);
      } catch (err) {
        cleanup();

        // Caller explicitly aborted: propagate abort immediately without retrying.
        if (userSignal?.aborted) {
          throw createAbortError(userSignal);
        }

        const connectionError = isTimeout()
          ? new APIConnectionError(`Request timed out after ${effectiveTimeoutMs}ms`, { cause: err })
          : new APIConnectionError(
              err instanceof Error && err.message
                ? `Network error: ${err.message}`
                : 'Network error while contacting the Kibotos API',
              { cause: err },
            );

        if (attempt < maxRetries) {
          const delayMs = calculateBackoffMs(attempt);
          attempt += 1;
          await sleep(delayMs, userSignal);
          continue;
        }
        throw connectionError;
      }

      if (rawResponse && (response.ok || config.successStatuses?.includes(response.status))) {
        return response;
      }

      let parsedBody: unknown;
      try {
        const text = await response.text();
        if (text.length > 0) {
          try {
            parsedBody = JSON.parse(text);
          } catch {
            parsedBody = text;
          }
        }
      } catch (err) {
        cleanup();
        if (userSignal?.aborted) {
          throw createAbortError(userSignal);
        }
        const readError = isTimeout()
          ? new APIConnectionError(`Request timed out after ${effectiveTimeoutMs}ms`, { cause: err })
          : new APIConnectionError('Failed to read response body from the Kibotos API', { cause: err });
        if (attempt < maxRetries) {
          const delayMs = calculateBackoffMs(attempt);
          attempt += 1;
          await sleep(delayMs, userSignal);
          continue;
        }
        throw readError;
      } finally {
        cleanup();
      }

      if (response.ok) {
        return parsedBody;
      }

      const apiError = createAPIError(
        response.status,
        response.headers,
        parsedBody,
        response.statusText || undefined,
      );

      if (this.shouldRetryAPIError(apiError, disableWaitTimeoutRetry) && attempt < maxRetries) {
        const retryAfterMs =
          apiError instanceof RateLimitError
            ? apiError.retryAfterMs
            : parseRetryAfterMs(response.headers.get('Retry-After'));
        const delayMs = calculateBackoffMs(attempt, retryAfterMs);
        attempt += 1;
        await sleep(delayMs, userSignal);
        continue;
      }

      throw apiError;
    }
  }

  private shouldRetryAPIError(error: KibotosAPIError, disableWaitTimeoutRetry?: boolean): boolean {
    if (disableWaitTimeoutRetry && error.status === 408 && error.code === 'wait_timeout') {
      return false;
    }
    return error.retryable || RETRYABLE_HTTP_STATUSES.has(error.status);
  }
}
