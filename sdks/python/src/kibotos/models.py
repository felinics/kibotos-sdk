"""Pydantic v2 models for the Kibotos v1 API."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field


class KibotosModel(BaseModel):
    """Base model configured for forward-compatible v1 deserialization."""

    model_config = ConfigDict(extra="ignore", populate_by_name=True)


# Known status/enum string types (union with str preserves forward compatibility).
ZoneStatus = Literal["available", "draining"] | str
TemplateOwner = Literal["platform", "organization"] | str
# Maps 1:1 to native Kibotos WorkspaceState in lowercase.
WorkspaceStatus = (
    Literal[
        "creating",
        "available",
        "stopping",
        "stopped",
        "starting",
        "deleting",
        "failed",
        "unknown",
    ]
    | str
)
# Maps 1:1 to native Kibotos VolumeState in lowercase:
# workspace_id expresses binding; "available" means ready and not mounted to a
# running workspace; "attached" means mounted to a running workspace; "lost"
# means the underlying storage pool was lost.
VolumeStatus = (
    Literal[
        "creating",
        "available",
        "attached",
        "resizing",
        "deleting",
        "failed",
        "lost",
        "unknown",
    ]
    | str
)
ExposureStatus = Literal["active", "revoked", "expired"] | str
NetworkMode = Literal["public_internet", "deny_all"] | str
TimeoutAction = Literal["stop", "delete"] | str


class ResourceFailure(KibotosModel):
    """Failure details on a failed workspace, volume, or template revision."""

    code: str
    message: str
    retryable: bool


class Zone(KibotosModel):
    """A Kibotos compute zone."""

    id: str
    display_name: str
    region: str
    country: str | None = None
    status: ZoneStatus
    default: bool


class ZonePage(KibotosModel):
    """Paginated list of zones."""

    data: list[Zone]
    next_page_token: str | None = None


class TemplateLatestRevision(KibotosModel):
    """Summary of the latest revision of a template."""

    id: str
    status: str


class Template(KibotosModel):
    """A workspace template."""

    id: str
    owner: TemplateOwner
    display_name: str
    description: str | None = None
    default_revision: str | None = None
    latest_revision: TemplateLatestRevision | None = None
    zones: list[str] = Field(default_factory=list)
    created_at: datetime | None = None
    updated_at: datetime | None = None


class TemplatePage(KibotosModel):
    """Multi-zone paginated list of templates."""

    data: list[Template]
    unavailable_zones: list[str] = Field(default_factory=list)
    next_page_token: str | None = None


class TemplateRevision(KibotosModel):
    """A concrete build revision of a template."""

    id: str
    template_id: str
    status: str
    failure: ResourceFailure | None = None
    source: dict[str, Any] | None = None
    process: dict[str, Any] | None = None
    size_mib: int | None = None
    zones: list[str] = Field(default_factory=list)
    created_at: datetime | None = None
    ready_at: datetime | None = None


class TemplateRevisionPage(KibotosModel):
    """Paginated list of template revisions."""

    data: list[TemplateRevision]
    next_page_token: str | None = None


class WorkspaceTemplateRef(KibotosModel):
    """Reference to a template (and optional revision) for a workspace."""

    id: str
    revision: str | None = None


class WorkspaceResources(KibotosModel):
    """Compute resource allocation for a workspace."""

    vcpus: int | None = None
    memory_mib: int | None = None
    disk_mib: int | None = None


class WorkspaceNetwork(KibotosModel):
    """Network policy for a workspace."""

    mode: NetworkMode
    applied: bool | None = None


class WorkspaceVolume(KibotosModel):
    """Persistent volume attachment configuration on a workspace."""

    id: str
    mount_path: str
    delete_with_workspace: bool = False


class WorkspaceTimeout(KibotosModel):
    """Active timeout policy on a workspace."""

    expires_at: datetime | None = None
    action: TimeoutAction | None = None


class WorkspaceTimeoutSpec(KibotosModel):
    """Timeout configuration passed when creating, starting, or extending a workspace."""

    seconds: int
    action: TimeoutAction | None = None


class Workspace(KibotosModel):
    """A Kibotos microVM workspace."""

    id: str
    name: str | None = None
    zone: str
    template: WorkspaceTemplateRef
    status: WorkspaceStatus
    failure: ResourceFailure | None = None
    resources: WorkspaceResources
    network: WorkspaceNetwork
    volume: WorkspaceVolume | None = None
    timeout: WorkspaceTimeout | None = None
    labels: dict[str, str] = Field(default_factory=dict)
    created_at: datetime
    updated_at: datetime


class WorkspacePage(KibotosModel):
    """Multi-zone paginated list of workspaces."""

    data: list[Workspace]
    unavailable_zones: list[str] = Field(default_factory=list)
    next_page_token: str | None = None


class Volume(KibotosModel):
    """A persistent volume."""

    id: str
    name: str | None = None
    zone: str
    size_gib: int
    status: VolumeStatus
    failure: ResourceFailure | None = None
    workspace_id: str | None = None
    labels: dict[str, str] = Field(default_factory=dict)
    created_at: datetime
    updated_at: datetime


class VolumePage(KibotosModel):
    """Multi-zone paginated list of volumes."""

    data: list[Volume]
    unavailable_zones: list[str] = Field(default_factory=list)
    next_page_token: str | None = None


class Exposure(KibotosModel):
    """An HTTPS port exposure on a workspace."""

    id: str
    workspace_id: str
    port: int
    url: str
    status: ExposureStatus
    expires_at: datetime
    revoked_at: datetime | None = None
    created_at: datetime


class ExposurePage(KibotosModel):
    """Paginated list of workspace exposures."""

    data: list[Exposure]
    next_page_token: str | None = None
