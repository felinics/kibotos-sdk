export { Kibotos } from './client';
export {
  APIConnectionError,
  AuthenticationError,
  ConflictError,
  KibotosAPIError,
  KibotosError,
  NotFoundError,
  PermissionDeniedError,
  RateLimitError,
  UnexpectedStatusError,
  WaitTimeoutError,
  type KibotosAPIErrorInit,
} from './errors';
export { DEFAULT_BASE_URL, DEFAULT_MAX_RETRIES, DEFAULT_TIMEOUT_MS, SDK_VERSION } from './http';
export * from './resources/index';
export type * from './types';
export {
  WorkspaceChannel,
  WorkspaceSocket,
  decodeBinaryFrame,
  decodeWorkspaceFrame,
  encodeBinaryFrame,
  type TunnelTarget,
  type WebSocketConnectOptions,
  type WebSocketHandshake,
  type WorkspaceBinaryFrame,
  type WorkspaceControlFrame,
  type WorkspaceControlType,
  type WorkspaceFrame,
  type WorkspaceStreamKind,
} from './websocket';
