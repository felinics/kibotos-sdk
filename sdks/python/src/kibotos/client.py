"""Synchronous and asynchronous clients for the Kibotos v1 API."""

from __future__ import annotations

import os
from types import TracebackType

import httpx

from ._transport import (
    DEFAULT_BASE_URL,
    DEFAULT_INITIAL_BACKOFF,
    DEFAULT_MAX_BACKOFF,
    DEFAULT_MAX_RETRIES,
    DEFAULT_TIMEOUT,
    AsyncTransport,
    SyncTransport,
)
from .resources import (
    AsyncEgressesResource,
    AsyncPacksResource,
    AsyncResolversResource,
    AsyncExposuresResource,
    AsyncTemplatesResource,
    AsyncVolumesResource,
    AsyncWorkspacesResource,
    AsyncZonesResource,
    EgressesResource,
    PacksResource,
    ResolversResource,
    ExposuresResource,
    TemplatesResource,
    VolumesResource,
    WorkspacesResource,
    ZonesResource,
)


def _resolve_api_key(api_key: str | None) -> str:
    resolved = api_key if api_key is not None else os.environ.get("KIBOTOS_API_KEY")
    if not resolved or not resolved.strip():
        raise ValueError(
            "Kibotos API key is required. Pass api_key=... or set the KIBOTOS_API_KEY environment variable."
        )
    return resolved.strip()


def _resolve_base_url(base_url: str | None) -> str:
    if base_url is not None and base_url.strip():
        return base_url
    env_url = os.environ.get("KIBOTOS_API_URL")
    if env_url and env_url.strip():
        return env_url
    return DEFAULT_BASE_URL


class Kibotos:
    """Synchronous client for the Kibotos v1 API.

    Can be used as a context manager (`with Kibotos() as client:`) to ensure the
    underlying HTTP connection pool is closed cleanly.
    """

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str | None = None,
        timeout: float | httpx.Timeout = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        http_client: httpx.Client | None = None,
        transport: httpx.BaseTransport | None = None,
        initial_backoff: float = DEFAULT_INITIAL_BACKOFF,
        max_backoff: float = DEFAULT_MAX_BACKOFF,
    ) -> None:
        resolved_key = _resolve_api_key(api_key)
        resolved_url = _resolve_base_url(base_url)

        self._transport = SyncTransport(
            api_key=resolved_key,
            base_url=resolved_url,
            timeout=timeout,
            max_retries=max_retries,
            http_client=http_client,
            transport=transport,
            initial_backoff=initial_backoff,
            max_backoff=max_backoff,
        )
        self.zones = ZonesResource(self._transport)
        self.templates = TemplatesResource(self._transport)
        self.workspaces = WorkspacesResource(self._transport)
        self.volumes = VolumesResource(self._transport)
        self.egresses = EgressesResource(self._transport)
        self.packs = PacksResource(self._transport)
        self.resolvers = ResolversResource(self._transport)
        self.exposures = ExposuresResource(self._transport)

    @property
    def base_url(self) -> str:
        """Normalized v1 API base URL."""
        return self._transport.base_url

    def close(self) -> None:
        """Close the underlying HTTP client if owned by this client."""
        self._transport.close()

    def __enter__(self) -> Kibotos:
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        self.close()


class AsyncKibotos:
    """Asynchronous client for the Kibotos v1 API.

    Can be used as an async context manager (`async with AsyncKibotos() as client:`).
    """

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str | None = None,
        timeout: float | httpx.Timeout = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        http_client: httpx.AsyncClient | None = None,
        transport: httpx.AsyncBaseTransport | None = None,
        initial_backoff: float = DEFAULT_INITIAL_BACKOFF,
        max_backoff: float = DEFAULT_MAX_BACKOFF,
    ) -> None:
        resolved_key = _resolve_api_key(api_key)
        resolved_url = _resolve_base_url(base_url)

        self._transport = AsyncTransport(
            api_key=resolved_key,
            base_url=resolved_url,
            timeout=timeout,
            max_retries=max_retries,
            http_client=http_client,
            transport=transport,
            initial_backoff=initial_backoff,
            max_backoff=max_backoff,
        )
        self.zones = AsyncZonesResource(self._transport)
        self.templates = AsyncTemplatesResource(self._transport)
        self.workspaces = AsyncWorkspacesResource(self._transport)
        self.volumes = AsyncVolumesResource(self._transport)
        self.egresses = AsyncEgressesResource(self._transport)
        self.packs = AsyncPacksResource(self._transport)
        self.resolvers = AsyncResolversResource(self._transport)
        self.exposures = AsyncExposuresResource(self._transport)

    @property
    def base_url(self) -> str:
        """Normalized v1 API base URL."""
        return self._transport.base_url

    async def aclose(self) -> None:
        """Close the underlying async HTTP client if owned by this client."""
        await self._transport.aclose()

    async def __aenter__(self) -> AsyncKibotos:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        await self.aclose()
