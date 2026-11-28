import type { Page } from './types';

/**
 * Lazily iterates over every item across all pages returned by a paginated Kibotos endpoint.
 */
export async function* paginate<T>(
  fetchPage: (pageToken?: string) => Promise<Page<T>>,
  initialPageToken?: string,
): AsyncGenerator<T, void, undefined> {
  let pageToken = initialPageToken;
  const seenTokens = new Set<string>();

  while (true) {
    const page = await fetchPage(pageToken);
    for (const item of page.data) {
      yield item;
    }

    const next = page.next_page_token;
    if (!next || seenTokens.has(next)) {
      break;
    }
    seenTokens.add(next);
    pageToken = next;
  }
}

export type QueryValue =
  | string
  | number
  | boolean
  | readonly string[]
  | Record<string, string>
  | undefined;

/**
 * Builds a `URLSearchParams` instance from typed query parameters.
 * - `labels` objects are expanded to `label.<key>=<value>` entries.
 * - Array values are appended repeatedly with the same key.
 * - `undefined` entries are omitted.
 */
export function buildQueryParams(params?: Record<string, QueryValue>): URLSearchParams {
  const searchParams = new URLSearchParams();
  if (!params) {
    return searchParams;
  }

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) {
      continue;
    }
    if (key === 'labels' && typeof value === 'object' && !Array.isArray(value)) {
      for (const [labelKey, labelValue] of Object.entries(value)) {
        searchParams.set(`label.${labelKey}`, labelValue);
      }
    } else if (Array.isArray(value)) {
      for (const item of value) {
        searchParams.append(key, String(item));
      }
    } else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
      searchParams.set(key, String(value));
    }
  }

  return searchParams;
}
