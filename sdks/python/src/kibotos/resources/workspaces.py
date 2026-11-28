"""Workspaces resource for sync and async Kibotos clients."""

from __future__ import annotations

import math
import time
from collections.abc import (
    AsyncGenerator,
    AsyncIterable,
    Generator,
    Iterable,
    Iterator,
    Mapping,
    Sequence,
)
from contextlib import AbstractAsyncContextManager, AbstractContextManager
from datetime import datetime
from typing import Any, Generic, Literal, TypeVar, overload
from urllib.parse import urlencode

import httpx

from .._sse import ExecEventParser
from .._transport import (
    UNSET,
    AsyncTransport,
    SyncTransport,
    _UnsetType,
    encode_path_segment,
    query_params,
    serialize_labels,
    to_payload,
)
from ..errors import WaitTimeoutError
from ..models import (
    ExecEvent,
    ExecRequest,
    ExecResponse,
    FileEntry,
    FileList,
    FileWriteResult,
    NetworkEgressRule,
    NetworkResolverRule,
    Workspace,
    WorkspaceMetrics,
    WorkspaceNetworkInput,
    WorkspacePage,
    WorkspaceResources,
    WorkspaceTemplateRef,
    WorkspaceTimeoutSpec,
    WorkspaceVolume,
    WorkspaceVolumeInput,
)
from ..websocket import web_socket_url
from ._network import config_body, metadata_body

MAX_SERVER_WAIT_SECONDS = 60
_NO_STREAM_TIMEOUT = httpx.Timeout(None)

_VolumeInput = WorkspaceVolumeInput | WorkspaceVolume | Mapping[str, Any]
_NetworkInput = WorkspaceNetworkInput | Mapping[str, Any]
_Transport = TypeVar("_Transport", SyncTransport, AsyncTransport)


def _workspace_path(workspace_id: str) -> str:
    return f"/workspaces/{encode_path_segment(workspace_id)}"


def _network_body(network: _NetworkInput) -> dict[str, Any]:
    body = to_payload(network)
    body.pop("applied", None)
    return body


def _file_read_headers(range: str | None, if_none_match: str | None) -> dict[str, str]:
    headers = {"Accept": "application/octet-stream"}
    if range is not None:
        headers["Range"] = range
    if if_none_match is not None:
        headers["If-None-Match"] = if_none_match
    return headers


def _metrics_params(start: str | datetime | None, end: str | datetime | None) -> list[tuple[str, str]]:
    for value in (start, end):
        if isinstance(value, datetime) and value.utcoffset() is None:
            raise ValueError("Metrics start and end datetimes must be timezone-aware")
    return query_params(
        start=start.isoformat() if isinstance(start, datetime) else start,
        end=end.isoformat() if isinstance(end, datetime) else end,
    )


def _normalize_statuses(status: str | Sequence[str]) -> list[str]:
    if isinstance(status, str):
        statuses = [status]
    else:
        statuses = list(status)
    if not statuses:
        raise ValueError("At least one target status must be provided.")
    return statuses


def _build_create_body(
    *,
    template: str | WorkspaceTemplateRef | Mapping[str, Any],
    name: str | None,
    zone: str | None,
    resources: WorkspaceResources | Mapping[str, Any] | None,
    network: _NetworkInput | None,
    volume: _VolumeInput | None,
    volumes: Sequence[_VolumeInput] | None,
    packs: Sequence[str] | None,
    timeout: WorkspaceTimeoutSpec | Mapping[str, Any] | None,
    labels: Mapping[str, str] | None,
) -> dict[str, Any]:
    template_payload = {"id": template} if isinstance(template, str) else to_payload(template)
    body: dict[str, Any] = {"template": template_payload}
    if name is not None:
        body["name"] = name
    if zone is not None:
        body["zone"] = zone
    if resources is not None:
        body["resources"] = to_payload(resources)
    if network is not None:
        body["network"] = _network_body(network)
    if volume is not None:
        body["volume"] = to_payload(volume)
    if volumes is not None:
        body["volumes"] = [to_payload(attachment) for attachment in volumes]
    if packs is not None:
        body["packs"] = list(packs)
    if timeout is not None:
        body["timeout"] = to_payload(timeout)
    if labels is not None:
        body["labels"] = dict(labels)
    return body


def _list_params(
    *,
    status: str | None,
    zone: str | None,
    labels: Mapping[str, str] | None,
    limit: int | None,
    page_token: str | None,
) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if status is not None:
        params.append(("status", status))
    if zone is not None:
        params.append(("zone", zone))
    params.extend(serialize_labels(labels))
    if limit is not None:
        params.append(("limit", str(limit)))
    if page_token is not None:
        params.append(("page_token", page_token))
    return params


class _WorkspaceURLs(Generic[_Transport]):
    def __init__(self, transport: _Transport) -> None:
        self._transport = transport

    def ws_url(self, workspace_id: str) -> str:
        return web_socket_url(f"{self._transport.base_url}{_workspace_path(workspace_id)}/ws")

    def ws_headers(self) -> dict[str, str]:
        """Return bearer headers for an upgrade or an authenticated proxy request."""
        return {"Authorization": f"Bearer {self._transport.api_key}"}

    def tunnel_ws_url(
        self,
        workspace_id: str,
        *,
        port: int | None = None,
        family: Literal["ipv4", "ipv6"] | None = None,
        unix_path: str | None = None,
    ) -> str:
        if (port is None) == (unix_path is None) or (port is None and family is not None):
            raise ValueError("Specify either port (with optional family) or unix_path")
        query = urlencode(query_params(port=port, family=family, unix_path=unix_path))
        return web_socket_url(
            f"{self._transport.base_url}{_workspace_path(workspace_id)}/tunnels/ws?{query}"
        )

    def desktop_vnc_url(self, workspace_id: str) -> str:
        return web_socket_url(
            f"{self._transport.base_url}{_workspace_path(workspace_id)}/desktop/vnc"
        )

    def proxy_url(
        self,
        workspace_id: str,
        port: int,
        path: str = "",
        *,
        query: Mapping[str, str] | Sequence[tuple[str, str]] | None = None,
    ) -> str:
        if not 1 <= port <= 65535:
            raise ValueError("port must be between 1 and 65535")
        segments = path.removeprefix("/").split("/")
        if any(segment in {".", ".."} for segment in segments):
            raise ValueError("Proxy path must not contain dot segments")
        url = f"{self._transport.base_url}{_workspace_path(workspace_id)}/proxy/{port}"
        if path:
            url += "/" + "/".join(encode_path_segment(segment) for segment in segments)
        if query:
            url += "?" + urlencode(query)
        return url

    def proxy_ws_url(
        self,
        workspace_id: str,
        port: int,
        path: str = "",
        *,
        query: Mapping[str, str] | Sequence[tuple[str, str]] | None = None,
    ) -> str:
        return web_socket_url(self.proxy_url(workspace_id, port, path, query=query))


class WorkspacesResource(_WorkspaceURLs[SyncTransport]):
    """Synchronous interface for `/v1/workspaces`."""

    def create(
        self,
        *,
        template: str | WorkspaceTemplateRef | Mapping[str, Any],
        name: str | None = None,
        zone: str | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
        network: _NetworkInput | None = None,
        volume: _VolumeInput | None = None,
        volumes: Sequence[_VolumeInput] | None = None,
        packs: Sequence[str] | None = None,
        timeout: WorkspaceTimeoutSpec | Mapping[str, Any] | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Create a new workspace (async, returns HTTP 202 Workspace)."""
        body = _build_create_body(
            template=template,
            name=name,
            zone=zone,
            resources=resources,
            network=network,
            volume=volume,
            volumes=volumes,
            packs=packs,
            timeout=timeout,
            labels=labels,
        )
        data = self._transport.request(
            "POST",
            "/workspaces",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    def list(
        self,
        *,
        status: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> WorkspacePage:
        """List workspaces across zones."""
        data = self._transport.request(
            "GET",
            "/workspaces",
            params=_list_params(
                status=status,
                zone=zone,
                labels=labels,
                limit=limit,
                page_token=page_token,
            ),
        )
        return WorkspacePage.model_validate(data)

    def iterate(
        self,
        *,
        status: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
    ) -> Iterator[Workspace]:
        """Iterate across all pages of workspaces."""
        page_token: str | None = None
        while True:
            page = self.list(
                status=status,
                zone=zone,
                labels=labels,
                limit=limit,
                page_token=page_token,
            )
            yield from page.data
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    def get(self, workspace_id: str) -> Workspace:
        """Fetch a single workspace by ID."""
        data = self._transport.request(
            "GET",
            f"/workspaces/{encode_path_segment(workspace_id)}",
        )
        return Workspace.model_validate(data)

    def update(
        self,
        workspace_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
    ) -> Workspace:
        """Update metadata and resize eligible stopped or failed workspaces."""
        body = metadata_body(name=name, labels=labels)
        if resources is not None:
            body["resources"] = to_payload(resources)
        data = self._transport.request(
            "PATCH", _workspace_path(workspace_id), json_body=body,
        )
        return Workspace.model_validate(data)

    def delete(self, workspace_id: str) -> Workspace:
        """Delete a workspace (async, returns HTTP 202 Workspace)."""
        data = self._transport.request(
            "DELETE",
            f"/workspaces/{encode_path_segment(workspace_id)}",
        )
        return Workspace.model_validate(data)

    def start(
        self,
        workspace_id: str,
        *,
        timeout: WorkspaceTimeoutSpec | Mapping[str, Any] | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Start a stopped workspace."""
        body: dict[str, Any] | None = (
            {"timeout": to_payload(timeout)} if timeout is not None else None
        )
        data = self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/start",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    def stop(
        self,
        workspace_id: str,
        *,
        force: bool | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Stop an available workspace."""
        body: dict[str, Any] | None = {"force": force} if force is not None else None
        data = self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/stop",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    def set_timeout(
        self,
        workspace_id: str,
        *,
        seconds: int,
        action: str | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Update or extend a workspace's timeout."""
        body: dict[str, Any] = {"seconds": seconds}
        if action is not None:
            body["action"] = action
        data = self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/timeout",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    def set_network(
        self,
        workspace_id: str,
        *,
        mode: str,
        allow: Sequence[str] | None = None,
        deny: Sequence[str] | None = None,
        egresses: Sequence[NetworkEgressRule | Mapping[str, Any]] | None = None,
        resolvers: Sequence[NetworkResolverRule | Mapping[str, Any]] | None = None,
        drain_seconds: int | None = None,
    ) -> Workspace:
        """Replace a workspace's outbound network policy."""
        body = config_body(
            mode=mode,
            allow=list(allow) if allow is not None else None,
            deny=list(deny) if deny is not None else None,
            egresses=[to_payload(rule) for rule in egresses] if egresses is not None else None,
            resolvers=[to_payload(rule) for rule in resolvers] if resolvers is not None else None,
            drain_seconds=drain_seconds,
        )
        data = self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/network", json_body=body,
        )
        return Workspace.model_validate(data)

    def set_volumes(self, workspace_id: str, *, volumes: Sequence[_VolumeInput]) -> Workspace:
        """Replace all volume bindings. An empty list detaches all volumes."""
        data = self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/volumes",
            json_body={"volumes": [to_payload(attachment) for attachment in volumes]},
        )
        return Workspace.model_validate(data)

    def set_packs(self, workspace_id: str, *, packs: Sequence[str]) -> Workspace:
        """Replace the Pack list, effective on the next start."""
        data = self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/packs", json_body={"packs": list(packs)},
        )
        return Workspace.model_validate(data)

    def get_metrics(
        self,
        workspace_id: str,
        *,
        start: str | datetime | None = None,
        end: str | datetime | None = None,
    ) -> WorkspaceMetrics:
        data = self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/metrics", params=_metrics_params(start, end),
        )
        return WorkspaceMetrics.model_validate(data)

    def attach_volume(
        self,
        workspace_id: str,
        *,
        volume_id: str,
        mount_path: str,
        delete_with_workspace: bool = False,
    ) -> Workspace:
        """Attach a persistent volume to a workspace."""
        data = self._transport.request(
            "PUT",
            f"/workspaces/{encode_path_segment(workspace_id)}/volume",
            json_body={
                "id": volume_id,
                "mount_path": mount_path,
                "delete_with_workspace": delete_with_workspace,
            },
        )
        return Workspace.model_validate(data)

    def detach_volume(self, workspace_id: str) -> Workspace:
        """Detach the attached persistent volume from a workspace."""
        data = self._transport.request(
            "DELETE",
            f"/workspaces/{encode_path_segment(workspace_id)}/volume",
        )
        return Workspace.model_validate(data)

    def exec(
        self,
        workspace_id: str,
        request: ExecRequest | Mapping[str, Any],
        *,
        timeout: float | httpx.Timeout | None = None,
    ) -> ExecResponse:
        """Run a command and wait for its JSON result."""
        payload = to_payload(request)
        if timeout is None:
            timeout = payload.get("timeout_seconds", 30) + 15
        data = self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/exec", json_body=payload,
            timeout=timeout, replayable=False, use_idempotency=False,
        )
        return ExecResponse.model_validate(data)

    def exec_stream(
        self,
        workspace_id: str,
        request: ExecRequest | Mapping[str, Any],
        *,
        timeout: float | httpx.Timeout | None = None,
    ) -> Generator[ExecEvent, None, None]:
        """Yield server-sent events for a command."""
        parser = ExecEventParser()
        with self._transport.stream(
            "POST", f"{_workspace_path(workspace_id)}/exec", json_body=to_payload(request),
            headers={"Accept": "text/event-stream"},
            timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT,
        ) as response:
            response.encoding = "utf-8"
            for chunk in response.iter_text():
                yield from parser.feed(chunk)

    def list_files(
        self, workspace_id: str, path: str, *, depth: int | None = None, user: str | None = None,
    ) -> FileList:
        data = self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/files",
            params=query_params(path=path, depth=depth, user=user),
        )
        return FileList.model_validate(data)

    def stat_file(self, workspace_id: str, path: str, *, user: str | None = None) -> FileEntry:
        data = self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/files/stat",
            params=query_params(path=path, user=user),
        )
        return FileEntry.model_validate(data)

    @overload
    def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: Literal[False] = False,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes: ...

    @overload
    def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: Literal[True],
        timeout: float | httpx.Timeout | None = None,
    ) -> AbstractContextManager[httpx.Response]: ...

    @overload
    def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: bool,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes | AbstractContextManager[httpx.Response]: ...

    def read_file(
        self,
        workspace_id: str,
        path: str,
        *,
        user: str | None = None,
        range: str | None = None,
        if_none_match: str | None = None,
        stream: bool = False,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes | AbstractContextManager[httpx.Response]:
        """Read bytes or return a context manager for a streaming response."""
        response = self._transport.stream(
            "GET", f"{_workspace_path(workspace_id)}/files/content",
            params=query_params(path=path, user=user),
            headers=_file_read_headers(range, if_none_match),
            timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT, success_statuses=(304,),
        )
        if stream:
            return response
        with response as result:
            return result.read()

    def write_file(
        self,
        workspace_id: str,
        path: str,
        content: bytes | Iterable[bytes],
        *,
        mode: str | None = None,
        create_only: bool | None = None,
        user: str | None = None,
        timeout: float | httpx.Timeout | None = None,
    ) -> FileWriteResult:
        """Write raw file content, optionally from a stream of bytes."""
        data = self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/files/content",
            params=query_params(path=path, mode=mode, create_only=create_only, user=user),
            content=content, timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT,
            replayable=False,
        )
        return FileWriteResult.model_validate(data)

    def make_dir(
        self, workspace_id: str, path: str, *, mode: str | None = None, user: str | None = None,
    ) -> FileEntry:
        data = self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/files/mkdir",
            json_body=config_body(path=path, mode=mode, user=user),
            replayable=False, use_idempotency=False,
        )
        return FileEntry.model_validate(data)

    def move_file(
        self, workspace_id: str, source: str, destination: str, *, user: str | None = None,
    ) -> FileEntry:
        data = self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/files/move",
            json_body=config_body(source=source, destination=destination, user=user),
            replayable=False, use_idempotency=False,
        )
        return FileEntry.model_validate(data)

    def delete_file(self, workspace_id: str, path: str, *, user: str | None = None) -> None:
        self._transport.request(
            "DELETE", f"{_workspace_path(workspace_id)}/files",
            params=query_params(path=path, user=user), replayable=False,
        )

    def wait(
        self,
        workspace_id: str,
        *,
        status: str | Sequence[str],
        timeout: float = 30.0,
    ) -> Workspace:
        """Wait for a workspace to reach one of the target statuses.

        Supports timeouts greater than 60 seconds by looping server wait chunks of <= 60s.
        """
        statuses = _normalize_statuses(status)
        if timeout <= 0:
            raise ValueError("timeout must be positive.")

        deadline = time.monotonic() + timeout
        logical_remaining = float(timeout)
        last_error: WaitTimeoutError | None = None
        path = f"/workspaces/{encode_path_segment(workspace_id)}/wait"

        while True:
            wall_remaining = deadline - time.monotonic()
            remaining = min(logical_remaining, wall_remaining)
            if remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise WaitTimeoutError(
                    f"Timed out waiting for workspace {workspace_id} to reach {statuses}",
                    status=408,
                    code="wait_timeout",
                    retryable=True,
                )

            chunk = min(MAX_SERVER_WAIT_SECONDS, max(1, math.ceil(remaining)))
            params: list[tuple[str, str]] = [("status", s) for s in statuses]
            params.append(("timeout", str(chunk)))

            try:
                data = self._transport.request(
                    "GET",
                    path,
                    params=params,
                    timeout=max(float(chunk) + 10.0, 15.0),
                )
                return Workspace.model_validate(data)
            except WaitTimeoutError as exc:
                last_error = exc
                logical_remaining -= chunk


class AsyncWorkspacesResource(_WorkspaceURLs[AsyncTransport]):
    """Asynchronous interface for `/v1/workspaces`."""

    async def create(
        self,
        *,
        template: str | WorkspaceTemplateRef | Mapping[str, Any],
        name: str | None = None,
        zone: str | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
        network: _NetworkInput | None = None,
        volume: _VolumeInput | None = None,
        volumes: Sequence[_VolumeInput] | None = None,
        packs: Sequence[str] | None = None,
        timeout: WorkspaceTimeoutSpec | Mapping[str, Any] | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Create a new workspace (async, returns HTTP 202 Workspace)."""
        body = _build_create_body(
            template=template,
            name=name,
            zone=zone,
            resources=resources,
            network=network,
            volume=volume,
            volumes=volumes,
            packs=packs,
            timeout=timeout,
            labels=labels,
        )
        data = await self._transport.request(
            "POST",
            "/workspaces",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    async def list(
        self,
        *,
        status: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> WorkspacePage:
        """List workspaces across zones."""
        data = await self._transport.request(
            "GET",
            "/workspaces",
            params=_list_params(
                status=status,
                zone=zone,
                labels=labels,
                limit=limit,
                page_token=page_token,
            ),
        )
        return WorkspacePage.model_validate(data)

    async def iterate(
        self,
        *,
        status: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[Workspace]:
        """Iterate asynchronously across all pages of workspaces."""
        page_token: str | None = None
        while True:
            page = await self.list(
                status=status,
                zone=zone,
                labels=labels,
                limit=limit,
                page_token=page_token,
            )
            for item in page.data:
                yield item
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    async def get(self, workspace_id: str) -> Workspace:
        """Fetch a single workspace by ID."""
        data = await self._transport.request(
            "GET",
            f"/workspaces/{encode_path_segment(workspace_id)}",
        )
        return Workspace.model_validate(data)

    async def update(
        self,
        workspace_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
    ) -> Workspace:
        """Update metadata and resize eligible stopped or failed workspaces."""
        body = metadata_body(name=name, labels=labels)
        if resources is not None:
            body["resources"] = to_payload(resources)
        data = await self._transport.request(
            "PATCH", _workspace_path(workspace_id), json_body=body,
        )
        return Workspace.model_validate(data)

    async def delete(self, workspace_id: str) -> Workspace:
        """Delete a workspace (async, returns HTTP 202 Workspace)."""
        data = await self._transport.request(
            "DELETE",
            f"/workspaces/{encode_path_segment(workspace_id)}",
        )
        return Workspace.model_validate(data)

    async def start(
        self,
        workspace_id: str,
        *,
        timeout: WorkspaceTimeoutSpec | Mapping[str, Any] | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Start a stopped workspace."""
        body: dict[str, Any] | None = (
            {"timeout": to_payload(timeout)} if timeout is not None else None
        )
        data = await self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/start",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    async def stop(
        self,
        workspace_id: str,
        *,
        force: bool | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Stop an available workspace."""
        body: dict[str, Any] | None = {"force": force} if force is not None else None
        data = await self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/stop",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    async def set_timeout(
        self,
        workspace_id: str,
        *,
        seconds: int,
        action: str | None = None,
        idempotency_key: str | None = None,
    ) -> Workspace:
        """Update or extend a workspace's timeout."""
        body: dict[str, Any] = {"seconds": seconds}
        if action is not None:
            body["action"] = action
        data = await self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/timeout",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Workspace.model_validate(data)

    async def set_network(
        self,
        workspace_id: str,
        *,
        mode: str,
        allow: Sequence[str] | None = None,
        deny: Sequence[str] | None = None,
        egresses: Sequence[NetworkEgressRule | Mapping[str, Any]] | None = None,
        resolvers: Sequence[NetworkResolverRule | Mapping[str, Any]] | None = None,
        drain_seconds: int | None = None,
    ) -> Workspace:
        """Replace a workspace's outbound network policy."""
        body = config_body(
            mode=mode,
            allow=list(allow) if allow is not None else None,
            deny=list(deny) if deny is not None else None,
            egresses=[to_payload(rule) for rule in egresses] if egresses is not None else None,
            resolvers=[to_payload(rule) for rule in resolvers] if resolvers is not None else None,
            drain_seconds=drain_seconds,
        )
        data = await self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/network", json_body=body,
        )
        return Workspace.model_validate(data)

    async def set_volumes(self, workspace_id: str, *, volumes: Sequence[_VolumeInput]) -> Workspace:
        """Replace all volume bindings. An empty list detaches all volumes."""
        data = await self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/volumes",
            json_body={"volumes": [to_payload(attachment) for attachment in volumes]},
        )
        return Workspace.model_validate(data)

    async def set_packs(self, workspace_id: str, *, packs: Sequence[str]) -> Workspace:
        """Replace the Pack list, effective on the next start."""
        data = await self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/packs", json_body={"packs": list(packs)},
        )
        return Workspace.model_validate(data)

    async def get_metrics(
        self,
        workspace_id: str,
        *,
        start: str | datetime | None = None,
        end: str | datetime | None = None,
    ) -> WorkspaceMetrics:
        data = await self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/metrics", params=_metrics_params(start, end),
        )
        return WorkspaceMetrics.model_validate(data)

    async def attach_volume(
        self,
        workspace_id: str,
        *,
        volume_id: str,
        mount_path: str,
        delete_with_workspace: bool = False,
    ) -> Workspace:
        """Attach a persistent volume to a workspace."""
        data = await self._transport.request(
            "PUT",
            f"/workspaces/{encode_path_segment(workspace_id)}/volume",
            json_body={
                "id": volume_id,
                "mount_path": mount_path,
                "delete_with_workspace": delete_with_workspace,
            },
        )
        return Workspace.model_validate(data)

    async def detach_volume(self, workspace_id: str) -> Workspace:
        """Detach the attached persistent volume from a workspace."""
        data = await self._transport.request(
            "DELETE",
            f"/workspaces/{encode_path_segment(workspace_id)}/volume",
        )
        return Workspace.model_validate(data)

    async def exec(
        self,
        workspace_id: str,
        request: ExecRequest | Mapping[str, Any],
        *,
        timeout: float | httpx.Timeout | None = None,
    ) -> ExecResponse:
        """Run a command and wait for its JSON result."""
        payload = to_payload(request)
        if timeout is None:
            timeout = payload.get("timeout_seconds", 30) + 15
        data = await self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/exec", json_body=payload,
            timeout=timeout, replayable=False, use_idempotency=False,
        )
        return ExecResponse.model_validate(data)

    async def exec_stream(
        self,
        workspace_id: str,
        request: ExecRequest | Mapping[str, Any],
        *,
        timeout: float | httpx.Timeout | None = None,
    ) -> AsyncGenerator[ExecEvent, None]:
        """Yield server-sent events for a command."""
        parser = ExecEventParser()
        async with self._transport.stream(
            "POST", f"{_workspace_path(workspace_id)}/exec", json_body=to_payload(request),
            headers={"Accept": "text/event-stream"},
            timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT,
        ) as response:
            response.encoding = "utf-8"
            async for chunk in response.aiter_text():
                for event in parser.feed(chunk):
                    yield event

    async def list_files(
        self, workspace_id: str, path: str, *, depth: int | None = None, user: str | None = None,
    ) -> FileList:
        data = await self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/files",
            params=query_params(path=path, depth=depth, user=user),
        )
        return FileList.model_validate(data)

    async def stat_file(self, workspace_id: str, path: str, *, user: str | None = None) -> FileEntry:
        data = await self._transport.request(
            "GET", f"{_workspace_path(workspace_id)}/files/stat",
            params=query_params(path=path, user=user),
        )
        return FileEntry.model_validate(data)

    @overload
    async def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: Literal[False] = False,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes: ...

    @overload
    async def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: Literal[True],
        timeout: float | httpx.Timeout | None = None,
    ) -> AbstractAsyncContextManager[httpx.Response]: ...

    @overload
    async def read_file(
        self, workspace_id: str, path: str, *, user: str | None = None, range: str | None = None,
        if_none_match: str | None = None, stream: bool,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes | AbstractAsyncContextManager[httpx.Response]: ...

    async def read_file(
        self,
        workspace_id: str,
        path: str,
        *,
        user: str | None = None,
        range: str | None = None,
        if_none_match: str | None = None,
        stream: bool = False,
        timeout: float | httpx.Timeout | None = None,
    ) -> bytes | AbstractAsyncContextManager[httpx.Response]:
        """Read bytes or return a context manager for a streaming response."""
        response = self._transport.stream(
            "GET", f"{_workspace_path(workspace_id)}/files/content",
            params=query_params(path=path, user=user),
            headers=_file_read_headers(range, if_none_match),
            timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT, success_statuses=(304,),
        )
        if stream:
            return response
        async with response as result:
            return await result.aread()

    async def write_file(
        self,
        workspace_id: str,
        path: str,
        content: bytes | AsyncIterable[bytes],
        *,
        mode: str | None = None,
        create_only: bool | None = None,
        user: str | None = None,
        timeout: float | httpx.Timeout | None = None,
    ) -> FileWriteResult:
        """Write raw file content, optionally from a stream of bytes."""
        data = await self._transport.request(
            "PUT", f"{_workspace_path(workspace_id)}/files/content",
            params=query_params(path=path, mode=mode, create_only=create_only, user=user),
            content=content, timeout=timeout if timeout is not None else _NO_STREAM_TIMEOUT,
            replayable=False,
        )
        return FileWriteResult.model_validate(data)

    async def make_dir(
        self, workspace_id: str, path: str, *, mode: str | None = None, user: str | None = None,
    ) -> FileEntry:
        data = await self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/files/mkdir",
            json_body=config_body(path=path, mode=mode, user=user),
            replayable=False, use_idempotency=False,
        )
        return FileEntry.model_validate(data)

    async def move_file(
        self, workspace_id: str, source: str, destination: str, *, user: str | None = None,
    ) -> FileEntry:
        data = await self._transport.request(
            "POST", f"{_workspace_path(workspace_id)}/files/move",
            json_body=config_body(source=source, destination=destination, user=user),
            replayable=False, use_idempotency=False,
        )
        return FileEntry.model_validate(data)

    async def delete_file(self, workspace_id: str, path: str, *, user: str | None = None) -> None:
        await self._transport.request(
            "DELETE", f"{_workspace_path(workspace_id)}/files",
            params=query_params(path=path, user=user), replayable=False,
        )

    async def wait(
        self,
        workspace_id: str,
        *,
        status: str | Sequence[str],
        timeout: float = 30.0,
    ) -> Workspace:
        """Wait asynchronously for a workspace to reach one of the target statuses.

        Supports timeouts greater than 60 seconds by looping server wait chunks of <= 60s.
        """
        statuses = _normalize_statuses(status)
        if timeout <= 0:
            raise ValueError("timeout must be positive.")

        deadline = time.monotonic() + timeout
        logical_remaining = float(timeout)
        last_error: WaitTimeoutError | None = None
        path = f"/workspaces/{encode_path_segment(workspace_id)}/wait"

        while True:
            wall_remaining = deadline - time.monotonic()
            remaining = min(logical_remaining, wall_remaining)
            if remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise WaitTimeoutError(
                    f"Timed out waiting for workspace {workspace_id} to reach {statuses}",
                    status=408,
                    code="wait_timeout",
                    retryable=True,
                )

            chunk = min(MAX_SERVER_WAIT_SECONDS, max(1, math.ceil(remaining)))
            params: list[tuple[str, str]] = [("status", s) for s in statuses]
            params.append(("timeout", str(chunk)))

            try:
                data = await self._transport.request(
                    "GET",
                    path,
                    params=params,
                    timeout=max(float(chunk) + 10.0, 15.0),
                )
                return Workspace.model_validate(data)
            except WaitTimeoutError as exc:
                last_error = exc
                logical_remaining -= chunk
