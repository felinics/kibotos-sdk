import { KibotosError, WaitTimeoutError } from './errors.js';
import type { HttpClient } from './http.js';
import type { RequestOptions } from './types.js';

const DEFAULT_WAIT_TIMEOUT_SECONDS = 30;
const MAX_CHUNK_TIMEOUT_SECONDS = 60;
const HTTP_TIMEOUT_BUFFER_SECONDS = 15;

export interface WaitLoopParams<TStatus extends string> {
  path: string;
  status: TStatus | TStatus[];
  timeoutSeconds?: number;
  opts?: RequestOptions;
}

/**
 * Executes long-polling `GET .../wait` calls, automatically chunking requests into
 * `<= 60s` server windows when `timeoutSeconds > 60`.
 */
export async function executeWaitLoop<TResource, TStatus extends string>(
  http: HttpClient,
  params: WaitLoopParams<TStatus>,
): Promise<TResource> {
  const statuses = Array.isArray(params.status) ? params.status : [params.status];
  if (statuses.length === 0) {
    throw new KibotosError('wait() requires at least one target status');
  }

  const totalTimeoutSeconds = params.timeoutSeconds ?? DEFAULT_WAIT_TIMEOUT_SECONDS;
  if (!Number.isFinite(totalTimeoutSeconds) || totalTimeoutSeconds <= 0) {
    throw new KibotosError('wait() timeoutSeconds must be a positive number');
  }

  const deadlineMs = Date.now() + totalTimeoutSeconds * 1000;
  let remainingBudgetSeconds = totalTimeoutSeconds;

  while (true) {
    if (params.opts?.signal?.aborted) {
      throw params.opts.signal.reason ?? new Error('The operation was aborted.');
    }

    const chunkSeconds = Math.min(
      MAX_CHUNK_TIMEOUT_SECONDS,
      Math.max(1, Math.ceil(remainingBudgetSeconds)),
    );

    const query = new URLSearchParams();
    for (const status of statuses) {
      query.append('status', status);
    }
    query.set('timeout', String(chunkSeconds));

    // Ensure the per-request HTTP timeout exceeds the server's long-poll window
    // unless the caller explicitly provided opts.timeoutMs.
    const effectiveOpts: RequestOptions = {
      ...params.opts,
      timeoutMs:
        params.opts?.timeoutMs ??
        Math.max(http.timeoutMs, (chunkSeconds + HTTP_TIMEOUT_BUFFER_SECONDS) * 1000),
    };

    try {
      return await http.request<TResource>({
        method: 'GET',
        path: params.path,
        query,
        opts: effectiveOpts,
        disableWaitTimeoutRetry: true,
      });
    } catch (err) {
      if (!(err instanceof WaitTimeoutError)) {
        throw err;
      }

      const wallRemainingSeconds = (deadlineMs - Date.now()) / 1000;
      remainingBudgetSeconds = Math.min(remainingBudgetSeconds - chunkSeconds, wallRemainingSeconds);

      if (remainingBudgetSeconds <= 0) {
        throw err;
      }
    }
  }
}
