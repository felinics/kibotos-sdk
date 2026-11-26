"""Comprehensive synchronous tests for all Kibotos v1 resources."""

from __future__ import annotations

import json
import unittest

import httpx

from kibotos import (
    Kibotos,
    UnexpectedStatusError,
    WaitTimeoutError,
    WorkspaceNetwork,
    WorkspaceResources,
    WorkspaceTemplateRef,
    WorkspaceTimeoutSpec,
    WorkspaceVolume,
)
from tests._fixtures import (
    make_error,
    make_exposure,
    make_template,
    make_template_revision,
    make_volume,
    make_workspace,
    make_zone,
)


class TestSyncResources(unittest.TestCase):
    def test_zones_list_and_iterate(self) -> None:
        calls: list[httpx.Request] = []

        def handler(request: httpx.Request) -> httpx.Response:
            calls.append(request)
            token = request.url.params.get("page_token")
            if token is None:
                return httpx.Response(
                    200,
                    json={"data": [make_zone("global-1")], "next_page_token": "tok_2"},
                )
            return httpx.Response(
                200,
                json={"data": [make_zone("eu-1", country=None, default=False)], "next_page_token": None},
            )

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            zones = list(client.zones.iterate(limit=1))

        self.assertEqual([z.id for z in zones], ["global-1", "eu-1"])
        self.assertEqual([z.country for z in zones], ["JP", None])
        self.assertEqual(calls[0].url.params["limit"], "1")
        self.assertEqual(calls[1].url.params["page_token"], "tok_2")

    def test_templates_endpoints(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            path = request.url.path
            if path == "/v1/templates":
                self.assertEqual(request.url.params.get("owner"), "platform")
                self.assertEqual(request.url.params.get("zone"), "global-1")
                token = request.url.params.get("page_token")
                if token is None:
                    return httpx.Response(
                        200,
                        json={
                            "data": [make_template("python-3.12")],
                            "unavailable_zones": ["ap-1"],
                            "next_page_token": "p2",
                        },
                    )
                return httpx.Response(
                    200,
                    json={
                        "data": [make_template("node-22")],
                        "unavailable_zones": [],
                        "next_page_token": None,
                    },
                )
            if path == "/v1/templates/python-3.12":
                return httpx.Response(200, json=make_template("python-3.12"))
            if path == "/v1/templates/python-3.12/revisions":
                return httpx.Response(
                    200,
                    json={"data": [make_template_revision("r42")], "next_page_token": None},
                )
            if path == "/v1/templates/python-3.12/revisions/r42":
                return httpx.Response(200, json=make_template_revision("r42"))
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            page = client.templates.list(owner="platform", zone="global-1")
            self.assertEqual(page.unavailable_zones, ["ap-1"])
            self.assertEqual(page.data[0].id, "python-3.12")

            all_templates = list(client.templates.iterate(owner="platform", zone="global-1"))
            self.assertEqual([t.id for t in all_templates], ["python-3.12", "node-22"])

            tpl = client.templates.get("python-3.12")
            self.assertEqual(tpl.default_revision, "r42")
            assert tpl.latest_revision is not None
            self.assertEqual(tpl.latest_revision.id, "r43")

            revs = client.templates.list_revisions("python-3.12")
            self.assertEqual(len(revs.data), 1)
            rev = client.templates.get_revision("python-3.12", "r42")
            self.assertEqual(rev.size_mib, 1024)

    def test_workspaces_lifecycle_and_filters(self) -> None:
        recorded: list[tuple[str, str, dict[str, object] | None, list[tuple[str, str]]]] = []

        def handler(request: httpx.Request) -> httpx.Response:
            body = json.loads(request.content.decode()) if request.content else None
            recorded.append(
                (request.method, request.url.path, body, list(request.url.params.multi_items()))
            )
            method, path = request.method, request.url.path
            if method == "POST" and path == "/v1/workspaces":
                return httpx.Response(202, json=make_workspace(status="creating"))
            if method == "GET" and path == "/v1/workspaces":
                token = request.url.params.get("page_token")
                if token is None:
                    return httpx.Response(
                        200,
                        json={
                            "data": [make_workspace("wks_01")],
                            "unavailable_zones": [],
                            "next_page_token": "next_wks",
                        },
                    )
                return httpx.Response(
                    200,
                    json={
                        "data": [make_workspace("wks_02")],
                        "unavailable_zones": [],
                        "next_page_token": None,
                    },
                )
            if method == "GET" and path == "/v1/workspaces/wks_01":
                return httpx.Response(200, json=make_workspace("wks_01"))
            if method == "PATCH" and path == "/v1/workspaces/wks_01":
                return httpx.Response(200, json=make_workspace("wks_01", name=None, labels={"k": "v"}))
            if method == "POST" and path == "/v1/workspaces/wks_01/stop":
                return httpx.Response(202, json=make_workspace("wks_01", status="stopping"))
            if method == "POST" and path == "/v1/workspaces/wks_01/start":
                return httpx.Response(202, json=make_workspace("wks_01", status="starting"))
            if method == "POST" and path == "/v1/workspaces/wks_01/timeout":
                return httpx.Response(200, json=make_workspace("wks_01", status="available"))
            if method == "PUT" and path == "/v1/workspaces/wks_01/network":
                return httpx.Response(
                    202, json=make_workspace("wks_01", network_mode="deny_all")
                )
            if method == "PUT" and path == "/v1/workspaces/wks_01/volume":
                return httpx.Response(
                    200,
                    json=make_workspace(
                        "wks_01",
                        volume={
                            "id": "vol_01",
                            "mount_path": "/data",
                            "delete_with_workspace": False,
                        },
                    ),
                )
            if method == "DELETE" and path == "/v1/workspaces/wks_01/volume":
                return httpx.Response(200, json=make_workspace("wks_01", volume=None))
            if method == "DELETE" and path == "/v1/workspaces/wks_01":
                return httpx.Response(202, json=make_workspace("wks_01", status="deleting"))
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            created = client.workspaces.create(
                template=WorkspaceTemplateRef(id="python-3.12", revision="r42"),
                name="agent-runner",
                zone="global-1",
                resources=WorkspaceResources(vcpus=2, memory_mib=4096, disk_mib=20480),
                network=WorkspaceNetwork(mode="public_internet"),
                volume=WorkspaceVolume(id="vol_01", mount_path="/data", delete_with_workspace=False),
                timeout=WorkspaceTimeoutSpec(seconds=3600, action="stop"),
                labels={"team": "ml"},
            )
            self.assertEqual(created.status, "creating")

            all_wks = list(
                client.workspaces.iterate(
                    status="available",
                    zone="global-1",
                    labels={"team": "ml", "env": "prod"},
                    limit=1,
                )
            )
            self.assertEqual([w.id for w in all_wks], ["wks_01", "wks_02"])

            fetched = client.workspaces.get("wks_01")
            self.assertEqual(fetched.id, "wks_01")

            updated = client.workspaces.update(
                "wks_01",
                name=None,
                labels={"k": "v", "del_me": None},
            )
            self.assertIsNone(updated.name)

            stopped = client.workspaces.stop("wks_01", force=True)
            self.assertEqual(stopped.status, "stopping")

            started = client.workspaces.start(
                "wks_01",
                timeout=WorkspaceTimeoutSpec(seconds=7200, action="stop"),
            )
            self.assertEqual(started.status, "starting")

            extended = client.workspaces.set_timeout("wks_01", seconds=1800, action="stop")
            self.assertEqual(extended.status, "available")

            net_wks = client.workspaces.set_network("wks_01", mode="deny_all")
            self.assertEqual(net_wks.network.mode, "deny_all")

            vol_wks = client.workspaces.attach_volume(
                "wks_01",
                volume_id="vol_01",
                mount_path="/data",
                delete_with_workspace=False,
            )
            assert vol_wks.volume is not None
            self.assertEqual(vol_wks.volume.id, "vol_01")

            detached = client.workspaces.detach_volume("wks_01")
            self.assertIsNone(detached.volume)

            deleted = client.workspaces.delete("wks_01")
            self.assertEqual(deleted.status, "deleting")

        # Verify label.<k>=<v> query serialization on GET /v1/workspaces.
        list_params = recorded[1][3]
        self.assertIn(("status", "available"), list_params)
        self.assertIn(("zone", "global-1"), list_params)
        self.assertIn(("label.team", "ml"), list_params)
        self.assertIn(("label.env", "prod"), list_params)

        # Verify PATCH sends explicit null for name and deleted label.
        patch_body = recorded[4][2]
        self.assertEqual(patch_body, {"name": None, "labels": {"k": "v", "del_me": None}})

    def test_workspace_wait_chunks_over_60s_and_raises_on_exhaustion(self) -> None:
        observed_timeouts: list[str] = []
        observed_statuses: list[list[str]] = []

        def handler_succeeds(request: httpx.Request) -> httpx.Response:
            observed_timeouts.append(request.url.params["timeout"])
            observed_statuses.append(request.url.params.get_list("status"))
            if len(observed_timeouts) < 3:
                return httpx.Response(
                    408,
                    json=make_error(
                        status=408,
                        code="wait_timeout",
                        message="timed out waiting",
                        retryable=True,
                        details={"workspace": make_workspace(status="creating")},
                    ),
                )
            return httpx.Response(200, json=make_workspace(status="available"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler_succeeds)) as client:
            wks = client.workspaces.wait("wks_01", status=["available", "failed"], timeout=125)

        self.assertEqual(wks.status, "available")
        self.assertEqual(observed_timeouts, ["60", "60", "5"])
        self.assertEqual(observed_statuses[0], ["available", "failed"])

        # Exhausted timeout raises WaitTimeoutError with details preserved.
        always_timeout = httpx.MockTransport(
            lambda req: httpx.Response(
                408,
                json=make_error(
                    status=408,
                    code="wait_timeout",
                    retryable=True,
                    details={"workspace": make_workspace(status="creating")},
                ),
            )
        )
        with Kibotos("kb_test", transport=always_timeout) as client:
            with self.assertRaises(WaitTimeoutError) as ctx:
                client.workspaces.wait("wks_01", status="available", timeout=70)
        self.assertEqual(ctx.exception.code, "wait_timeout")
        self.assertIn("workspace", ctx.exception.details)

        # UnexpectedStatusError propagates immediately without retrying.
        unexpected_calls = 0

        def handler_unexpected(request: httpx.Request) -> httpx.Response:
            nonlocal unexpected_calls
            unexpected_calls += 1
            return httpx.Response(
                409,
                json=make_error(
                    status=409,
                    code="unexpected_status",
                    message="workspace reached failed",
                    retryable=False,
                ),
            )

        with Kibotos("kb_test", transport=httpx.MockTransport(handler_unexpected)) as client:
            with self.assertRaises(UnexpectedStatusError):
                client.workspaces.wait("wks_01", status="available", timeout=120)
        self.assertEqual(unexpected_calls, 1)

    def test_volumes_endpoints_and_wait(self) -> None:
        list_params_seen: list[tuple[str, str]] = []
        wait_chunks: list[str] = []

        def handler(request: httpx.Request) -> httpx.Response:
            method, path = request.method, request.url.path
            if method == "POST" and path == "/v1/volumes":
                body = json.loads(request.content.decode())
                self.assertEqual(body["size_gib"], 50)
                return httpx.Response(202, json=make_volume(status="creating"))
            if method == "GET" and path == "/v1/volumes":
                list_params_seen.extend(request.url.params.multi_items())
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
                body = json.loads(request.content.decode())
                self.assertEqual(body, {"name": "datasets-v2", "labels": {"tier": "hot"}})
                return httpx.Response(
                    200, json=make_volume("vol_01", name="datasets-v2", labels={"tier": "hot"})
                )
            if method == "POST" and path == "/v1/volumes/vol_01/resize":
                body = json.loads(request.content.decode())
                self.assertEqual(body, {"size_gib": 100})
                return httpx.Response(202, json=make_volume("vol_01", size_gib=100, status="resizing"))
            if method == "GET" and path == "/v1/volumes/vol_01/wait":
                wait_chunks.append(request.url.params["timeout"])
                if len(wait_chunks) == 1:
                    return httpx.Response(
                        408, json=make_error(status=408, code="wait_timeout", retryable=True)
                    )
                return httpx.Response(200, json=make_volume("vol_01", size_gib=100, status="available"))
            if method == "GET" and path == "/v1/volumes/vol_lost":
                return httpx.Response(200, json=make_volume("vol_lost", status="lost"))
            if method == "DELETE" and path == "/v1/volumes/vol_01":
                return httpx.Response(202, json=make_volume("vol_01", status="deleting"))
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            vol = client.volumes.create(size_gib=50, name="datasets", zone="global-1")
            self.assertEqual(vol.status, "creating")

            vols = list(client.volumes.iterate(zone="global-1", attached=False, labels={"env": "dev"}))
            self.assertEqual([v.id for v in vols], ["vol_01"])
            self.assertIn(("attached", "false"), list_params_seen)
            self.assertIn(("label.env", "dev"), list_params_seen)

            fetched = client.volumes.get("vol_01")
            self.assertEqual(fetched.size_gib, 50)

            updated = client.volumes.update("vol_01", name="datasets-v2", labels={"tier": "hot"})
            self.assertEqual(updated.name, "datasets-v2")

            resizing = client.volumes.resize("vol_01", size_gib=100)
            self.assertEqual(resizing.status, "resizing")

            ready = client.volumes.wait("vol_01", status="available", timeout=90)
            self.assertEqual(ready.status, "available")
            self.assertEqual(wait_chunks, ["60", "30"])

            lost = client.volumes.get("vol_lost")
            self.assertEqual(lost.status, "lost")

            deleting = client.volumes.delete("vol_01")
            self.assertEqual(deleting.status, "deleting")

    def test_exposures_endpoints(self) -> None:
        def handler(request: httpx.Request) -> httpx.Response:
            method, path = request.method, request.url.path
            if method == "POST" and path == "/v1/workspaces/wks_01/exposures":
                body = json.loads(request.content.decode())
                self.assertEqual(body, {"port": 8080, "ttl_seconds": 1800})
                return httpx.Response(201, json=make_exposure("exp_01"))
            if method == "GET" and path == "/v1/workspaces/wks_01/exposures":
                self.assertEqual(request.url.params.get("include_revoked"), "true")
                return httpx.Response(
                    200,
                    json={"data": [make_exposure("exp_01")], "next_page_token": None},
                )
            if method == "GET" and path == "/v1/workspaces/wks_01/exposures/exp_01":
                return httpx.Response(200, json=make_exposure("exp_01"))
            if method == "DELETE" and path == "/v1/workspaces/wks_01/exposures/exp_01":
                return httpx.Response(
                    200,
                    json=make_exposure(
                        "exp_01",
                        status="revoked",
                        revoked_at="2026-11-21T12:10:00Z",
                    ),
                )
            return httpx.Response(404, json=make_error(status=404, code="not_found"))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            exp = client.exposures.create("wks_01", port=8080, ttl_seconds=1800)
            self.assertEqual(exp.url, "https://k7x2n4p6r8t0w2y6.kibo.run")

            items = list(client.exposures.iterate("wks_01", include_revoked=True))
            self.assertEqual([e.id for e in items], ["exp_01"])

            fetched = client.exposures.get("wks_01", "exp_01")
            self.assertEqual(fetched.port, 8080)

            revoked = client.exposures.revoke("wks_01", "exp_01")
            self.assertEqual(revoked.status, "revoked")
            self.assertIsNotNone(revoked.revoked_at)


if __name__ == "__main__":
    unittest.main()
