"""Templates resource for sync and async Kibotos clients."""

from __future__ import annotations

from collections.abc import AsyncIterator, Iterator

from .._transport import AsyncTransport, SyncTransport, encode_path_segment
from ..models import Template, TemplatePage, TemplateRevision, TemplateRevisionPage


def _list_params(
    *,
    owner: str | None,
    zone: str | None,
    limit: int | None,
    page_token: str | None,
) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if owner is not None:
        params.append(("owner", owner))
    if zone is not None:
        params.append(("zone", zone))
    if limit is not None:
        params.append(("limit", str(limit)))
    if page_token is not None:
        params.append(("page_token", page_token))
    return params


def _revision_list_params(
    *,
    limit: int | None,
    page_token: str | None,
) -> list[tuple[str, str]]:
    params: list[tuple[str, str]] = []
    if limit is not None:
        params.append(("limit", str(limit)))
    if page_token is not None:
        params.append(("page_token", page_token))
    return params


class TemplatesResource:
    """Synchronous interface for `/v1/templates`."""

    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def list(
        self,
        *,
        owner: str | None = None,
        zone: str | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> TemplatePage:
        """List templates across zones."""
        data = self._transport.request(
            "GET",
            "/templates",
            params=_list_params(owner=owner, zone=zone, limit=limit, page_token=page_token),
        )
        return TemplatePage.model_validate(data)

    def iterate(
        self,
        *,
        owner: str | None = None,
        zone: str | None = None,
        limit: int | None = None,
    ) -> Iterator[Template]:
        """Iterate across all pages of templates."""
        page_token: str | None = None
        while True:
            page = self.list(owner=owner, zone=zone, limit=limit, page_token=page_token)
            yield from page.data
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    def get(self, template_id: str) -> Template:
        """Fetch a single template by ID."""
        data = self._transport.request(
            "GET",
            f"/templates/{encode_path_segment(template_id)}",
        )
        return Template.model_validate(data)

    def list_revisions(
        self,
        template_id: str,
        *,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> TemplateRevisionPage:
        """List revisions for a template."""
        data = self._transport.request(
            "GET",
            f"/templates/{encode_path_segment(template_id)}/revisions",
            params=_revision_list_params(limit=limit, page_token=page_token),
        )
        return TemplateRevisionPage.model_validate(data)

    def get_revision(self, template_id: str, revision_id: str) -> TemplateRevision:
        """Fetch a specific revision of a template."""
        data = self._transport.request(
            "GET",
            (
                f"/templates/{encode_path_segment(template_id)}"
                f"/revisions/{encode_path_segment(revision_id)}"
            ),
        )
        return TemplateRevision.model_validate(data)


class AsyncTemplatesResource:
    """Asynchronous interface for `/v1/templates`."""

    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def list(
        self,
        *,
        owner: str | None = None,
        zone: str | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> TemplatePage:
        """List templates across zones."""
        data = await self._transport.request(
            "GET",
            "/templates",
            params=_list_params(owner=owner, zone=zone, limit=limit, page_token=page_token),
        )
        return TemplatePage.model_validate(data)

    async def iterate(
        self,
        *,
        owner: str | None = None,
        zone: str | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[Template]:
        """Iterate asynchronously across all pages of templates."""
        page_token: str | None = None
        while True:
            page = await self.list(owner=owner, zone=zone, limit=limit, page_token=page_token)
            for item in page.data:
                yield item
            if not page.next_page_token:
                break
            page_token = page.next_page_token

    async def get(self, template_id: str) -> Template:
        """Fetch a single template by ID."""
        data = await self._transport.request(
            "GET",
            f"/templates/{encode_path_segment(template_id)}",
        )
        return Template.model_validate(data)

    async def list_revisions(
        self,
        template_id: str,
        *,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> TemplateRevisionPage:
        """List revisions for a template."""
        data = await self._transport.request(
            "GET",
            f"/templates/{encode_path_segment(template_id)}/revisions",
            params=_revision_list_params(limit=limit, page_token=page_token),
        )
        return TemplateRevisionPage.model_validate(data)

    async def get_revision(self, template_id: str, revision_id: str) -> TemplateRevision:
        """Fetch a specific revision of a template."""
        data = await self._transport.request(
            "GET",
            (
                f"/templates/{encode_path_segment(template_id)}"
                f"/revisions/{encode_path_segment(revision_id)}"
            ),
        )
        return TemplateRevision.model_validate(data)
