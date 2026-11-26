"""Asynchronous tests for AsyncKibotos across all resources, retries, and wait()."""

from __future__ import annotations

import json
import unittest
from unittest.mock import AsyncMock, patch

import httpx

from kibotos import AsyncKibotos, RateLimitError, WaitTimeoutError
from tests._fixtures import (
    make_error,
    make_exposure,
    make_template,
    make_template_revision,
    make_volume,
    make_workspace,
    make_zone,
)


class TestAsyncClient(unittest.IsolatedAsyncioTestCase):
    async def test_async_pagination_and_resources(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            method, path = request.method, request.url.path
            if method == "GET" and path == "/v1/zones":
                token = request.url.params.get("page_token")
                if token is None:
                    return httpx.Response(
                        200,
                        json={"data": [make_zone("global-1")], "next_page_token": "z2"},
                    )
                return httpx.Response(
                    200,
                    json={"data": [make_zone("us-1", default=False)], "next_page_token": None},
                )
            if method == "GET" and path == "/v1/templates":
                return httpx.Response(
                    200,
                    json={
                        "data": [make_template("python-3.12")],
                        "unavailable_zones": [],
                        "next_page_token": None,
                    },
                )
            if method == "GET" and path == "/v1/templates/python-3.12/revisions/r42":
                return httpx.Response(200, json=make_template_revision("r42"))
            if method == "POST" and path == "/v1/workspaces":
                body = json.loads(request.content.decode())
                self.assertEqual(body["template"], {"id": "python-3.12"})
                return httpx.Response(202, json=make_workspace("wks_async", status="creating"))
            if method == "GET" and path == "/v1/workspaces":
                self.assertEqual(request.url.params.get("label.owner"), "bot")
                return httpx.Response(
                    200,
                    json={
                        "data": [make_workspace("wks_async")],
                        "unavailable_zones": [],
                        "next_page_token": None,
                    },
                )
            if method == "POST" and path == "/v1/volumes":
                return httpx.Response(202, json=make_volume("vol_async", status="creating"))
            if method == "POST" and path == "/v1/workspaces/wks_async/exposures":
                return httpx.Response(
                    201, json=make_exposure("exp_async", workspace_id="wks_async")
                )
            if method == "DELETE" and path == "/v1/workspaces/wks_async/exposures/exp_async":
                return httpx.Response(
                    200,
                    json=make_exposure(
                        "exp_async",
                        workspace_id="wks_async",
                        status="revoked",
                        revoked_at="2026-11-21T12:15:00Z",
                    ),
                )
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        async with AsyncKibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            zones = [z async for z in client.zones.iterate()]
            self.assertEqual([z.id for z in zones], ["global-1", "us-1"])

            templates = [t async for t in client.templates.iterate()]
            self.assertEqual([t.id for t in templates], ["python-3.12"])

            rev = await client.templates.get_revision("python-3.12", "r42")
            self.assertEqual(rev.id, "r42")

            wks = await client.workspaces.create(template="python-3.12")
            self.assertEqual(wks.id, "wks_async")

            found_wks = [w async for w in client.workspaces.iterate(labels={"owner": "bot"})]
            self.assertEqual([w.id for w in found_wks], ["wks_async"])

            vol = await client.volumes.create(size_gib=20)
            self.assertEqual(vol.id, "vol_async")

            exp = await client.exposures.create("wks_async", port=8080)
            self.assertEqual(exp.id, "exp_async")

            revoked = await client.exposures.revoke("wks_async", "exp_async")
            self.assertEqual(revoked.status, "revoked")

    async def test_async_retry_with_retry_after_and_idempotency_key_reuse(self) -> None:
        seen_keys: list[str] = []
        attempts = 0

        async def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            seen_keys.append(request.headers["Idempotency-Key"])
            if attempts == 1:
                return httpx.Response(
                    429,
                    headers={"Retry-After": "0.5"},
                    json=make_error(status=429, code="rate_limited", retryable=True),
                )
            return httpx.Response(202, json=make_volume("vol_01", status="creating"))

        with patch("kibotos._transport.asyncio.sleep", new_callable=AsyncMock) as mock_sleep:
            async with AsyncKibotos(
                "kb_test",
                max_retries=2,
                transport=httpx.MockTransport(handler),
            ) as client:
                vol = await client.volumes.create(size_gib=10)

        self.assertEqual(vol.id, "vol_01")
        self.assertEqual(attempts, 2)
        self.assertEqual(seen_keys[0], seen_keys[1])
        mock_sleep.assert_awaited_once_with(0.5)

    async def test_async_wait_chunks_over_60s(self) -> None:
        chunks: list[str] = []

        async def handler(request: httpx.Request) -> httpx.Response:
            chunks.append(request.url.params["timeout"])
            if len(chunks) < 3:
                return httpx.Response(
                    408,
                    json=make_error(status=408, code="wait_timeout", retryable=True),
                )
            return httpx.Response(200, json=make_workspace("wks_01", status="available"))

        async with AsyncKibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            wks = await client.workspaces.wait("wks_01", status=["available", "failed"], timeout=130)

        self.assertEqual(wks.status, "available")
        self.assertEqual(chunks, ["60", "60", "10"])

    async def test_async_wait_timeout_and_rate_limit_errors(self) -> None:
        async def timeout_handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                408,
                json=make_error(status=408, code="wait_timeout", retryable=True),
            )

        async with AsyncKibotos("kb_test", transport=httpx.MockTransport(timeout_handler)) as client:
            with self.assertRaises(WaitTimeoutError):
                await client.volumes.wait("vol_01", status="available", timeout=65)

        async def rl_handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(
                429,
                headers={"Retry-After": "4"},
                json=make_error(status=429, code="rate_limited", retryable=True),
            )

        async with AsyncKibotos(
            "kb_test", max_retries=0, transport=httpx.MockTransport(rl_handler)
        ) as client:
            with self.assertRaises(RateLimitError) as ctx:
                await client.zones.list()
        self.assertEqual(ctx.exception.retry_after, 4.0)

    async def test_async_all_mutation_and_detail_methods(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            method, path = request.method, request.url.path
            if method == "GET" and path == "/v1/templates/python-3.12":
                return httpx.Response(200, json=make_template("python-3.12"))
            if method == "GET" and path == "/v1/templates/python-3.12/revisions":
                return httpx.Response(
                    200,
                    json={"data": [make_template_revision("r42")], "next_page_token": None},
                )
            if method == "GET" and path == "/v1/workspaces/wks_01":
                return httpx.Response(200, json=make_workspace("wks_01"))
            if method == "PATCH" and path == "/v1/workspaces/wks_01":
                return httpx.Response(200, json=make_workspace("wks_01", name="renamed"))
            if method == "POST" and path == "/v1/workspaces/wks_01/stop":
                return httpx.Response(202, json=make_workspace("wks_01", status="stopping"))
            if method == "POST" and path == "/v1/workspaces/wks_01/start":
                return httpx.Response(202, json=make_workspace("wks_01", status="starting"))
            if method == "POST" and path == "/v1/workspaces/wks_01/timeout":
                return httpx.Response(200, json=make_workspace("wks_01", status="available"))
            if method == "PUT" and path == "/v1/workspaces/wks_01/network":
                return httpx.Response(202, json=make_workspace("wks_01", network_mode="deny_all"))
            if method == "PUT" and path == "/v1/workspaces/wks_01/volume":
                return httpx.Response(
                    200,
                    json=make_workspace(
                        "wks_01",
                        volume={
                            "id": "vol_01",
                            "mount_path": "/data",
                            "delete_with_workspace": True,
                        },
                    ),
                )
            if method == "DELETE" and path == "/v1/workspaces/wks_01/volume":
                return httpx.Response(200, json=make_workspace("wks_01", volume=None))
            if method == "DELETE" and path == "/v1/workspaces/wks_01":
                return httpx.Response(202, json=make_workspace("wks_01", status="deleting"))
            if method == "GET" and path == "/v1/volumes":
                return httpx.Response(
                    200,
                    json={
                        "data": [make_volume("vol_01")],
                        "unavailable_zones": [],
                        "next_page_token": None,
                    },
                )
            if method == "GET" and path == "/v1/volumes/vol_01":
                return httpx.Response(200, json=make_volume("vol_01"))
            if method == "PATCH" and path == "/v1/volumes/vol_01":
                return httpx.Response(200, json=make_volume("vol_01", name="vol-new"))
            if method == "POST" and path == "/v1/volumes/vol_01/resize":
                return httpx.Response(
                    202, json=make_volume("vol_01", size_gib=80, status="resizing")
                )
            if method == "DELETE" and path == "/v1/volumes/vol_01":
                return httpx.Response(202, json=make_volume("vol_01", status="deleting"))
            if method == "GET" and path == "/v1/workspaces/wks_01/exposures":
                return httpx.Response(
                    200,
                    json={"data": [make_exposure("exp_01")], "next_page_token": None},
                )
            if method == "GET" and path == "/v1/workspaces/wks_01/exposures/exp_01":
                return httpx.Response(200, json=make_exposure("exp_01"))
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        async with AsyncKibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            tpl = await client.templates.get("python-3.12")
            self.assertEqual(tpl.id, "python-3.12")
            revs = await client.templates.list_revisions("python-3.12")
            self.assertEqual(len(revs.data), 1)

            wks = await client.workspaces.get("wks_01")
            self.assertEqual(wks.id, "wks_01")
            wks = await client.workspaces.update("wks_01", name="renamed")
            self.assertEqual(wks.name, "renamed")
            self.assertEqual((await client.workspaces.stop("wks_01", force=False)).status, "stopping")
            self.assertEqual((await client.workspaces.start("wks_01")).status, "starting")
            self.assertEqual(
                (await client.workspaces.set_timeout("wks_01", seconds=600, action="stop")).status,
                "available",
            )
            self.assertEqual(
                (await client.workspaces.set_network("wks_01", mode="deny_all")).network.mode,
                "deny_all",
            )
            attached = await client.workspaces.attach_volume(
                "wks_01", volume_id="vol_01", mount_path="/data", delete_with_workspace=True
            )
            assert attached.volume is not None
            self.assertTrue(attached.volume.delete_with_workspace)
            self.assertIsNone((await client.workspaces.detach_volume("wks_01")).volume)
            self.assertEqual((await client.workspaces.delete("wks_01")).status, "deleting")

            vols = [v async for v in client.volumes.iterate(attached=False)]
            self.assertEqual([v.id for v in vols], ["vol_01"])
            self.assertEqual((await client.volumes.get("vol_01")).size_gib, 50)
            self.assertEqual((await client.volumes.update("vol_01", name="vol-new")).name, "vol-new")
            self.assertEqual((await client.volumes.resize("vol_01", size_gib=80)).size_gib, 80)
            self.assertEqual((await client.volumes.delete("vol_01")).status, "deleting")

            exps = [e async for e in client.exposures.iterate("wks_01")]
            self.assertEqual([e.id for e in exps], ["exp_01"])
            self.assertEqual((await client.exposures.get("wks_01", "exp_01")).port, 8080)


if __name__ == "__main__":
    unittest.main()
