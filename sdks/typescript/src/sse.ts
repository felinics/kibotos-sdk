import type { ExecEvent } from './types';

const EXEC_EVENTS: ReadonlySet<string> = new Set([
  'started', 'stdout', 'stderr', 'heartbeat', 'exited', 'error',
]);

export async function* parseExecEvents(response: Response): AsyncGenerator<ExecEvent> {
  if (!response.body) throw new Error('Kibotos exec stream has no response body');

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let pending = '';
  let event = '';
  let data: string[] = [];
  let skipLF = false;

  try {
    while (true) {
      const { value, done } = await reader.read();
      const chunk = decoder.decode(value, { stream: !done });
      if (chunk.length > 0) {
        pending += skipLF && chunk.startsWith('\n') ? chunk.slice(1) : chunk;
        skipLF = false;
      }

      let offset = 0;
      const lineEnd = /\r\n|[\r\n]/g;
      for (let match = lineEnd.exec(pending); match; match = lineEnd.exec(pending)) {
        const line = pending.slice(offset, match.index);
        offset = lineEnd.lastIndex;
        skipLF = match[0] === '\r' && offset === pending.length;

        if (line === '') {
          const name = event;
          const payload = data.join('\n');
          const hasData = data.length > 0;
          event = '';
          data = [];
          if (hasData && EXEC_EVENTS.has(name)) {
            let decoded: unknown = payload;
            try {
              decoded = JSON.parse(payload);
            } catch {
              // SSE also permits plain text payloads.
            }
            yield { event: name as ExecEvent['event'], data: decoded };
          }
          continue;
        }

        const colon = line.indexOf(':');
        const field = colon === -1 ? line : line.slice(0, colon);
        let fieldValue = colon === -1 ? '' : line.slice(colon + 1);
        if (fieldValue.startsWith(' ')) fieldValue = fieldValue.slice(1);
        if (field === 'event') event = fieldValue;
        if (field === 'data') data.push(fieldValue);
      }

      pending = pending.slice(offset);
      if (done) break;
    }
  } finally {
    // Return the connection to fetch when a consumer stops iterating early.
    try {
      await reader.cancel();
    } catch {
      // Preserve the original read or abort error.
    } finally {
      reader.releaseLock();
    }
  }
}
