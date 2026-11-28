"""Framing and URL helpers for Kibotos WebSocket clients."""

from __future__ import annotations

import json
from collections.abc import Mapping
from enum import IntEnum
from struct import Struct
from typing import Any, NamedTuple
from urllib.parse import urlsplit, urlunsplit

_FRAME_VERSION = 0x01
_HEADER = Struct(">BIB")


class WorkspaceChannel(IntEnum):
    STDIN = 0
    STDOUT = 1
    STDERR = 2
    PTY = 3
    TUNNEL = 4


class WorkspaceBinaryFrame(NamedTuple):
    stream_id: int
    channel: WorkspaceChannel
    payload: bytes


def _validate_stream_id(stream_id: int) -> None:
    if isinstance(stream_id, bool) or not isinstance(stream_id, int) or not 1 <= stream_id <= 0xFFFFFFFF:
        raise ValueError("stream_id must be a nonzero unsigned 32-bit integer")


def encode_open_frame(
    stream_id: int, kind: str, params: Mapping[str, Any] | None = None,
) -> str:
    """Encode a workspace stream open request as a JSON text frame."""
    _validate_stream_id(stream_id)
    return json.dumps({
        "type": "open", "stream_id": stream_id, "kind": kind,
        "params": {} if params is None else dict(params),
    }, allow_nan=False)


def encode_binary_frame(stream_id: int, channel: int, payload: bytes) -> bytes:
    """Encode the six-byte v1 header followed by the payload."""
    _validate_stream_id(stream_id)
    return _HEADER.pack(_FRAME_VERSION, stream_id, WorkspaceChannel(channel)) + payload


def decode_binary_frame(frame: bytes | bytearray | memoryview) -> WorkspaceBinaryFrame:
    """Decode a binary frame and validate its header."""
    data = memoryview(frame).cast("B")
    if len(data) < _HEADER.size:
        raise ValueError("Workspace frame is shorter than 6 bytes")
    version, stream_id, channel = _HEADER.unpack_from(data)
    if version != _FRAME_VERSION:
        raise ValueError("Unsupported workspace frame version")
    _validate_stream_id(stream_id)
    return WorkspaceBinaryFrame(stream_id, WorkspaceChannel(channel), bytes(data[_HEADER.size:]))


def web_socket_url(url: str) -> str:
    target = urlsplit(url)
    scheme = {"http": "ws", "https": "wss"}.get(target.scheme, target.scheme)
    if scheme not in {"ws", "wss"}:
        raise ValueError("A WebSocket URL requires HTTP or HTTPS")
    return urlunsplit(target._replace(scheme=scheme))
