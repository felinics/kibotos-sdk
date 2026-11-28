"""Internal HTTP transport, retry/backoff, and serialization helpers."""

from __future__ import annotations

import asyncio
import random
import time
import urllib.parse
import uuid
from collections.abc import AsyncIterable, AsyncIterator, Iterable, Iterator, Mapping
from contextlib import asynccontextmanager, contextmanager
from typing import Any, Final

import httpx
from pydantic import BaseModel

from .errors import (
    APIConnectionError,
    APIError,
    RateLimitError,
    error_from_response,
    parse_retry_after,
)

SDK_VERSION: Final[str] = "0.1.0"
DEFAULT_BASE_URL: Final[str] = "https://api.kibo.to/v1"
DEFAULT_TIMEOUT: Final[float] = 30.0
DEFAULT_MAX_RETRIES: Final[int] = 3
DEFAULT_INITIAL_BACKOFF: Final[float] = 0.5
DEFAULT_MAX_BACKOFF: Final[float] = 8.0
AUTO_RETRY_STATUS_CODES: Final[frozenset[int]] = frozenset({429, 500, 502, 503, 504})


class _UnsetType:
    """Sentinel type for distinguishing omitted optional arguments from explicit None."""

    def __repr__(self) -> str:
        return "UNSET"


UNSET: Final[_UnsetType] = _UnsetType()


def normalize_base_url(base_url: str | None) -> str:
    """Normalize base URL so it always ends with `/v1` and has no trailing slash."""
    if not base_url or not base_url.strip():
        return DEFAULT_BASE_URL
    cleaned = base_url.strip().rstrip("/")
    if cleaned.endswith("/v1"):
        return cleaned
    return f"{cleaned}/v1"


def encode_path_segment(value: str) -> str:
    """URL-encode a single path segment."""
    return urllib.parse.quote(value, safe="")


def serialize_labels(labels: Mapping[str, str] | None) -> list[tuple[str, str]]:
    """Serialize a label filter dict into `label.<k>=<v>` query tuples."""
    if not labels:
        return []
    return [(f"label.{k}", v) for k, v in labels.items()]


def query_params(**values: str | int | bool | None) -> list[tuple[str, str]]:
    return [
        (key, str(value).lower() if isinstance(value, bool) else str(value))
        for key, value in values.items()
        if value is not None
    ]


def to_payload(value: BaseModel | Mapping[str, Any]) -> dict[str, Any]:
    """Convert a Pydantic model or mapping to a JSON-serializable dict, dropping None fields."""
    if isinstance(value, BaseModel):
        return value.model_dump(mode="json", exclude_none=True)
    return {k: v for k, v in value.items() if v is not None}


def _should_retry_error(error: APIError) -> bool:
    if error.code == "wait_timeout":
        return False
    return error.retryable or error.status in AUTO_RETRY_STATUS_CODES


def _compute_backoff(
    attempt: int,
    *,
    retry_after: float | None = None,
    initial_backoff: float = DEFAULT_INITIAL_BACKOFF,
    max_backoff: float = DEFAULT_MAX_BACKOFF,
) -> float:
    if retry_after is not None:
        return max(0.0, retry_after)
    exp = min(max_backoff, initial_backoff * (2**attempt))
    return exp * random.uniform(0.5, 1.0)


def _build_headers(
    api_key: str,
    *,
    method: str,
    has_body: bool,
    idempotency_key: str | None = None,
    use_idempotency: bool = True,
) -> dict[str, str]:
    headers: dict[str, str] = {
        "Authorization": f"Bearer {api_key}",
        "Accept": "application/json",
        "User-Agent": f"kibotos-python/{SDK_VERSION}",
    }
    if has_body:
        headers["Content-Type"] = "application/json"
    if use_idempotency and method.upper() == "POST":
        headers["Idempotency-Key"] = idempotency_key or str(uuid.uuid4())
    elif use_idempotency and idempotency_key:
        headers["Idempotency-Key"] = idempotency_key
    return headers


def _parse_json_response(response: httpx.Response) -> Any:
    if response.status_code == 204 or not response.content:
        return None
    try:
        return response.json()
    except ValueError as exc:
        raise APIError(
            f"Invalid JSON response from Kibotos API (HTTP {response.status_code})",
            status=response.status_code,
            code="invalid_response",
            request_id=response.headers.get("Request-Id"),
            retryable=False,
        ) from exc


class SyncTransport:
    """Synchronous HTTP transport with retries, idempotency, and error mapping."""

    def __init__(
        self,
        *,
        api_key: str,
        base_url: str,
        timeout: float | httpx.Timeout = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        http_client: httpx.Client | None = None,
        transport: httpx.BaseTransport | None = None,
        initial_backoff: float = DEFAULT_INITIAL_BACKOFF,
        max_backoff: float = DEFAULT_MAX_BACKOFF,
    ) -> None:
        self.api_key = api_key
        self.base_url = normalize_base_url(base_url)
        self.max_retries = max(0, max_retries)
        self._initial_backoff = initial_backoff
        self._max_backoff = max_backoff
        self._owns_client = http_client is None
        self._client = http_client or httpx.Client(
            timeout=timeout,
            transport=transport,
        )

    def close(self) -> None:
        if self._owns_client:
            self._client.close()

    def request(
        self,
        method: str,
        path: str,
        *,
        params: list[tuple[str, str]] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | Iterable[bytes] | None = None,
        idempotency_key: str | None = None,
        timeout: float | httpx.Timeout | None = None,
        replayable: bool = True,
        use_idempotency: bool = True,
    ) -> Any:
        url = f"{self.base_url}{path}"
        headers = _build_headers(
            self.api_key,
            method=method,
            has_body=json_body is not None,
            idempotency_key=idempotency_key,
            use_idempotency=use_idempotency,
        )
        if content is not None:
            headers["Content-Type"] = "application/octet-stream"
        max_retries = self.max_retries if replayable else 0

        for attempt in range(max_retries + 1):
            try:
                kwargs: dict[str, Any] = {
                    "headers": headers,
                    "params": params,
                    "json": json_body,
                }
                if timeout is not None:
                    kwargs["timeout"] = timeout
                if content is not None:
                    kwargs["content"] = content
                response = self._client.request(method, url, **kwargs)
            except httpx.TransportError as exc:
                if attempt >= max_retries:
                    raise APIConnectionError(
                        f"Request to {method} {url} failed: {exc}",
                        original_error=exc,
                    ) from exc
                delay = _compute_backoff(
                    attempt,
                    initial_backoff=self._initial_backoff,
                    max_backoff=self._max_backoff,
                )
                time.sleep(delay)
                continue

            if 200 <= response.status_code < 300:
                return _parse_json_response(response)

            error = error_from_response(response)
            if attempt < max_retries and _should_retry_error(error):
                retry_after = (
                    error.retry_after
                    if isinstance(error, RateLimitError)
                    else parse_retry_after(response.headers.get("Retry-After"))
                )
                delay = _compute_backoff(
                    attempt,
                    retry_after=retry_after,
                    initial_backoff=self._initial_backoff,
                    max_backoff=self._max_backoff,
                )
                time.sleep(delay)
                continue
            raise error

        raise AssertionError("unreachable")

    @contextmanager
    def stream(
        self,
        method: str,
        path: str,
        *,
        params: list[tuple[str, str]] | None = None,
        json_body: dict[str, Any] | None = None,
        headers: Mapping[str, str],
        timeout: float | httpx.Timeout | None = None,
        success_statuses: tuple[int, ...] = (),
    ) -> Iterator[httpx.Response]:
        """Open a single streaming request and close it on context exit."""
        url = f"{self.base_url}{path}"
        request_headers = _build_headers(
            self.api_key, method=method, has_body=json_body is not None, use_idempotency=False,
        )
        request_headers.update(headers)
        try:
            with self._client.stream(
                method, url, params=params, json=json_body, headers=request_headers,
                timeout=httpx.USE_CLIENT_DEFAULT if timeout is None else timeout,
            ) as response:
                if not response.is_success and response.status_code not in success_statuses:
                    response.read()
                    raise error_from_response(response)
                yield response
        except httpx.TransportError as exc:
            raise APIConnectionError(
                f"Request to {method} {url} failed: {exc}", original_error=exc,
            ) from exc


class AsyncTransport:
    """Asynchronous HTTP transport with retries, idempotency, and error mapping."""

    def __init__(
        self,
        *,
        api_key: str,
        base_url: str,
        timeout: float | httpx.Timeout = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        http_client: httpx.AsyncClient | None = None,
        transport: httpx.AsyncBaseTransport | None = None,
        initial_backoff: float = DEFAULT_INITIAL_BACKOFF,
        max_backoff: float = DEFAULT_MAX_BACKOFF,
    ) -> None:
        self.api_key = api_key
        self.base_url = normalize_base_url(base_url)
        self.max_retries = max(0, max_retries)
        self._initial_backoff = initial_backoff
        self._max_backoff = max_backoff
        self._owns_client = http_client is None
        self._client = http_client or httpx.AsyncClient(
            timeout=timeout,
            transport=transport,
        )

    async def aclose(self) -> None:
        if self._owns_client:
            await self._client.aclose()

    async def request(
        self,
        method: str,
        path: str,
        *,
        params: list[tuple[str, str]] | None = None,
        json_body: dict[str, Any] | None = None,
        content: bytes | AsyncIterable[bytes] | None = None,
        idempotency_key: str | None = None,
        timeout: float | httpx.Timeout | None = None,
        replayable: bool = True,
        use_idempotency: bool = True,
    ) -> Any:
        url = f"{self.base_url}{path}"
        headers = _build_headers(
            self.api_key,
            method=method,
            has_body=json_body is not None,
            idempotency_key=idempotency_key,
            use_idempotency=use_idempotency,
        )
        if content is not None:
            headers["Content-Type"] = "application/octet-stream"
        max_retries = self.max_retries if replayable else 0

        for attempt in range(max_retries + 1):
            try:
                kwargs: dict[str, Any] = {
                    "headers": headers,
                    "params": params,
                    "json": json_body,
                }
                if timeout is not None:
                    kwargs["timeout"] = timeout
                if content is not None:
                    kwargs["content"] = content
                response = await self._client.request(method, url, **kwargs)
            except httpx.TransportError as exc:
                if attempt >= max_retries:
                    raise APIConnectionError(
                        f"Request to {method} {url} failed: {exc}",
                        original_error=exc,
                    ) from exc
                delay = _compute_backoff(
                    attempt,
                    initial_backoff=self._initial_backoff,
                    max_backoff=self._max_backoff,
                )
                await asyncio.sleep(delay)
                continue

            if 200 <= response.status_code < 300:
                return _parse_json_response(response)

            error = error_from_response(response)
            if attempt < max_retries and _should_retry_error(error):
                retry_after = (
                    error.retry_after
                    if isinstance(error, RateLimitError)
                    else parse_retry_after(response.headers.get("Retry-After"))
                )
                delay = _compute_backoff(
                    attempt,
                    retry_after=retry_after,
                    initial_backoff=self._initial_backoff,
                    max_backoff=self._max_backoff,
                )
                await asyncio.sleep(delay)
                continue
            raise error

        raise AssertionError("unreachable")

    @asynccontextmanager
    async def stream(
        self,
        method: str,
        path: str,
        *,
        params: list[tuple[str, str]] | None = None,
        json_body: dict[str, Any] | None = None,
        headers: Mapping[str, str],
        timeout: float | httpx.Timeout | None = None,
        success_statuses: tuple[int, ...] = (),
    ) -> AsyncIterator[httpx.Response]:
        """Open a single streaming request and close it on context exit."""
        url = f"{self.base_url}{path}"
        request_headers = _build_headers(
            self.api_key, method=method, has_body=json_body is not None, use_idempotency=False,
        )
        request_headers.update(headers)
        try:
            async with self._client.stream(
                method, url, params=params, json=json_body, headers=request_headers,
                timeout=httpx.USE_CLIENT_DEFAULT if timeout is None else timeout,
            ) as response:
                if not response.is_success and response.status_code not in success_statuses:
                    await response.aread()
                    raise error_from_response(response)
                yield response
        except httpx.TransportError as exc:
            raise APIConnectionError(
                f"Request to {method} {url} failed: {exc}", original_error=exc,
            ) from exc
