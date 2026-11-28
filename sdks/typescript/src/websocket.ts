import { APIConnectionError } from './errors';

const FRAME_VERSION = 0x01;
const HEADER_BYTES = 6;

export const WorkspaceChannel = {
  stdin: 0,
  stdout: 1,
  stderr: 2,
  pty: 3,
  tunnel: 4,
} as const;

export type WorkspaceChannel = (typeof WorkspaceChannel)[keyof typeof WorkspaceChannel];

export interface WorkspaceBinaryFrame {
  stream_id: number;
  channel: WorkspaceChannel;
  payload: Uint8Array;
}

export function encodeBinaryFrame(
  streamId: number,
  channel: WorkspaceChannel,
  payload: Uint8Array,
): Uint8Array<ArrayBuffer> {
  validateStreamId(streamId);
  validateChannel(channel);
  const frame = new Uint8Array(HEADER_BYTES + payload.byteLength);
  const header = new DataView(frame.buffer);
  header.setUint8(0, FRAME_VERSION);
  header.setUint32(1, streamId, false);
  header.setUint8(5, channel);
  frame.set(payload, HEADER_BYTES);
  return frame;
}

export function decodeBinaryFrame(input: ArrayBuffer | ArrayBufferView): WorkspaceBinaryFrame {
  const bytes = input instanceof ArrayBuffer
    ? new Uint8Array(input)
    : new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
  if (bytes.byteLength < HEADER_BYTES) throw new RangeError('Workspace frame is shorter than 6 bytes');
  const header = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (header.getUint8(0) !== FRAME_VERSION) throw new RangeError('Unsupported workspace frame version');
  const streamId = header.getUint32(1, false);
  validateStreamId(streamId);
  const channel = header.getUint8(5);
  validateChannel(channel);
  return {
    stream_id: streamId,
    channel,
    payload: bytes.subarray(HEADER_BYTES),
  };
}

function validateStreamId(streamId: number): void {
  if (!Number.isInteger(streamId) || streamId < 1 || streamId > 0xffffffff) {
    throw new RangeError('stream_id must be a nonzero unsigned 32-bit integer');
  }
}

function validateChannel(channel: number): asserts channel is WorkspaceChannel {
  if (!Number.isInteger(channel) || channel < 0 || channel > 4) {
    throw new RangeError('Invalid workspace frame channel');
  }
}

export type WorkspaceStreamKind = 'process' | 'fs.watch' | 'tunnel';

export type WorkspaceControlType =
  | 'open' | 'opened' | 'resize' | 'signal' | 'close_stdin' | 'close'
  | 'exited' | 'fs.event' | 'heartbeat' | 'closed' | 'error' | 'ping' | 'pong';

export interface WorkspaceControlFrame {
  type: WorkspaceControlType;
  stream_id?: number;
  kind?: WorkspaceStreamKind;
  [key: string]: unknown;
}

export type WorkspaceFrame =
  | { type: 'control'; frame: WorkspaceControlFrame }
  | { type: 'binary'; frame: WorkspaceBinaryFrame };

export function decodeWorkspaceFrame(data: string | ArrayBuffer | ArrayBufferView): WorkspaceFrame {
  if (typeof data !== 'string') return { type: 'binary', frame: decodeBinaryFrame(data) };
  const frame: unknown = JSON.parse(data);
  if (!frame || typeof frame !== 'object' || !('type' in frame) || typeof frame.type !== 'string') {
    throw new TypeError('Invalid workspace control frame');
  }
  return { type: 'control', frame: frame as WorkspaceControlFrame };
}

export type TunnelTarget =
  | { port: number; family?: 'ipv4' | 'ipv6'; unix_path?: never }
  | { unix_path: string; port?: never; family?: never };

export interface WebSocketConnectOptions {
  protocols?: string | string[];
  headers?: Record<string, string>;
  /** Factory for runtimes whose global WebSocket cannot set handshake headers. */
  factory?: (url: string, options: WebSocketHandshake) => WebSocket;
}

export interface WebSocketHandshake {
  protocols?: string | string[];
  headers: Record<string, string>;
}

export function webSocketUrl(url: string): string {
  const target = new URL(url);
  if (target.protocol === 'https:') target.protocol = 'wss:';
  if (target.protocol === 'http:') target.protocol = 'ws:';
  if (target.protocol !== 'ws:' && target.protocol !== 'wss:') {
    throw new TypeError('A WebSocket URL requires HTTP or HTTPS');
  }
  return target.toString();
}

/** Opens a socket using the native Node WebSocket or an explicitly supplied factory. */
export function openWebSocket(url: string, headers: Headers, options?: WebSocketConnectOptions): WebSocket {
  const handshake = { protocols: options?.protocols, headers: Object.fromEntries(headers) };
  if (options?.factory) return options.factory(url, handshake);

  const NativeWebSocket = globalThis.WebSocket;
  if (typeof NativeWebSocket !== 'function') {
    throw new APIConnectionError('Global WebSocket is unavailable. Supply a WebSocket factory.');
  }
  if (Object.keys(handshake.headers).length === 0) {
    return new NativeWebSocket(url, options?.protocols);
  }
  if (globalThis.process?.versions?.node) {
    // Node's built-in WebSocket accepts an Undici WebSocketInit with handshake headers.
    const NodeWebSocket = NativeWebSocket as unknown as new (
      url: string, options: WebSocketHandshake,
    ) => WebSocket;
    return new NodeWebSocket(url, handshake);
  }
  throw new APIConnectionError('This runtime requires a WebSocket factory to set handshake headers.');
}

export class WorkspaceSocket {
  constructor(readonly socket: WebSocket) {
    socket.binaryType = 'arraybuffer';
  }

  /** Subscribe to decoded frames. Returns an unsubscribe function. */
  onFrame(listener: (frame: WorkspaceFrame) => void): () => void {
    const onMessage = (event: MessageEvent<string | ArrayBuffer>) => {
      listener(decodeWorkspaceFrame(event.data));
    };
    this.socket.addEventListener('message', onMessage);
    return () => this.socket.removeEventListener('message', onMessage);
  }

  sendControl(frame: WorkspaceControlFrame): void {
    this.socket.send(JSON.stringify(frame));
  }

  open(streamId: number, kind: WorkspaceStreamKind, params: Record<string, unknown> = {}): void {
    validateStreamId(streamId);
    this.sendControl({ type: 'open', stream_id: streamId, kind, params });
  }

  sendBinary(streamId: number, channel: WorkspaceChannel, payload: Uint8Array): void {
    this.socket.send(encodeBinaryFrame(streamId, channel, payload));
  }

  close(code?: number, reason?: string): void {
    this.socket.close(code, reason);
  }
}
