"""Volumes resource for sync and async Kibotos clients."""

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
)
from ..errors import WaitTimeoutError
from ..models import Volume, VolumePage

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
    size_gib: int,
    name: str | None,
    zone: str | None,
    labels: Mapping[str, str] | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {"size_gib": size_gib}
    if name is not None:
        body["name"] = name
    if zone is not None:
        body["zone"] = zone
    if labels is not None:
        body["labels"] = dict(labels)
    return body


def _list_params(
    *,
    zone: str | None,
    attached: bool | None,
    labels: Mapping[str, str] | None,
    limit: int | None,
    page_token: str | None,
) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if zone is not None:
        params.append(("zone", zone))
    if attached is not None:
        params.append(("attached", "true" if attached else "false"))
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


class VolumesResource:
    """Synchronous interface for `/v1/volumes`."""

    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def create(
        self,
        *,
        size_gib: int,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Volume:
        """Create a persistent volume (async, returns HTTP 202 Volume)."""
        data = self._transport.request(
            "POST",
            "/volumes",
            json_body=_build_create_body(
                size_gib=size_gib,
                name=name,
                zone=zone,
                labels=labels,
            ),
            idempotency_key=idempotency_key,
        )
        return Volume.model_validate(data)

    def list(
        self,
        *,
        zone: str | None = None,
        attached: bool | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> VolumePage:
        """List volumes across zones."""
        data = self._transport.request(
            "GET",
            "/volumes",
            params=_list_params(
                zone=zone,
                attached=attached,
                labels=labels,
                limit=limit,
                page_token=page_token,
            ),
        )
        return VolumePage.model_validate(data)

    def iterate(
        self,
        *,
        zone: str | None = None,
        attached: bool | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
    ) -> Iterator[Volume]:
        """Iterate across all pages of volumes."""
        page_token: str | None = None
        while True:
            page = self.list(
                zone=zone,
                attached=attached,
                labels=labels,
                limit=limit,
                page_token=page_token,
            )
            yield from page.data
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    def get(self, volume_id: str) -> Volume:
        """Fetch a single volume by ID."""
        data = self._transport.request(
            "GET",
            f"/volumes/{encode_path_segment(volume_id)}",
        )
        return Volume.model_validate(data)

    def update(
        self,
        volume_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Volume:
        """Update volume name and/or labels (pass None value to clear)."""
        data = self._transport.request(
            "PATCH",
            f"/volumes/{encode_path_segment(volume_id)}",
            json_body=_build_update_body(name=name, labels=labels),
        )
        return Volume.model_validate(data)

    def resize(
        self,
        volume_id: str,
        *,
        size_gib: int,
        idempotency_key: str | None = None,
    ) -> Volume:
        """Resize a persistent volume (async, returns HTTP 202 Volume)."""
        data = self._transport.request(
            "POST",
            f"/volumes/{encode_path_segment(volume_id)}/resize",
            json_body={"size_gib": size_gib},
            idempotency_key=idempotency_key,
        )
        return Volume.model_validate(data)

    def delete(self, volume_id: str) -> Volume:
        """Delete a persistent volume (async, returns HTTP 202 Volume)."""
        data = self._transport.request(
            "DELETE",
            f"/volumes/{encode_path_segment(volume_id)}",
        )
        return Volume.model_validate(data)

    def wait(
        self,
        volume_id: str,
        *,
        status: str | Sequence[str],
        timeout: float = 30.0,
    ) -> Volume:
        """Wait for a volume to reach one of the target statuses.

        Supports timeouts greater than 60 seconds by looping server wait chunks of <= 60s.
        """
        statuses = _normalize_statuses(status)
        if timeout <= 0:
            raise ValueError("timeout must be positive.")

        deadline = time.monotonic() + timeout
        logical_remaining = float(timeout)
        last_error: WaitTimeoutError | None = None
        path = f"/volumes/{encode_path_segment(volume_id)}/wait"

        while True:
            wall_remaining = deadline - time.monotonic()
            remaining = min(logical_remaining, wall_remaining)
            if remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise WaitTimeoutError(
                    f"Timed out waiting for volume {volume_id} to reach {statuses}",
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
                return Volume.model_validate(data)
            except WaitTimeoutError as exc:
                last_error = exc
                logical_remaining -= chunk


class AsyncVolumesResource:
    """Asynchronous interface for `/v1/volumes`."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def create(
        self,
        *,
        size_gib: int,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Volume:
        """Create a persistent volume (async, returns HTTP 202 Volume)."""
        data = await self._transport.request(
            "POST",
            "/volumes",
            json_body=_build_create_body(
                size_gib=size_gib,
                name=name,
                zone=zone,
                labels=labels,
            ),
            idempotency_key=idempotency_key,
        )
        return Volume.model_validate(data)

    async def list(
        self,
        *,
        zone: str | None = None,
        attached: bool | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> VolumePage:
        """List volumes across zones."""
        data = await self._transport.request(
            "GET",
            "/volumes",
            params=_list_params(
                zone=zone,
                attached=attached,
                labels=labels,
                limit=limit,
                page_token=page_token,
            ),
        )
        return VolumePage.model_validate(data)

    async def iterate(
        self,
        *,
        zone: str | None = None,
        attached: bool | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[Volume]:
        """Iterate asynchronously across all pages of volumes."""
        page_token: str | None = None
        while True:
            page = await self.list(
                zone=zone,
                attached=attached,
                labels=labels,
                limit=limit,
                page_token=page_token,
            )
            for item in page.data:
                yield item
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    async def get(self, volume_id: str) -> Volume:
        """Fetch a single volume by ID."""
        data = await self._transport.request(
            "GET",
            f"/volumes/{encode_path_segment(volume_id)}",
        )
        return Volume.model_validate(data)

    async def update(
        self,
        volume_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Volume:
        """Update volume name and/or labels (pass None value to clear)."""
        data = await self._transport.request(
            "PATCH",
            f"/volumes/{encode_path_segment(volume_id)}",
            json_body=_build_update_body(name=name, labels=labels),
        )
        return Volume.model_validate(data)

    async def resize(
        self,
        volume_id: str,
        *,
        size_gib: int,
        idempotency_key: str | None = None,
    ) -> Volume:
        """Resize a persistent volume (async, returns HTTP 202 Volume)."""
        data = await self._transport.request(
            "POST",
            f"/volumes/{encode_path_segment(volume_id)}/resize",
            json_body={"size_gib": size_gib},
            idempotency_key=idempotency_key,
        )
        return Volume.model_validate(data)

    async def delete(self, volume_id: str) -> Volume:
        """Delete a persistent volume (async, returns HTTP 202 Volume)."""
        data = await self._transport.request(
            "DELETE",
            f"/volumes/{encode_path_segment(volume_id)}",
        )
        return Volume.model_validate(data)

    async def wait(
        self,
        volume_id: str,
        *,
        status: str | Sequence[str],
        timeout: float = 30.0,
    ) -> Volume:
        """Wait asynchronously for a volume to reach one of the target statuses.

        Supports timeouts greater than 60 seconds by looping server wait chunks of <= 60s.
        """
        statuses = _normalize_statuses(status)
        if timeout <= 0:
            raise ValueError("timeout must be positive.")

        deadline = time.monotonic() + timeout
        logical_remaining = float(timeout)
        last_error: WaitTimeoutError | None = None
        path = f"/volumes/{encode_path_segment(volume_id)}/wait"

        while True:
            wall_remaining = deadline - time.monotonic()
            remaining = min(logical_remaining, wall_remaining)
            if remaining <= 0:
                if last_error is not None:
                    raise last_error
                raise WaitTimeoutError(
                    f"Timed out waiting for volume {volume_id} to reach {statuses}",
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
                return Volume.model_validate(data)
            except WaitTimeoutError as exc:
                last_error = exc
                logical_remaining -= chunk
