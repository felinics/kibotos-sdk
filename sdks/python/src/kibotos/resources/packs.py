"""Platform Pack catalog for Kibotos sync and async clients."""

from __future__ import annotations

from .._transport import AsyncTransport, SyncTransport, encode_path_segment, query_params
from ..models import Pack, PackPage, PackRevision


class PacksResource:
    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def list(
        self,
        *,
        zone: str | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> PackPage:
        data = self._transport.request(
            "GET", "/packs", params=query_params(zone=zone, limit=limit, page_token=page_token),
        )
        return PackPage.model_validate(data)

    def get(self, pack_id: str) -> Pack:
        data = self._transport.request("GET", f"/packs/{encode_path_segment(pack_id)}")
        return Pack.model_validate(data)

    def get_revision(self, pack_id: str, revision_id: str) -> PackRevision:
        data = self._transport.request(
            "GET", f"/packs/{encode_path_segment(pack_id)}/revisions/"
            f"{encode_path_segment(revision_id)}",
        )
        return PackRevision.model_validate(data)


class AsyncPacksResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def list(
        self,
        *,
        zone: str | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> PackPage:
        data = await self._transport.request(
            "GET", "/packs", params=query_params(zone=zone, limit=limit, page_token=page_token),
        )
        return PackPage.model_validate(data)

    async def get(self, pack_id: str) -> Pack:
        data = await self._transport.request("GET", f"/packs/{encode_path_segment(pack_id)}")
        return Pack.model_validate(data)

    async def get_revision(self, pack_id: str, revision_id: str) -> PackRevision:
        data = await self._transport.request(
            "GET", f"/packs/{encode_path_segment(pack_id)}/revisions/"
            f"{encode_path_segment(revision_id)}",
        )
        return PackRevision.model_validate(data)
