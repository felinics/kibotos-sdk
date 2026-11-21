"""Tests for client initialization, env vars, base URL normalization, and forward compatibility."""

from __future__ import annotations

import os
import unittest
from unittest.mock import patch

import httpx

from kibotos import AsyncKibotos, Kibotos, Workspace, Zone, __version__
from kibotos._transport import normalize_base_url
from tests._fixtures import make_workspace, make_zone


class TestClientConfiguration(unittest.TestCase):
    def test_missing_api_key_raises(self) -> None:
        with patch.dict(os.environ, {}, clear=True):
            with self.assertRaises(ValueError):
                Kibotos()
            with self.assertRaises(ValueError):
                AsyncKibotos()

    def test_env_var_resolution(self) -> None:
        captured: list[httpx.Request] = []

        def handler(request: httpx.Request) -> httpx.Response:
            captured.append(request)
            return httpx.Response(200, json={"data": [make_zone()], "next_page_token": None})

        with patch.dict(
            os.environ,
            {
                "KIBOTOS_API_KEY": "kb_env_secret_key",
                "KIBOTOS_API_URL": "https://sandbox.kibo.to/",
            },
            clear=True,
        ):
            with Kibotos(transport=httpx.MockTransport(handler)) as client:
                self.assertEqual(client.base_url, "https://sandbox.kibo.to/v1")
                page = client.zones.list()
                self.assertEqual(len(page.data), 1)

        self.assertEqual(len(captured), 1)
        self.assertEqual(captured[0].headers["Authorization"], "Bearer kb_env_secret_key")
        self.assertEqual(captured[0].headers["User-Agent"], f"kibotos-python/{__version__}")
        self.assertEqual(str(captured[0].url), "https://sandbox.kibo.to/v1/zones")

    def test_base_url_normalization(self) -> None:
        self.assertEqual(normalize_base_url(None), "https://api.kibo.to/v1")
        self.assertEqual(normalize_base_url("https://api.kibo.to"), "https://api.kibo.to/v1")
        self.assertEqual(normalize_base_url("https://api.kibo.to/"), "https://api.kibo.to/v1")
        self.assertEqual(normalize_base_url("https://api.kibo.to/v1"), "https://api.kibo.to/v1")
        self.assertEqual(normalize_base_url("https://api.kibo.to/v1/"), "https://api.kibo.to/v1")
        self.assertEqual(normalize_base_url("http://localhost:8080"), "http://localhost:8080/v1")

    def test_custom_http_client_not_closed_by_context_manager(self) -> None:
        transport = httpx.MockTransport(
            lambda req: httpx.Response(200, json={"data": [], "next_page_token": None})
        )
        custom = httpx.Client(transport=transport)
        try:
            with Kibotos("kb_test", http_client=custom) as client:
                client.zones.list()
            self.assertFalse(custom.is_closed)
        finally:
            custom.close()

    def test_forward_compatible_unknown_fields_and_future_enum(self) -> None:
        raw_zone = make_zone(
            status="future_status",
            extra={"new_v1_field": {"nested": True}, "experimental_flag": 42},
        )
        zone = Zone.model_validate(raw_zone)
        self.assertEqual(zone.id, "global-1")
        self.assertEqual(zone.status, "future_status")
        self.assertFalse(hasattr(zone, "new_v1_field"))

        raw_wks = make_workspace(
            status="hibernating",
            extra={"gpu_spec": {"count": 1}, "billing_tier": "pro"},
        )
        wks = Workspace.model_validate(raw_wks)
        self.assertEqual(wks.id, "wks_01")
        self.assertEqual(wks.status, "hibernating")
        self.assertFalse(hasattr(wks, "gpu_spec"))


if __name__ == "__main__":
    unittest.main()
