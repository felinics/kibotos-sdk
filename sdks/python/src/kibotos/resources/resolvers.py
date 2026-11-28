"""Resolvers resource for Kibotos sync and async clients."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from .._transport import UNSET, AsyncTransport, SyncTransport, _UnsetType, encode_path_segment
from ..models import Resolver, ResolverPage, ResolverAddress, ResolverHTTPS, ResolverProtocol
from ._network import config_body, list_params, metadata_body


class ResolversResource:
    def __init__(self, transport: SyncTransport) -> None:
        self._transport = transport

    def create(
        self,
        *,
        protocol: ResolverProtocol,
        udp: ResolverAddress | Mapping[str, Any] | None = None,
        tcp: ResolverAddress | Mapping[str, Any] | None = None,
        tls: ResolverAddress | Mapping[str, Any] | None = None,
        https: ResolverHTTPS | Mapping[str, Any] | None = None,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Resolver:
        body = config_body(
            protocol=protocol, udp=udp, tcp=tcp, tls=tls, https=https, name=name, zone=zone,
            labels=labels,
        )
        data = self._transport.request(
            "POST", "/resolvers", json_body=body, idempotency_key=idempotency_key,
        )
        return Resolver.model_validate(data)

    def list(
        self,
        *,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ResolverPage:
        data = self._transport.request(
            "GET", "/resolvers", params=list_params(zone, labels, limit, page_token),
        )
        return ResolverPage.model_validate(data)

    def get(self, resolver_id: str) -> Resolver:
        data = self._transport.request("GET", f"/resolvers/{encode_path_segment(resolver_id)}")
        return Resolver.model_validate(data)

    def replace(
        self,
        resolver_id: str,
        *,
        protocol: ResolverProtocol,
        udp: ResolverAddress | Mapping[str, Any] | None = None,
        tcp: ResolverAddress | Mapping[str, Any] | None = None,
        tls: ResolverAddress | Mapping[str, Any] | None = None,
        https: ResolverHTTPS | Mapping[str, Any] | None = None,
    ) -> Resolver:
        body = config_body(
            protocol=protocol, udp=udp, tcp=tcp, tls=tls, https=https,
        )
        data = self._transport.request(
            "PUT", f"/resolvers/{encode_path_segment(resolver_id)}", json_body=body,
        )
        return Resolver.model_validate(data)

    def update(
        self,
        resolver_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Resolver:
        data = self._transport.request(
            "PATCH", f"/resolvers/{encode_path_segment(resolver_id)}",
            json_body=metadata_body(name=name, labels=labels),
        )
        return Resolver.model_validate(data)

    def delete(self, resolver_id: str) -> None:
        self._transport.request("DELETE", f"/resolvers/{encode_path_segment(resolver_id)}")


class AsyncResolversResource:
    def __init__(self, transport: AsyncTransport) -> None:
        self._transport = transport

    async def create(
        self,
        *,
        protocol: ResolverProtocol,
        udp: ResolverAddress | Mapping[str, Any] | None = None,
        tcp: ResolverAddress | Mapping[str, Any] | None = None,
        tls: ResolverAddress | Mapping[str, Any] | None = None,
        https: ResolverHTTPS | Mapping[str, Any] | None = None,
        name: str | None = None,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        idempotency_key: str | None = None,
    ) -> Resolver:
        body = config_body(
            protocol=protocol, udp=udp, tcp=tcp, tls=tls, https=https, name=name, zone=zone,
            labels=labels,
        )
        data = await self._transport.request(
            "POST", "/resolvers", json_body=body, idempotency_key=idempotency_key,
        )
        return Resolver.model_validate(data)

    async def list(
        self,
        *,
        zone: str | None = None,
        labels: Mapping[str, str] | None = None,
        limit: int | None = None,
        page_token: str | None = None,
    ) -> ResolverPage:
        data = await self._transport.request(
            "GET", "/resolvers", params=list_params(zone, labels, limit, page_token),
        )
        return ResolverPage.model_validate(data)

    async def get(self, resolver_id: str) -> Resolver:
        data = await self._transport.request("GET", f"/resolvers/{encode_path_segment(resolver_id)}")
        return Resolver.model_validate(data)

    async def replace(
        self,
        resolver_id: str,
        *,
        protocol: ResolverProtocol,
        udp: ResolverAddress | Mapping[str, Any] | None = None,
        tcp: ResolverAddress | Mapping[str, Any] | None = None,
        tls: ResolverAddress | Mapping[str, Any] | None = None,
        https: ResolverHTTPS | Mapping[str, Any] | None = None,
    ) -> Resolver:
        body = config_body(
            protocol=protocol, udp=udp, tcp=tcp, tls=tls, https=https,
        )
        data = await self._transport.request(
            "PUT", f"/resolvers/{encode_path_segment(resolver_id)}", json_body=body,
        )
        return Resolver.model_validate(data)

    async def update(
        self,
        resolver_id: str,
        *,
        name: str | None | _UnsetType = UNSET,
        labels: Mapping[str, str | None] | None = None,
    ) -> Resolver:
        data = await self._transport.request(
            "PATCH", f"/resolvers/{encode_path_segment(resolver_id)}",
            json_body=metadata_body(name=name, labels=labels),
        )
        return Resolver.model_validate(data)

    async def delete(self, resolver_id: str) -> None:
        await self._transport.request("DELETE", f"/resolvers/{encode_path_segment(resolver_id)}")
