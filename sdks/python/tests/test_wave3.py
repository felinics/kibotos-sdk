"""Contract tests for the Wave 3 workspace, network and data-plane endpoints."""

from __future__ import annotations

import json
import unittest
from contextlib import aclosing, closing
from datetime import datetime, timezone

import httpx

from kibotos import (
    APIConnectionError,
    APIError,
    AsyncKibotos,
    EgressSocks5Input,
    ExecRequest,
    Kibotos,
    NetworkEgressRule,
    ResolverAddress,
    ResolverHTTPS,
    WorkspaceChannel,
    WorkspaceNetworkInput,
    WorkspaceResources,
    WorkspaceVolumeInput,
    decode_binary_frame,
    encode_binary_frame,
    encode_open_frame,
)
from tests._fixtures import make_error, make_workspace

ID = "id/a"
PREFIX = "/v1/workspaces/id%2Fa"
TIME = "2026-11-21T12:00:00Z"
PATH = "/some/a b+?.txt"
PACK_RELEASE = {
    "id": "pack/a", "architecture": "x86_64", "guest_requirements": [],
    "provided_capabilities": ["desktop.v1"], "published_at": TIME,
    "version": "1.0", "zones": ["global-1"],
}
PACK = {**PACK_RELEASE, "display_name": "Desktop", "description": "", "default_revision": "r?1"}
REVISION = {**PACK_RELEASE, "id": "r?1", "pack_id": PACK["id"]}
ATTACHMENT = {"id": "vol_1", "mount_path": "/mnt", "delete_with_workspace": False}
NETWORK = {
    "mode": "deny_all", "allow": ["203.0.113.0/24"], "deny": ["198.51.100.0/24"],
    "egresses": [{"egress_id": "egr", "destinations": ["10.0.0.0/8"]}],
    "resolvers": [{"resolver_id": "rsv", "path": {"type": "egress", "egress_id": "egr"}}],
}
WORKSPACE = make_workspace(
    volume=ATTACHMENT,
    extra={
        "volumes": [ATTACHMENT],
        "packs": [{"id": "desktop", "revision": "r1", "provided_capabilities": ["desktop.v1"]}],
        "network": {**NETWORK, "applied": None},
    },
)
METRICS = {
    "earliest_time": None, "guest": [], "host": [{"time": TIME, "cpu_cores": 0.5}],
    "step_seconds": 60,
}
FILE = {"path": PATH, "name": "a b+?.txt", "type": "file", "mode": "0640", "size_bytes": 2}
WRITE_RESULT = {"path": PATH, "size_bytes": 2, "sha256": "hash"}
EXEC_REQUEST = {"argv": ["echo", "日本語"], "timeout_seconds": 60}
EXEC_RESULT = {
    "duration_ms": 2, "exit_code": 0, "signal": None, "stdout": "ok", "stderr": "",
    "termination_reason": "exited", "truncated": False,
}
SSE = (
    "\ufeff: comment\r\nevent: started\r\ndata: {\"pid\":1}\r\n\r\n"
    'event: stdout\ndata: {"data":"日本語\u2028行"}\n\nevent: stderr\rdata: {\rdata: "data":"a\\nb"}\r\r'
    'event: heartbeat\ndata: {}\n\nevent: exited\ndata: {"exit_code":2}\n\n'
    'event: error\ndata: {"code":"internal"}\n\nevent: future\ndata: {}\n\n'
    'event: stdout\ndata: {"data":"incomplete"}\n'
)
EVENTS = [
    ("started", {"pid": 1}), ("stdout", {"data": "日本語\u2028行"}), ("stderr", {"data": "a\nb"}),
    ("heartbeat", {}), ("exited", {"exit_code": 2}), ("error", {"code": "internal"}),
]
RESOURCE = {
    "id": ID, "zone": "global-1", "revision": 2, "labels": {"team": "one"},
    "created_at": TIME, "updated_at": TIME,
}
EGRESS = {
    **RESOURCE, "protocol": "socks5", "udp_enabled": False, "socks5": {"address": "proxy.example:1080"},
}
RESOLVER = {**RESOURCE, "protocol": "udp", "udp": {"address": "10.0.0.53"}}


class Chunks(httpx.SyncByteStream, httpx.AsyncByteStream):
    def __init__(self, data: bytes) -> None:
        self.data = data
        self.closed = False

    def __iter__(self):
        for value in self.data:
            yield bytes([value])

    async def __aiter__(self):
        for chunk in self:
            yield chunk

    def close(self) -> None:
        self.closed = True

    async def aclose(self) -> None:
        self.close()


def raw_path(request: httpx.Request) -> str:
    return request.url.raw_path.split(b"?", 1)[0].decode()


def body(request: httpx.Request):
    return json.loads(request.content) if request.content else None


class MockAPI:
    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self.streams: list[Chunks] = []
        self.transport = httpx.MockTransport(self.handle)

    def handle(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        path, method = raw_path(request), request.method
        if method == "DELETE":
            return httpx.Response(204)
        if path.startswith("/v1/packs"):
            if path == "/v1/packs":
                return self.page(PACK)
            return httpx.Response(200, json=REVISION if "/revisions/" in path else PACK)
        for resource, value in (("egresses", EGRESS), ("resolvers", RESOLVER)):
            if path.startswith("/v1/" + resource):
                if path == "/v1/" + resource and method == "GET":
                    return self.page(value)
                return httpx.Response(200, json=value)
        if path.endswith("/metrics"):
            return httpx.Response(200, json=METRICS)
        if path.endswith("/exec"):
            if request.headers["Accept"] == "text/event-stream":
                chunks = Chunks(SSE.encode())
                self.streams.append(chunks)
                return httpx.Response(200, stream=chunks, headers={"Content-Type": "text/event-stream"})
            return httpx.Response(200, json=EXEC_RESULT)
        if path.endswith("/files/content"):
            if method == "PUT":
                return httpx.Response(200, json=WRITE_RESULT)
            if request.headers.get("If-None-Match") == '"tag"':
                return httpx.Response(304)
            chunks = Chunks(b"\x00\xff")
            self.streams.append(chunks)
            return httpx.Response(
                206, stream=chunks, headers={"Content-Range": "bytes 1-2/3", "ETag": '"tag"'},
            )
        if path.endswith("/files"):
            return httpx.Response(200, json={"path": "/some", "entries": [FILE]})
        if "/files/" in path:
            return httpx.Response(200, json=FILE)
        if path.startswith("/v1/workspaces"):
            return httpx.Response(200, json=WORKSPACE)
        raise AssertionError(f"Unexpected request: {method} {path}")

    @staticmethod
    def page(value: dict) -> httpx.Response:
        return httpx.Response(200, json={
            "data": [value], "next_page_token": "next", "unavailable_zones": ["global-2"],
        })


class TestWave3Sync(unittest.TestCase):
    def setUp(self) -> None:
        self.api = MockAPI()
        self.client = Kibotos("kb_test", transport=self.api.transport)
        self.addCleanup(self.client.close)

    def test_workspace_create_resize_replace_and_metrics(self) -> None:
        ws = self.client.workspaces
        result = ws.create(
            template="python", volumes=[WorkspaceVolumeInput(**ATTACHMENT)], packs=["desktop"],
            network=WorkspaceNetworkInput.model_validate(NETWORK),
        )
        self.assertEqual(result.volume, result.volumes[0])
        self.assertEqual(result.packs[0].provided_capabilities, ["desktop.v1"])
        self.assertEqual(result.network.egresses[0].egress_id, "egr")
        self.assertEqual(result.network.resolvers[0].path.egress_id, "egr")
        self.assertIsNone(result.network.applied)

        resize = WorkspaceResources(vcpus=4, memory_mib=8192, disk_mib=40960)
        ws.update(ID, name=None, labels={"old": None}, resources=resize)
        ws.set_volumes(ID, volumes=[])
        ws.set_packs(ID, packs=[])
        ws.set_network(
            ID, mode="deny_all", allow=[], deny=NETWORK["deny"],
            egresses=[NetworkEgressRule.model_validate(NETWORK["egresses"][0])],
            resolvers=NETWORK["resolvers"], drain_seconds=0,
        )
        start = datetime(2026, 11, 21, 12, tzinfo=timezone.utc)
        metrics = ws.get_metrics(ID, start=start, end=TIME)
        self.assertEqual(metrics.host[0].cpu_cores, 0.5)
        self.assertEqual(metrics.step_seconds, 60)
        self.assertIsNone(metrics.earliest_time)
        with self.assertRaisesRegex(ValueError, "timezone-aware"):
            ws.get_metrics(ID, start=start.replace(tzinfo=None))
        requests = self.api.requests
        self.assertEqual([(r.method, raw_path(r), body(r)) for r in requests], [
            ("POST", "/v1/workspaces", {
                "template": {"id": "python"}, "volumes": [ATTACHMENT],
                "packs": ["desktop"], "network": NETWORK,
            }),
            ("PATCH", PREFIX, {
                "name": None, "labels": {"old": None}, "resources": resize.model_dump(),
            }),
            ("PUT", PREFIX + "/volumes", {"volumes": []}),
            ("PUT", PREFIX + "/packs", {"packs": []}),
            ("PUT", PREFIX + "/network", {**NETWORK, "allow": [], "drain_seconds": 0}),
            ("GET", PREFIX + "/metrics", None),
        ])
        self.assertEqual(dict(requests[-1].url.params), {"start": start.isoformat(), "end": TIME})

    def test_pack_catalog_and_revision(self) -> None:
        packs = self.client.packs
        page = packs.list(zone="global-1", limit=5, page_token="a +")
        self.assertEqual(page.data[0].id, "pack/a")
        self.assertEqual(page.unavailable_zones, ["global-2"])
        self.assertEqual(page.next_page_token, "next")
        self.assertEqual(packs.get("pack/a").default_revision, "r?1")
        self.assertEqual(packs.get_revision("pack/a", "r?1").pack_id, "pack/a")
        self.assertEqual([raw_path(r) for r in self.api.requests], [
            "/v1/packs", "/v1/packs/pack%2Fa", "/v1/packs/pack%2Fa/revisions/r%3F1",
        ])
        self.assertEqual(dict(self.api.requests[0].url.params), {
            "zone": "global-1", "limit": "5", "page_token": "a +",
        })

    def test_egress_and_resolver_crud(self) -> None:
        for resource in ("egresses", "resolvers"):
            with self.subTest(resource=resource):
                self.api.requests.clear()
                api = getattr(self.client, resource)
                if resource == "egresses":
                    socks = EgressSocks5Input(address="proxy.example:1080", password="example")
                    result = api.create(socks5=socks, udp_enabled=False)
                    self.assertFalse(hasattr(result.socks5, "password"))
                    api.list(zone="global-1", labels={"team": "a/b"}, limit=5, page_token="p")
                    api.get(ID)
                    api.replace(ID, socks5=socks, drain_seconds=0)
                    create = {"protocol": "socks5", "socks5": socks.model_dump(exclude_unset=True)}
                    replace = {**create, "drain_seconds": 0}
                    create["udp_enabled"] = False
                else:
                    udp = ResolverAddress(address="10.0.0.53")
                    self.assertEqual(api.create(protocol="udp", udp=udp).udp, udp)
                    api.list(zone="global-1", labels={"team": "a/b"}, limit=5, page_token="p")
                    api.get(ID)
                    https = ResolverHTTPS(url="https://dns.example/dns-query")
                    api.replace(ID, protocol="https", https=https)
                    create = {"protocol": "udp", "udp": udp.model_dump()}
                    replace = {"protocol": "https", "https": https.model_dump()}
                patch = {"name": None, "labels": {"old": None}}
                api.update(ID, **patch)
                self.assertIsNone(api.delete(ID))
                route = "/v1/" + resource
                item = route + "/id%2Fa"
                requests = self.api.requests
                self.assertEqual([(r.method, raw_path(r), body(r)) for r in requests], [
                    ("POST", route, create), ("GET", route, None), ("GET", item, None),
                    ("PUT", item, replace), ("PATCH", item, patch), ("DELETE", item, None),
                ])
                self.assertTrue(requests[0].headers.get("Idempotency-Key"))
                self.assertEqual(dict(requests[1].url.params), {
                    "zone": "global-1", "limit": "5", "page_token": "p", "label.team": "a/b",
                })

    def test_exec_json_and_fragmented_stream(self) -> None:
        ws = self.client.workspaces
        self.assertEqual(ws.exec(ID, ExecRequest.model_validate(EXEC_REQUEST)).stdout, "ok")
        self.assertEqual([(e.event, e.data) for e in ws.exec_stream(ID, EXEC_REQUEST)], EVENTS)
        self.assertTrue(self.api.streams[0].closed)
        for index, accept in enumerate(("application/json", "text/event-stream")):
            request = self.api.requests[index]
            self.assertEqual((request.method, raw_path(request), body(request)), (
                "POST", PREFIX + "/exec", EXEC_REQUEST,
            ))
            self.assertEqual(request.headers["Accept"], accept)
            self.assertNotIn("Idempotency-Key", request.headers)

    def test_exec_stream_closes_on_early_exit(self) -> None:
        with closing(self.client.workspaces.exec_stream(ID, EXEC_REQUEST)) as events:
            self.assertEqual(next(events).event, "started")
            self.assertFalse(self.api.streams[0].closed)
        self.assertTrue(self.api.streams[0].closed)

    def test_exec_timeout_margin_and_streaming_timeout_overrides(self) -> None:
        ws = self.client.workspaces
        ws.exec(ID, {"argv": ["true"]})
        ws.exec(ID, EXEC_REQUEST)
        ws.exec(ID, EXEC_REQUEST, timeout=5)
        list(ws.exec_stream(ID, EXEC_REQUEST))
        ws.read_file(ID, PATH)
        ws.write_file(ID, PATH, iter([b"x"]))
        list(ws.exec_stream(ID, EXEC_REQUEST, timeout=6))
        ws.read_file(ID, PATH, timeout=7)
        ws.write_file(ID, PATH, iter([b"x"]), timeout=8)
        self.assertEqual([set(r.extensions["timeout"].values()) for r in self.api.requests], [
            {45}, {75}, {5}, {None}, {None}, {None}, {6}, {7}, {8},
        ])

    def test_file_methods_and_streaming_upload_and_download(self) -> None:
        ws = self.client.workspaces
        self.assertEqual(ws.list_files(ID, "/some", depth=1, user="root").entries[0].name, FILE["name"])
        self.assertEqual(ws.stat_file(ID, PATH, user="root").path, PATH)
        self.assertEqual(ws.make_dir(ID, "/some", mode="0750", user="root").mode, "0640")
        self.assertEqual(ws.move_file(ID, PATH, "/new", user="root").path, PATH)
        self.assertIsNone(ws.delete_file(ID, PATH, user="root"))
        self.assertEqual(ws.read_file(ID, PATH, range="bytes=1-2", user="root"), b"\x00\xff")
        with ws.read_file(ID, PATH, stream=True, range="bytes=1-2") as response:
            self.assertEqual(response.status_code, 206)
            self.assertEqual(response.headers["Content-Range"], "bytes 1-2/3")
            self.assertEqual(next(response.iter_bytes()), b"\x00")
        self.assertTrue(self.api.streams[-1].closed)
        with ws.read_file(ID, PATH, stream=True, if_none_match='"tag"') as response:
            self.assertEqual(response.status_code, 304)
        self.assertEqual(ws.write_file(
            ID, PATH, iter([b"\x00", b"\xff"]), mode="0640", create_only=False, user="root",
        ).model_dump(), WRITE_RESULT)

        r = self.api.requests
        self.assertEqual([(x.method, raw_path(x)) for x in r], [
            ("GET", PREFIX + "/files"), ("GET", PREFIX + "/files/stat"),
            ("POST", PREFIX + "/files/mkdir"), ("POST", PREFIX + "/files/move"),
            ("DELETE", PREFIX + "/files"), ("GET", PREFIX + "/files/content"),
            ("GET", PREFIX + "/files/content"), ("GET", PREFIX + "/files/content"),
            ("PUT", PREFIX + "/files/content"),
        ])
        self.assertEqual(dict(r[0].url.params), {"path": "/some", "depth": "1", "user": "root"})
        for i in (1, 4, 5):
            self.assertEqual(dict(r[i].url.params), {"path": PATH, "user": "root"})
        self.assertEqual(body(r[2]), {"path": "/some", "mode": "0750", "user": "root"})
        self.assertEqual(body(r[3]), {"source": PATH, "destination": "/new", "user": "root"})
        for request in r[2:5]:
            self.assertNotIn("Idempotency-Key", request.headers)
        self.assertEqual(r[5].headers["Range"], "bytes=1-2")
        self.assertEqual(r[7].headers["If-None-Match"], '"tag"')
        self.assertEqual(dict(r[8].url.params), {
            "path": PATH, "mode": "0640", "create_only": "false", "user": "root",
        })
        self.assertEqual(r[8].headers["Content-Type"], "application/octet-stream")
        self.assertEqual(r[8].headers["Transfer-Encoding"], "chunked")
        self.assertEqual(r[8].content, b"\x00\xff")

    def test_non_replayable_requests_and_streaming_errors(self) -> None:
        attempts = []

        def handler(request: httpx.Request) -> httpx.Response:
            attempts.append((request.method, raw_path(request)))
            self.assertNotIn("Idempotency-Key", request.headers)
            if request.method == "POST" and request.headers["Accept"] == "application/json":
                raise httpx.ReadError("dropped", request=request)
            return httpx.Response(503, json=make_error(
                status=503, code="unavailable", retryable=True,
            ))

        with Kibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            with self.assertRaises(APIConnectionError):
                client.workspaces.exec(ID, EXEC_REQUEST)
            for call in (
                lambda: next(client.workspaces.exec_stream(ID, EXEC_REQUEST)),
                lambda: client.workspaces.read_file(ID, PATH),
                lambda: client.workspaces.write_file(ID, PATH, iter([b"a"])),
                lambda: client.workspaces.delete_file(ID, PATH),
            ):
                with self.assertRaises(APIError) as ctx:
                    call()
                self.assertEqual(ctx.exception.code, "unavailable")
            for call in (
                lambda: client.workspaces.make_dir(ID, "/some"),
                lambda: client.workspaces.move_file(ID, PATH, "/new"),
            ):
                with self.assertRaises(APIConnectionError):
                    call()
        self.assertEqual(attempts, [(method, PREFIX + suffix) for method, suffix in [
            ("POST", "/exec"), ("POST", "/exec"), ("GET", "/files/content"),
            ("PUT", "/files/content"), ("DELETE", "/files"), ("POST", "/files/mkdir"),
            ("POST", "/files/move"),
        ]])

    def test_binary_frames_and_authenticated_urls(self) -> None:
        for channel in WorkspaceChannel:
            with self.subTest(channel=channel):
                wire = encode_binary_frame(0x010203FE, channel, b"\x00\xff")
                self.assertEqual(wire, bytes([1, 1, 2, 3, 254, channel, 0, 255]))
                self.assertEqual(decode_binary_frame(memoryview(b"x" + wire)[1:]), (
                    0x010203FE, channel, b"\x00\xff",
                ))
                self.assertEqual(decode_binary_frame(encode_binary_frame(0xFFFFFFFF, channel, b"")), (
                    0xFFFFFFFF, channel, b"",
                ))
        for wire in (
            b"short", b"\x02\x00\x00\x00\x01\x00", b"\x01\x00\x00\x00\x01\x05",
            b"\x01\x00\x00\x00\x00\x00",
        ):
            with self.assertRaises(ValueError):
                decode_binary_frame(wire)
        for stream_id in (0, -1, 0x100000000, 1.5, True):
            with self.assertRaisesRegex(ValueError, "stream_id"):
                encode_binary_frame(stream_id, 0, b"")
            with self.assertRaisesRegex(ValueError, "stream_id"):
                encode_open_frame(stream_id, "process")
        with self.assertRaises(ValueError):
            encode_binary_frame(1, 5, b"")
        target = {"target": {"type": "tcp", "port": 8000}}
        self.assertEqual(json.loads(encode_open_frame(1, "tunnel", target)), {
            "type": "open", "stream_id": 1, "kind": "tunnel", "params": target,
        })
        self.assertEqual(json.loads(encode_open_frame(0xFFFFFFFF, "process")), {
            "type": "open", "stream_id": 0xFFFFFFFF, "kind": "process", "params": {},
        })

        with Kibotos("kb_test", base_url="http://localhost:1234/api/") as client:
            ws = client.workspaces
            base = "localhost:1234/api" + PREFIX
            self.assertEqual(ws.ws_url(ID), f"ws://{base}/ws")
            self.assertEqual(ws.tunnel_ws_url(ID, port=80, family="ipv6"), (
                f"ws://{base}/tunnels/ws?port=80&family=ipv6"
            ))
            self.assertEqual(ws.tunnel_ws_url(ID, unix_path="/tmp/a+b.sock"), (
                f"ws://{base}/tunnels/ws?unix_path=%2Ftmp%2Fa%2Bb.sock"
            ))
            self.assertEqual(ws.desktop_vnc_url(ID), f"ws://{base}/desktop/vnc")
            query = [("q", "a +"), ("q", "b")]
            self.assertEqual(ws.proxy_url(ID, 80, "/a b/c?", query=query), (
                f"http://{base}/proxy/80/a%20b/c%3F?q=a+%2B&q=b"
            ))
            self.assertEqual(ws.proxy_ws_url(ID, 80), f"ws://{base}/proxy/80")
            self.assertEqual(ws.ws_headers(), {"Authorization": "Bearer kb_test"})
            with self.assertRaises(ValueError):
                ws.tunnel_ws_url(ID, port=80, unix_path="/tmp/socket")


class TestWave3Async(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.api = MockAPI()
        self.client = AsyncKibotos("kb_test", transport=self.api.transport)
        self.addAsyncCleanup(self.client.aclose)

    async def test_workspace_and_pack_mutations_match_sync_routes(self) -> None:
        ws = self.client.workspaces
        self.assertEqual((await ws.create(template="python", packs=[])).packs[0].id, "desktop")
        await ws.update(ID, resources={"memory_mib": 8192})
        await ws.set_volumes(ID, volumes=[ATTACHMENT])
        await ws.set_packs(ID, packs=["desktop"])
        await ws.set_network(ID, mode="deny_all", deny=[], drain_seconds=0)
        await ws.get_metrics(ID)
        with self.assertRaisesRegex(ValueError, "timezone-aware"):
            await ws.get_metrics(ID, end=datetime(2026, 11, 21))
        self.assertEqual((await self.client.packs.list()).data[0].id, "pack/a")
        self.assertEqual((await self.client.packs.get("pack/a")).default_revision, "r?1")
        self.assertEqual((await self.client.packs.get_revision("pack/a", "r?1")).pack_id, "pack/a")
        self.assertEqual([raw_path(r) for r in self.api.requests], [
            "/v1/workspaces", PREFIX, PREFIX + "/volumes", PREFIX + "/packs",
            PREFIX + "/network", PREFIX + "/metrics", "/v1/packs",
            "/v1/packs/pack%2Fa", "/v1/packs/pack%2Fa/revisions/r%3F1",
        ])
        self.assertEqual([body(r) for r in self.api.requests[:5]], [
            {"template": {"id": "python"}, "packs": []}, {"resources": {"memory_mib": 8192}},
            {"volumes": [ATTACHMENT]}, {"packs": ["desktop"]},
            {"mode": "deny_all", "deny": [], "drain_seconds": 0},
        ])
        self.assertEqual(ws.ws_url(ID), "wss://api.kibo.to" + PREFIX + "/ws")

    async def test_async_egresses_and_resolvers(self) -> None:
        egress = self.client.egresses
        socks = EgressSocks5Input(address="proxy.example:1080", password="example")
        self.assertEqual((await egress.create(socks5=socks)).revision, 2)
        self.assertEqual((await egress.list(labels={"team": "a/b"})).next_page_token, "next")
        self.assertEqual((await egress.get(ID)).protocol, "socks5")
        await egress.replace(ID, socks5=socks, drain_seconds=0)
        await egress.update(ID, labels={"remove": None})
        self.assertIsNone(await egress.delete(ID))

        resolvers = self.client.resolvers
        udp = ResolverAddress(address="10.0.0.53")
        await resolvers.create(protocol="udp", udp=udp)
        self.assertEqual((await resolvers.list(zone="global-1")).data[0].udp, udp)
        self.assertEqual((await resolvers.get(ID)).revision, 2)
        await resolvers.replace(ID, protocol="tcp", tcp=udp)
        await resolvers.update(ID, name=None)
        self.assertIsNone(await resolvers.delete(ID))

        for index, resource in ((0, "egresses"), (6, "resolvers")):
            requests = self.api.requests[index:index + 6]
            route = "/v1/" + resource
            self.assertEqual([(r.method, raw_path(r)) for r in requests], [
                ("POST", route), ("GET", route), ("GET", route + "/id%2Fa"),
                ("PUT", route + "/id%2Fa"), ("PATCH", route + "/id%2Fa"), ("DELETE", route + "/id%2Fa"),
            ])
        self.assertEqual(body(self.api.requests[3])["drain_seconds"], 0)
        self.assertEqual(body(self.api.requests[9]), {"protocol": "tcp", "tcp": udp.model_dump()})
        self.assertEqual(body(self.api.requests[10]), {"name": None})

    async def test_async_streams_exec_files_and_early_close(self) -> None:
        ws = self.client.workspaces
        self.assertEqual((await ws.exec(ID, EXEC_REQUEST)).exit_code, 0)
        self.assertEqual([(e.event, e.data) async for e in ws.exec_stream(ID, EXEC_REQUEST)], EVENTS)
        async with aclosing(ws.exec_stream(ID, EXEC_REQUEST)) as events:
            self.assertEqual((await anext(events)).event, "started")
            self.assertFalse(self.api.streams[-1].closed)
        self.assertTrue(self.api.streams[-1].closed)

        await ws.list_files(ID, "/some", depth=1)
        await ws.stat_file(ID, PATH)
        await ws.make_dir(ID, "/some")
        await ws.move_file(ID, PATH, "/new")
        await ws.delete_file(ID, PATH)
        self.assertEqual(await ws.read_file(ID, PATH), b"\x00\xff")
        async with await ws.read_file(ID, PATH, stream=True, range="bytes=1-2") as response:
            self.assertEqual(response.status_code, 206)
            self.assertEqual(await anext(response.aiter_bytes()), b"\x00")
        self.assertTrue(self.api.streams[-1].closed)

        async def chunks():
            yield b"\x00"
            yield b"\xff"

        self.assertEqual((await ws.write_file(ID, PATH, chunks(), create_only=True)).size_bytes, 2)
        r = self.api.requests
        self.assertEqual([raw_path(x) for x in r], [PREFIX + suffix for suffix in [
            "/exec", "/exec", "/exec", "/files", "/files/stat", "/files/mkdir", "/files/move",
            "/files", "/files/content", "/files/content", "/files/content",
        ]])
        self.assertEqual(r[9].headers["Range"], "bytes=1-2")
        self.assertEqual(r[10].url.params["create_only"], "true")
        self.assertEqual(r[10].headers["Transfer-Encoding"], "chunked")
        self.assertEqual(r[10].content, b"\x00\xff")
        self.assertEqual(set(r[0].extensions["timeout"].values()), {75})
        for index in (1, 2, 8, 9, 10):
            self.assertEqual(set(r[index].extensions["timeout"].values()), {None})
        for request in r[5:8]:
            self.assertNotIn("Idempotency-Key", request.headers)

    async def test_file_mutations_do_not_retry_dropped_responses(self) -> None:
        attempts = []

        def handler(request: httpx.Request) -> httpx.Response:
            attempts.append(raw_path(request))
            self.assertNotIn("Idempotency-Key", request.headers)
            raise httpx.ReadError("dropped", request=request)

        async with AsyncKibotos("kb_test", transport=httpx.MockTransport(handler)) as client:
            ws = client.workspaces
            for call in (ws.make_dir(ID, "/some"), ws.move_file(ID, PATH, "/new"), ws.delete_file(ID, PATH)):
                with self.assertRaises(APIConnectionError):
                    await call
        self.assertEqual(attempts, [PREFIX + suffix for suffix in ("/files/mkdir", "/files/move", "/files")])
