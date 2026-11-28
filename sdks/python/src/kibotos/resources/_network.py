"""Shared network resource request serialization."""

from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from pydantic import BaseModel

from .._transport import _UnsetType, query_params, serialize_labels


def config_body(**fields: Any) -> dict[str, Any]:
    body: dict[str, Any] = {}
    for key, value in fields.items():
        if isinstance(value, BaseModel):
            body[key] = value.model_dump(mode="json", exclude_unset=True)
        elif isinstance(value, Mapping):
            body[key] = dict(value)
        elif value is not None:
            body[key] = value
    return body


def metadata_body(
    *,
    name: str | None | _UnsetType,
    labels: Mapping[str, str | None] | None,
) -> dict[str, Any]:
    body: dict[str, Any] = {}
    if not isinstance(name, _UnsetType):
        body["name"] = name
    if labels is not None:
        body["labels"] = dict(labels)
    return body


def list_params(
    zone: str | None, labels: Mapping[str, str] | None, limit: int | None, page_token: str | None,
) -> list[tuple[str, str]]:
    return query_params(zone=zone, limit=limit, page_token=page_token) + serialize_labels(labels)
