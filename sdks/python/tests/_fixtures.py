"""Shared test fixtures matching openapi/v1-contract.md."""

from __future__ import annotations

from typing import Any


def make_zone(
    zone_id: str = "global-1",
    *,
    country: str | None = "JP",
    default: bool = True,
    status: str = "available",
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": zone_id,
        "display_name": f"Zone {zone_id}",
        "region": "global",
        "country": country,
        "status": status,
        "default": default,
    }
    if extra:
        data.update(extra)
    return data


def make_template(
    template_id: str = "python-3.12",
    *,
    owner: str = "platform",
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": template_id,
        "owner": owner,
        "display_name": "Python 3.12",
        "description": "Python 3.12 Linux environment",
        "default_revision": "r42",
        "latest_revision": {"id": "r43", "status": "ready"},
        "zones": ["global-1"],
        "created_at": "2026-11-21T12:00:00Z",
        "updated_at": "2026-11-21T12:00:05Z",
    }
    if extra:
        data.update(extra)
    return data


def make_template_revision(
    revision_id: str = "r42",
    *,
    template_id: str = "python-3.12",
    status: str = "ready",
) -> dict[str, Any]:
    return {
        "id": revision_id,
        "template_id": template_id,
        "status": status,
        "failure": None,
        "source": None,
        "process": None,
        "size_mib": 1024,
        "zones": ["global-1"],
        "created_at": "2026-11-21T12:00:00Z",
        "ready_at": "2026-11-21T12:01:00Z",
    }


def make_workspace(
    workspace_id: str = "wks_01",
    *,
    name: str | None = "agent-runner",
    status: str = "available",
    zone: str = "global-1",
    network_mode: str = "public_internet",
    volume: dict[str, Any] | None = None,
    labels: dict[str, str] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": workspace_id,
        "name": name,
        "zone": zone,
        "template": {"id": "python-3.12", "revision": "r42"},
        "status": status,
        "failure": None,
        "resources": {"vcpus": 2, "memory_mib": 4096, "disk_mib": 20480},
        "network": {"mode": network_mode, "applied": True},
        "volume": volume,
        "timeout": {"expires_at": "2026-11-21T13:00:00Z", "action": "stop"},
        "labels": labels if labels is not None else {"team": "ml"},
        "created_at": "2026-11-21T12:00:00Z",
        "updated_at": "2026-11-21T12:00:05Z",
    }
    if extra:
        data.update(extra)
    return data


def make_volume(
    volume_id: str = "vol_01",
    *,
    name: str | None = "datasets",
    size_gib: int = 50,
    status: str = "available",
    workspace_id: str | None = None,
    labels: dict[str, str] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    data: dict[str, Any] = {
        "id": volume_id,
        "name": name,
        "zone": "global-1",
        "size_gib": size_gib,
        "status": status,
        "failure": None,
        "workspace_id": workspace_id,
        "labels": labels if labels is not None else {},
        "created_at": "2026-11-21T12:00:00Z",
        "updated_at": "2026-11-21T12:00:00Z",
    }
    if extra:
        data.update(extra)
    return data


def make_exposure(
    exposure_id: str = "exp_01",
    *,
    workspace_id: str = "wks_01",
    port: int = 8080,
    status: str = "active",
    revoked_at: str | None = None,
) -> dict[str, Any]:
    return {
        "id": exposure_id,
        "workspace_id": workspace_id,
        "port": port,
        "url": "https://k7x2n4p6r8t0w2y6.kibo.run",
        "status": status,
        "expires_at": "2026-11-21T13:00:00Z",
        "revoked_at": revoked_at,
        "created_at": "2026-11-21T12:00:00Z",
    }


def make_error(
    *,
    status: int,
    code: str,
    message: str = "error occurred",
    retryable: bool = False,
    request_id: str = "req_test123",
    details: dict[str, Any] | None = None,
) -> dict[str, Any]:
    return {
        "error": {
            "code": code,
            "message": message,
            "status": status,
            "request_id": request_id,
            "retryable": retryable,
            "details": details or {},
        }
    }
