"""Tests for retry/backoff, Retry-After, Idempotency-Key reuse, and error hierarchy."""

from __future__ import annotations

import uuid
import unittest
from datetime import datetime, timedelta, timezone
from email.utils import format_datetime
from unittest.mock import patch

import httpx

from kibotos import (
    APIConnectionError,
    APIError,
    AuthenticationError,
    ConflictError,
    Kibotos,
    KibotosError,
    NotFoundError,
    PermissionDeniedError,
    RateLimitError,
    UnexpectedStatusError,
    WaitTimeoutError,
)
from kibotos.errors import parse_retry_after
from tests._fixtures import make_error, make_workspace


class TestTransportAndErrors(unittest.TestCase):
    def test_idempotency_key_auto_generated_and_reused_across_retries(self) -> None:
        seen_keys: list[str] = []
        attempts = 0

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            seen_keys.append(request.headers["Idempotency-Key"])
            if attempts < 3:
                return httpx.Response(
                    503,
                    headers={"Request-Id": f"req_{attempts}"},
                    json=make_error(
                        status=503,
                        code="capacity_unavailable",
                        message="try again",
                        retryable=True,
                        request_id=f"req_{attempts}",
                    ),
                )
            return httpx.Response(202, json=make_workspace(status="creating"))

        with patch("kibotos._transport.time.sleep") as mock_sleep:
            with Kibotos(
                "kb_test",
                max_retries=3,
                transport=httpx.MockTransport(handler),
            ) as client:
                wks = client.workspaces.create(template="python-3.12")

        self.assertEqual(wks.status, "creating")
        self.assertEqual(attempts, 3)
        self.assertEqual(len(seen_keys), 3)
        self.assertEqual(len(set(seen_keys)), 1)
        # Verify auto-generated key is a valid UUID v4.
        parsed_uuid = uuid.UUID(seen_keys[0])
        self.assertEqual(parsed_uuid.version, 4)
        self.assertEqual(mock_sleep.call_count, 2)

    def test_explicit_idempotency_key_is_preserved(self) -> None:
        seen_key: str | None = None

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal seen_key
            seen_key = request.headers.get("Idempotency-Key")
            return httpx.Response(202, json=make_workspace(status="creating"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            client.workspaces.create(template="python-3.12", idempotency_key="custom-idem-key-123")

        self.assertEqual(seen_key, "custom-idem-key-123")

    def test_rate_limit_honors_retry_after_header(self) -> None:
        attempts = 0

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            if attempts == 1:
                return httpx.Response(
                    429,
                    headers={"Retry-After": "1.25", "Request-Id": "req_rl"},
                    json=make_error(
                        status=429,
                        code="rate_limited",
                        message="Slow down",
                        retryable=True,
                        request_id="req_rl",
                    ),
                )
            return httpx.Response(200, json=make_workspace())

        with patch("kibotos._transport.time.sleep") as mock_sleep:
            with Kibotos(
                "kb_test",
                max_retries=2,
                transport=httpx.MockTransport(handler),
            ) as client:
                wks = client.workspaces.get("wks_01")

        self.assertEqual(wks.id, "wks_01")
        mock_sleep.assert_called_once_with(1.25)

    def test_parse_retry_after_http_date(self) -> None:
        future = datetime.now(timezone.utc) + timedelta(seconds=10)
        header = format_datetime(future, usegmt=True)
        delay = parse_retry_after(header)
        self.assertIsNotNone(delay)
        assert delay is not None
        self.assertGreaterEqual(delay, 5.0)
        self.assertLessEqual(delay, 12.0)

    def test_retryable_409_operation_in_progress_retries(self) -> None:
        attempts = 0

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            if attempts == 1:
                return httpx.Response(
                    409,
                    json=make_error(
                        status=409,
                        code="operation_in_progress",
                        message="Another operation is running",
                        retryable=True,
                    ),
                )
            return httpx.Response(202, json=make_workspace(status="stopping"))

        with patch("kibotos._transport.time.sleep"):
            with Kibotos("kb_test", max_retries=2, transport=httpx.MockTransport(handler)) as client:
                wks = client.workspaces.stop("wks_01")

        self.assertEqual(attempts, 2)
        self.assertEqual(wks.status, "stopping")

    def test_non_retryable_error_does_not_retry(self) -> None:
        attempts = 0

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            return httpx.Response(
                409,
                json=make_error(
                    status=409,
                    code="invalid_state",
                    message="Workspace is already stopped",
                    retryable=False,
                    request_id="req_conflict",
                    details={"current_status": "stopped"},
                ),
            )

        with Kibotos("kb_test", max_retries=3, transport=httpx.MockTransport(handler)) as client:
            with self.assertRaises(ConflictError) as ctx:
                client.workspaces.stop("wks_01")

        self.assertEqual(attempts, 1)
        err = ctx.exception
        self.assertEqual(err.status, 409)
        self.assertEqual(err.code, "invalid_state")
        self.assertEqual(err.request_id, "req_conflict")
        self.assertFalse(err.retryable)
        self.assertEqual(err.details, {"current_status": "stopped"})
        self.assertIn("req_conflict", str(err))

    def test_error_hierarchy_mapping(self) -> None:
        cases = [
            (401, "unauthenticated", False, AuthenticationError),
            (403, "quota_exceeded", False, PermissionDeniedError),
            (404, "workspace_not_found", False, NotFoundError),
            (408, "wait_timeout", True, WaitTimeoutError),
            (409, "invalid_state", False, ConflictError),
            (409, "unexpected_status", False, UnexpectedStatusError),
            (429, "rate_limited", True, RateLimitError),
            (500, "internal_error", True, APIError),
        ]
        for status, code, retryable, expected_cls in cases:
            with self.subTest(status=status, code=code):
                transport = httpx.MockTransport(
                    lambda req, s=status, c=code, r=retryable: httpx.Response(
                        s,
                        headers={"Retry-After": "3"} if s == 429 else {},
                        json=make_error(status=s, code=c, retryable=r, request_id="req_map"),
                    )
                )
                with Kibotos("kb_test", max_retries=0, transport=transport) as client:
                    with self.assertRaises(expected_cls) as ctx:
                        client.workspaces.get("wks_01")
                err = ctx.exception
                self.assertIsInstance(err, KibotosError)
                self.assertIsInstance(err, APIError)
                self.assertEqual(err.status, status)
                self.assertEqual(err.code, code)
                self.assertEqual(err.request_id, "req_map")
                if isinstance(err, RateLimitError):
                    self.assertEqual(err.retry_after, 3.0)

    def test_network_error_retries_then_raises_connection_error(self) -> None:
        attempts = 0

        def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            raise httpx.ConnectError("connection refused", request=request)

        with patch("kibotos._transport.time.sleep") as mock_sleep:
            with Kibotos("kb_test", max_retries=2, transport=httpx.MockTransport(handler)) as client:
                with self.assertRaises(APIConnectionError) as ctx:
                    client.zones.list()

        self.assertEqual(attempts, 3)
        self.assertEqual(mock_sleep.call_count, 2)
        self.assertIsInstance(ctx.exception.original_error, httpx.ConnectError)


if __name__ == "__main__":
    unittest.main()
