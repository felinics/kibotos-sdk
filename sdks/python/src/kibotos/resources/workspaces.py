"""Workspaces resource for sync and async Kibotos clients."""

from __future__ import annotations

import math
import time
from collections.abc import AsyncIterator, Iterator, Mapping, Sequence
from typing import Any

from .._transport import (
    UNSET,
    AsyncTransport,
    SyncTransport,
    _UnsetType,
    encode_path_segment,
    serialize_labels,
    to_payload,
)
from ..errors import WaitTimeoutError
from ..models import (
    Workspace,
    WorkspaceNetwork,
    WorkspacePage,
    WorkspaceResources,
    WorkspaceTemplateRef,
    WorkspaceTimeoutSpec,
    WorkspaceVolume,
)

MAX_SERVER_WAIT_SECONDS = 60


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
    network: WorkspaceNetwork | Mapping[str, Any] | None,
    volume: WorkspaceVolume | Mapping[str, Any] | None,
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
        body["network"] = to_payload(network)
    if volume is not None:
        body["volume"] = to_payload(volume)
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


def _build_update_body(
    *,
    name: str | None | _UnsetType,
    labels: Mapping[str, str | None] | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {}
    if not isinstance(name, _UnsetType):
        body["name"] = name
    if labels is not None:
        body["labels"] = dict(labels)
    return body


class WorkspacesResource:
    """Synchronous interface for `/v1/workspaces`."""

    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def create(
        self,
        *,
        template: str | WorkspaceTemplateRef | Mapping[str, Any],
        name: str | None = None,
        zone: str | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
        network: WorkspaceNetwork | Mapping[str, Any] | None = None,
        volume: WorkspaceVolume | Mapping[str, Any] | None = None,
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
    ) -> Workspace:
        """Update workspace name and/or labels (pass None value to clear)."""
        data = self._transport.request(
            "PATCH",
            f"/workspaces/{encode_path_segment(workspace_id)}",
            json_body=_build_update_body(name=name, labels=labels),
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

    def set_network(self, workspace_id: str, *, mode: str) -> Workspace:
        """Update a workspace's network policy (`public_internet` or `deny_all`)."""
        data = self._transport.request(
            "PUT",
            f"/workspaces/{encode_path_segment(workspace_id)}/network",
            json_body={"mode": mode},
        )
        return Workspace.model_validate(data)

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


class AsyncWorkspacesResource:
    """Asynchronous interface for `/v1/workspaces`."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def create(
        self,
        *,
        template: str | WorkspaceTemplateRef | Mapping[str, Any],
        name: str | None = None,
        zone: str | None = None,
        resources: WorkspaceResources | Mapping[str, Any] | None = None,
        network: WorkspaceNetwork | Mapping[str, Any] | None = None,
        volume: WorkspaceVolume | Mapping[str, Any] | None = None,
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
    ) -> Workspace:
        """Update workspace name and/or labels (pass None value to clear)."""
        data = await self._transport.request(
            "PATCH",
            f"/workspaces/{encode_path_segment(workspace_id)}",
            json_body=_build_update_body(name=name, labels=labels),
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

    async def set_network(self, workspace_id: str, *, mode: str) -> Workspace:
        """Update a workspace's network policy (`public_internet` or `deny_all`)."""
        data = await self._transport.request(
            "PUT",
            f"/workspaces/{encode_path_segment(workspace_id)}/network",
            json_body={"mode": mode},
        )
        return Workspace.model_validate(data)

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
