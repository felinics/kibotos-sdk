import { afterEach, describe, expect, it, vi } from 'vitest';
import { type WebSocketHandshake, type WorkspaceControlFrame, Kibotos, WorkspaceChannel, decodeBinaryFrame, decodeWorkspaceFrame, encodeBinaryFrame } from '../src/index';

class MockWebSocket extends EventTarget {
  binaryType = 'blob';
  send = vi.fn();
  close = vi.fn();
}

describe('Workspace WebSockets', () => {
  afterEach(() => vi.unstubAllGlobals());

  it.each(Object.values(WorkspaceChannel))('encodes and decodes channel %d with a six-byte big-endian header', (channel) => {
    const frame = encodeBinaryFrame(0x010203fe, channel, Uint8Array.of(0, 255));
    expect([...frame]).toEqual([1, 1, 2, 3, 254, channel, 0, 255]);
    const padded = Uint8Array.of(99, ...frame, 99);
    expect(decodeBinaryFrame(padded.subarray(1, padded.length - 1))).toEqual({
      stream_id: 0x010203fe, channel, payload: Uint8Array.of(0, 255),
    });
    expect(decodeBinaryFrame(encodeBinaryFrame(0xffffffff, channel, new Uint8Array()).buffer)).toMatchObject({
      stream_id: 0xffffffff, channel, payload: new Uint8Array(),
    });
  });

  it('rejects invalid versions, lengths, channels, IDs, and control envelopes', () => {
    expect(() => decodeBinaryFrame(Uint8Array.of(1, 0, 0, 0, 0))).toThrow('shorter');
    expect(() => decodeBinaryFrame(Uint8Array.of(2, 0, 0, 0, 0, 1))).toThrow('version');
    expect(() => decodeBinaryFrame(Uint8Array.of(1, 0, 0, 0, 1, 5))).toThrow('channel');
    expect(() => decodeBinaryFrame(Uint8Array.of(1, 0, 0, 0, 0, 1))).toThrow('stream_id');
    for (const id of [0, -1, 0x100000000, 1.5]) {
      expect(() => encodeBinaryFrame(id, 0, new Uint8Array())).toThrow('32-bit');
    }
    expect(() => decodeWorkspaceFrame('null')).toThrow('control frame');
  });

  it('dispatches control and binary frames and sends controls for all three stream kinds', () => {
    const socket = new MockWebSocket();
    const factory = vi.fn((_url: string, _options: WebSocketHandshake) => socket as unknown as WebSocket);
    const client = new Kibotos({ apiKey: 'kb_test', defaultHeaders: { 'X-App': 'test' } });
    const channel = client.workspaces.connectWS('wks/a', {
      factory, headers: { 'X-App': 'override' }, protocols: ['kibotos-test'],
    });
    expect(factory).toHaveBeenCalledWith('wss://api.kibo.to/v1/workspaces/wks%2Fa/ws', {
      headers: { authorization: 'Bearer kb_test', 'x-app': 'override' },
      protocols: ['kibotos-test'],
    });
    expect(socket.binaryType).toBe('arraybuffer');

    for (const id of [0, -1, 0x100000000, 1.5]) {
      expect(() => channel.open(id, 'process')).toThrow('stream_id');
    }
    channel.open(1, 'process', { action: 'start', mode: 'pipe', argv: ['cat'] });
    channel.open(2, 'fs.watch', { path: '/tmp' });
    channel.open(3, 'tunnel', { target: { type: 'tcp', port: 8000 } });
    channel.open(0xffffffff, 'process');
    const outgoing = [
      { type: 'resize', stream_id: 1, cols: 80, rows: 24 },
      { type: 'signal', stream_id: 1, signal: 'SIGTERM' },
      { type: 'close_stdin', stream_id: 1 },
      { type: 'close', stream_id: 2 },
      { type: 'ping' },
      { type: 'pong' },
    ] as const;
    for (const frame of outgoing) channel.sendControl(frame);
    const controls = socket.send.mock.calls.map(([value]) => JSON.parse(value));
    expect(controls).toEqual([
      { type: 'open', kind: 'process', stream_id: 1, params: { action: 'start', mode: 'pipe', argv: ['cat'] } },
      { type: 'open', kind: 'fs.watch', stream_id: 2, params: { path: '/tmp' } },
      { type: 'open', kind: 'tunnel', stream_id: 3, params: { target: { type: 'tcp', port: 8000 } } },
      { type: 'open', kind: 'process', stream_id: 0xffffffff, params: {} },
      ...outgoing,
    ]);
    channel.sendBinary(1, WorkspaceChannel.stdin, Uint8Array.of(0x61));
    expect(socket.send).toHaveBeenLastCalledWith(Uint8Array.of(1, 0, 0, 0, 1, 0, 0x61));

    const onFrame = vi.fn();
    const unsubscribe = channel.onFrame(onFrame);
    const incoming: WorkspaceControlFrame[] = [
      { type: 'opened', stream_id: 1, reattach_token: 'server-token' },
      { type: 'fs.event', stream_id: 2, path: '/tmp/a' },
      { type: 'heartbeat', stream_id: 2 },
      { type: 'closed', stream_id: 2, reason: 'closed' },
      { type: 'exited', stream_id: 1, exit_code: 0 },
      { type: 'error', stream_id: 3, code: 'unavailable' },
      { type: 'ping' },
      { type: 'pong' },
    ];
    for (const frame of incoming) {
      socket.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(frame) }));
    }
    socket.dispatchEvent(new MessageEvent('message', {
      data: encodeBinaryFrame(1, WorkspaceChannel.stdout, Uint8Array.of(0xff)).buffer,
    }));
    expect(onFrame.mock.calls.map(([value]) => value)).toEqual([
      ...incoming.map((frame) => ({ type: 'control', frame })),
      { type: 'binary', frame: { stream_id: 1, channel: 1, payload: Uint8Array.of(0xff) } },
    ]);
    unsubscribe();
    socket.dispatchEvent(new MessageEvent('message', { data: '{"type":"pong"}' }));
    expect(onFrame).toHaveBeenCalledTimes(incoming.length + 1);
    channel.close(1000, 'done');
    expect(socket.close).toHaveBeenCalledWith(1000, 'done');
  });

  it('uses the Node global WebSocket for authenticated upgrades by default', () => {
    const constructor = vi.fn(function () { return new MockWebSocket(); });
    vi.stubGlobal('WebSocket', constructor);
    const workspaces = new Kibotos({ apiKey: 'kb_test' }).workspaces;
    workspaces.connectWS('wks');
    expect(constructor).toHaveBeenCalledWith('wss://api.kibo.to/v1/workspaces/wks/ws', {
      protocols: undefined, headers: { authorization: 'Bearer kb_test' },
    });
  });

  it('builds tunnel, desktop and HTTP/WS proxy URLs and connects raw sockets', () => {
    const socket = new MockWebSocket();
    const factory = vi.fn((_url: string, _options: WebSocketHandshake) => socket as unknown as WebSocket);
    const client = new Kibotos({ apiKey: 'kb_test', baseUrl: 'http://localhost:8000/api/' });
    const workspaces = client.workspaces;
    const base = 'localhost:8000/api/v1/workspaces/wks%2Fa';
    const query = new URLSearchParams([['q', 'a +'], ['q', 'b']]);

    expect(workspaces.tunnelWSUrl('wks/a', { unix_path: '/tmp/a+b.sock' }))
      .toBe(`ws://${base}/tunnels/ws?unix_path=%2Ftmp%2Fa%2Bb.sock`);
    expect(workspaces.proxyUrl('wks/a', 8080, '/a b/c?', query))
      .toBe(`http://${base}/proxy/8080/a%20b/c%3F?q=a+%2B&q=b`);
    expect(workspaces.connectTunnel('wks/a', { port: 80, family: 'ipv6' }, { factory })).toBe(socket);
    expect(workspaces.connectDesktop('wks/a', { factory })).toBe(socket);
    expect(workspaces.connectProxy('wks/a', 8080, { path: '/a b/c?', query, factory })).toBe(socket);
    expect(factory.mock.calls.map((args) => args[0])).toEqual([
      `ws://${base}/tunnels/ws?port=80&family=ipv6`,
      `ws://${base}/desktop/vnc`,
      `ws://${base}/proxy/8080/a%20b/c%3F?q=a+%2B&q=b`,
    ]);
    expect(socket.binaryType).toBe('blob');
  });
});
