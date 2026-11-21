"""Error hierarchy for the Kibotos Python SDK."""

from __future__ import annotations

from datetime import datetime, timezone
from email.utils import parsedate_to_datetime
from typing import Any

import httpx

RETRYABLE_STATUS_CODES = frozenset({408, 429, 500, 502, 503, 504})


class KibotosError(Exception):
    """Base exception for all Kibotos SDK errors."""


class APIConnectionError(KibotosError):
    """Raised when a network or transport error prevents receiving a response."""

    def __init__(self, message: str, *, original_error: Exception | None = None) -> None:
        super().__init__(message)
        self.original_error = original_error


class APIError(KibotosError):
    """Error returned by the Kibotos API."""

    def __init__(
        self,
        message: str,
        *,
        status: int,
        code: str,
        request_id: str | None = None,
        retryable: bool = False,
        details: dict[str, Any] | None = None,
    ) -> None:
        super().__init__(message)
        self.message = message
        self.status = status
        self.code = code
        self.request_id = request_id
        self.retryable = retryable
        self.details: dict[str, Any] = details if details is not None else {}

    def __str__(self) -> str:
        base = f"[{self.status} {self.code}] {self.message}"
        if self.request_id:
            return f"{base} (request_id={self.request_id})"
        return base


class AuthenticationError(APIError):
    """Raised on HTTP 401 authentication failures."""


class PermissionDeniedError(APIError):
    """Raised on HTTP 403 permission, zone access, suspension, or quota errors."""


class NotFoundError(APIError):
    """Raised on HTTP 404 when a requested resource does not exist."""


class ConflictError(APIError):
    """Raised on HTTP 409 state conflicts, attached volumes, or reused idempotency keys."""


class UnexpectedStatusError(ConflictError):
    """Raised when a wait call observes a terminal or unexpected status."""


class RateLimitError(APIError):
    """Raised on HTTP 429 when rate limits are exceeded."""

    def __init__(
        self,
        message: str,
        *,
        status: int = 429,
        code: str = "rate_limited",
        request_id: str | None = None,
        retryable: bool = True,
        details: dict[str, Any] | None = None,
        retry_after: float | None = None,
    ) -> None:
        super().__init__(
            message,
            status=status,
            code=code,
            request_id=request_id,
            retryable=retryable,
            details=details,
        )
        self.retry_after = retry_after


class WaitTimeoutError(APIError):
    """Raised on HTTP 408 or client-side timeout when waiting for a resource state."""


def parse_retry_after(value: str | None) -> float | None:
    """Parse an HTTP Retry-After header (seconds or HTTP-date) into delay seconds."""
    if not value:
        return None
    raw = value.strip()
    if not raw:
        return None
    try:
        seconds = float(raw)
        return max(0.0, seconds)
    except ValueError:
        pass
    try:
        dt = parsedate_to_datetime(raw)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        delta = (dt - datetime.now(timezone.utc)).total_seconds()
        return max(0.0, delta)
    except (TypeError, ValueError, OverflowError):
        return None


def error_from_response(response: httpx.Response) -> APIError:
    """Build the most specific APIError subclass from an HTTP error response."""
    status = response.status_code
    request_id = response.headers.get("Request-Id") or response.headers.get("request-id")
    code = "api_error"
    message = response.reason_phrase or f"HTTP {status}"
    retryable = status in RETRYABLE_STATUS_CODES
    details: dict[str, Any] = {}

    try:
        payload = response.json()
    except ValueError:
        payload = None

    if isinstance(payload, dict):
        err_obj = payload.get("error")
        if isinstance(err_obj, dict):
            if isinstance(err_obj.get("code"), str) and err_obj["code"]:
                code = err_obj["code"]
            if isinstance(err_obj.get("message"), str) and err_obj["message"]:
                message = err_obj["message"]
            if isinstance(err_obj.get("status"), int):
                status = err_obj["status"]
            if isinstance(err_obj.get("request_id"), str) and err_obj["request_id"]:
                request_id = err_obj["request_id"]
            if isinstance(err_obj.get("retryable"), bool):
                retryable = err_obj["retryable"]
            if isinstance(err_obj.get("details"), dict):
                details = err_obj["details"]

    kwargs: dict[str, Any] = {
        "status": status,
        "code": code,
        "request_id": request_id,
        "retryable": retryable,
        "details": details,
    }

    if code == "wait_timeout" or status == 408:
        return WaitTimeoutError(message, **kwargs)
    if code == "unexpected_status":
        return UnexpectedStatusError(message, **kwargs)
    if status == 401:
        return AuthenticationError(message, **kwargs)
    if status == 403:
        return PermissionDeniedError(message, **kwargs)
    if status == 404:
        return NotFoundError(message, **kwargs)
    if status == 409:
        return ConflictError(message, **kwargs)
    if status == 429:
        retry_after = parse_retry_after(response.headers.get("Retry-After"))
        return RateLimitError(message, retry_after=retry_after, **kwargs)
    return APIError(message, **kwargs)
