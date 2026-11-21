"""Zones resource for sync and async Kibotos clients."""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator

from .._transport import AsyncTransport, SyncTransport
from ..models import Zone, ZonePage


def _list_params(*, limit: int | None, page_token: str | None) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if limit is not None:
        params.append(("limit", str(limit)))
    if page_token is not None:
        params.append(("page_token", page_token))
    return params


class ZonesResource:
    """Synchronous interface for `/v1/zones`."""

    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def list(
        self,
        *,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ZonePage:
        """List available zones."""
        data = self._transport.request(
            "GET",
            "/zones",
            params=_list_params(limit=limit, page_token=page_token),
        )
        return ZonePage.model_validate(data)

    def iterate(self, *, limit: int | None = None) -> Iterator[Zone]:
        """Iterate across all pages of zones."""
        page_token: str | None = None
        while True:
            page = self.list(limit=limit, page_token=page_token)
            yield from page.data
            if not page.next_page_token:
                break
            page_token = page.next_page_token


class AsyncZonesResource:
    """Asynchronous interface for `/v1/zones`."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def list(
        self,
        *,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ZonePage:
        """List available zones."""
        data = await self._transport.request(
            "GET",
            "/zones",
            params=_list_params(limit=limit, page_token=page_token),
        )
        return ZonePage.model_validate(data)

    async def iterate(self, *, limit: int | None = None) -> AsyncIterator[Zone]:
        """Iterate asynchronously across all pages of zones."""
        page_token: str | None = None
        while True:
            page = await self.list(limit=limit, page_token=page_token)
            for item in page.data:
                yield item
            if not page.next_page_token:
                break
            page_token = page.next_page_token
