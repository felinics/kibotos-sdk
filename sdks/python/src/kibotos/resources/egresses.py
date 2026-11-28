"""Egresses resource for Kibotos sync and async clients."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any, Literal

from .._transport import UNSET, AsyncTransport, SyncTransport, _UnsetType, encode_path_segment
from ..models import Egress, EgressPage, EgressSocks5Input
from ._network import config_body, list_params, metadata_body


class EgressesResource:
    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def create(
        self,
        *,
        socks5: EgressSocks5Input | Mapping[str, Any],
        protocol: Literal["socks5"] = "socks5",
        udp_enabled: bool | None = None,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Egress:
        body = config_body(
            protocol=protocol, socks5=socks5, udp_enabled=udp_enabled, name=name, zone=zone,
            labels=labels,
        )
        data = self._transport.request(
            "POST", "/egresses", json_body=body, idempotency_key=idempotency_key,
        )
        return Egress.model_validate(data)

    def list(
        self,
        *,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> EgressPage:
        data = self._transport.request(
            "GET", "/egresses", params=list_params(zone, labels, limit, page_token),
        )
        return EgressPage.model_validate(data)

    def get(self, egress_id: str) -> Egress:
        data = self._transport.request("GET", f"/egresses/{encode_path_segment(egress_id)}")
        return Egress.model_validate(data)

    def replace(
        self,
        egress_id: str,
        *,
        socks5: EgressSocks5Input | Mapping[str, Any],
        protocol: Literal["socks5"] = "socks5",
        udp_enabled: bool | None = None,
        drain_seconds: int | None = None,
    ) -> Egress:
        body = config_body(
            protocol=protocol, socks5=socks5, udp_enabled=udp_enabled, drain_seconds=drain_seconds,
        )
        data = self._transport.request(
            "PUT", f"/egresses/{encode_path_segment(egress_id)}", json_body=body,
        )
        return Egress.model_validate(data)

    def update(
        self,
        egress_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Egress:
        data = self._transport.request(
            "PATCH", f"/egresses/{encode_path_segment(egress_id)}",
            json_body=metadata_body(name=name, labels=labels),
        )
        return Egress.model_validate(data)

    def delete(self, egress_id: str) -> None:
        self._transport.request("DELETE", f"/egresses/{encode_path_segment(egress_id)}")


class AsyncEgressesResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def create(
        self,
        *,
        socks5: EgressSocks5Input | Mapping[str, Any],
        protocol: Literal["socks5"] = "socks5",
        udp_enabled: bool | None = None,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Egress:
        body = config_body(
            protocol=protocol, socks5=socks5, udp_enabled=udp_enabled, name=name, zone=zone,
            labels=labels,
        )
        data = await self._transport.request(
            "POST", "/egresses", json_body=body, idempotency_key=idempotency_key,
        )
        return Egress.model_validate(data)

    async def list(
        self,
        *,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> EgressPage:
        data = await self._transport.request(
            "GET", "/egresses", params=list_params(zone, labels, limit, page_token),
        )
        return EgressPage.model_validate(data)

    async def get(self, egress_id: str) -> Egress:
        data = await self._transport.request("GET", f"/egresses/{encode_path_segment(egress_id)}")
        return Egress.model_validate(data)

    async def replace(
        self,
        egress_id: str,
        *,
        socks5: EgressSocks5Input | Mapping[str, Any],
        protocol: Literal["socks5"] = "socks5",
        udp_enabled: bool | None = None,
        drain_seconds: int | None = None,
    ) -> Egress:
        body = config_body(
            protocol=protocol, socks5=socks5, udp_enabled=udp_enabled, drain_seconds=drain_seconds,
        )
        data = await self._transport.request(
            "PUT", f"/egresses/{encode_path_segment(egress_id)}", json_body=body,
        )
        return Egress.model_validate(data)

    async def update(
        self,
        egress_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Egress:
        data = await self._transport.request(
            "PATCH", f"/egresses/{encode_path_segment(egress_id)}",
            json_body=metadata_body(name=name, labels=labels),
        )
        return Egress.model_validate(data)

    async def delete(self, egress_id: str) -> None:
        await self._transport.request("DELETE", f"/egresses/{encode_path_segment(egress_id)}")
