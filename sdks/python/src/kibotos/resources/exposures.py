"""Exposures resource for sync and async Kibotos clients."""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator
from typing import Any

from .._transport import AsyncTransport, SyncTransport, encode_path_segment
from ..models import Exposure, ExposurePage


def _list_params(
    *,
    include_revoked: bool | None,
    limit: int | None,
    page_token: str | None,
) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if include_revoked is not None:
        params.append(("include_revoked", "true" if include_revoked else "false"))
    if limit is not None:
        params.append(("limit", str(limit)))
    if page_token is not None:
        params.append(("page_token", page_token))
    return params


class ExposuresResource:
    """Synchronous interface for `/v1/workspaces/{id}/exposures`."""

    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def create(
        self,
        workspace_id: str,
        *,
        port: int,
        ttl_seconds: int | None = None,
        idempotency_key: str | None = None,
    ) -> Exposure:
        """Expose a workspace port on an HTTPS URL."""
        body: dict[str, Any] = {"port": port}
        if ttl_seconds is not None:
            body["ttl_seconds"] = ttl_seconds
        data = self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/exposures",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Exposure.model_validate(data)

    def list(
        self,
        workspace_id: str,
        *,
        include_revoked: bool | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ExposurePage:
        """List exposures for a workspace."""
        data = self._transport.request(
            "GET",
            f"/workspaces/{encode_path_segment(workspace_id)}/exposures",
            params=_list_params(
                include_revoked=include_revoked,
                limit=limit,
                page_token=page_token,
            ),
        )
        return ExposurePage.model_validate(data)

    def iterate(
        self,
        workspace_id: str,
        *,
        include_revoked: bool | None = None,
        limit: int | None = None,
    ) -> Iterator[Exposure]:
        """Iterate across all pages of exposures for a workspace."""
        page_token: str | None = None
        while True:
            page = self.list(
                workspace_id,
                include_revoked=include_revoked,
                limit=limit,
                page_token=page_token,
            )
            yield from page.data
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    def get(self, workspace_id: str, exposure_id: str) -> Exposure:
        """Fetch a single exposure by ID."""
        data = self._transport.request(
            "GET",
            (
                f"/workspaces/{encode_path_segment(workspace_id)}"
                f"/exposures/{encode_path_segment(exposure_id)}"
            ),
        )
        return Exposure.model_validate(data)

    def revoke(self, workspace_id: str, exposure_id: str) -> Exposure:
        """Revoke an active exposure."""
        data = self._transport.request(
            "DELETE",
            (
                f"/workspaces/{encode_path_segment(workspace_id)}"
                f"/exposures/{encode_path_segment(exposure_id)}"
            ),
        )
        return Exposure.model_validate(data)


class AsyncExposuresResource:
    """Asynchronous interface for `/v1/workspaces/{id}/exposures`."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def create(
        self,
        workspace_id: str,
        *,
        port: int,
        ttl_seconds: int | None = None,
        idempotency_key: str | None = None,
    ) -> Exposure:
        """Expose a workspace port on an HTTPS URL."""
        body: dict[str, Any] = {"port": port}
        if ttl_seconds is not None:
            body["ttl_seconds"] = ttl_seconds
        data = await self._transport.request(
            "POST",
            f"/workspaces/{encode_path_segment(workspace_id)}/exposures",
            json_body=body,
            idempotency_key=idempotency_key,
        )
        return Exposure.model_validate(data)

    async def list(
        self,
        workspace_id: str,
        *,
        include_revoked: bool | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ExposurePage:
        """List exposures for a workspace."""
        data = await self._transport.request(
            "GET",
            f"/workspaces/{encode_path_segment(workspace_id)}/exposures",
            params=_list_params(
                include_revoked=include_revoked,
                limit=limit,
                page_token=page_token,
            ),
        )
        return ExposurePage.model_validate(data)

    async def iterate(
        self,
        workspace_id: str,
        *,
        include_revoked: bool | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[Exposure]:
        """Iterate asynchronously across all pages of exposures for a workspace."""
        page_token: str | None = None
        while True:
            page = await self.list(
                workspace_id,
                include_revoked=include_revoked,
                limit=limit,
                page_token=page_token,
            )
            for item in page.data:
                yield item
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    async def get(self, workspace_id: str, exposure_id: str) -> Exposure:
        """Fetch a single exposure by ID."""
        data = await self._transport.request(
            "GET",
            (
                f"/workspaces/{encode_path_segment(workspace_id)}"
                f"/exposures/{encode_path_segment(exposure_id)}"
            ),
        )
        return Exposure.model_validate(data)

    async def revoke(self, workspace_id: str, exposure_id: str) -> Exposure:
        """Revoke an active exposure."""
        data = await self._transport.request(
            "DELETE",
            (
                f"/workspaces/{encode_path_segment(workspace_id)}"
                f"/exposures/{encode_path_segment(exposure_id)}"
            ),
        )
        return Exposure.model_validate(data)
