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
ExposureStatus = Literal["active", "revoked"] | str
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
    description: str | None = None
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


class NetworkEgressRule(KibotosModel):
    egress_id: str
    destinations: list[str]


class NetworkResolverPath(KibotosModel):
    type: Literal["direct", "egress"]
    egress_id: str | None = None


class NetworkResolverRule(KibotosModel):
    resolver_id: str
    path: NetworkResolverPath


class WorkspaceNetworkInput(KibotosModel):
    mode: NetworkMode
    allow: list[str] | None = None
    deny: list[str] | None = None
    egresses: list[NetworkEgressRule] | None = None
    resolvers: list[NetworkResolverRule] | None = None


class WorkspaceNetwork(WorkspaceNetworkInput):
    """Network policy and its application state in the workspace."""

    applied: bool | None = None


class WorkspaceVolumeInput(KibotosModel):
    id: str
    mount_path: str | None = None
    delete_with_workspace: bool | None = None


class WorkspaceVolume(KibotosModel):
    """Persistent volume attachment configuration on a workspace."""

    id: str
    mount_path: str
    delete_with_workspace: bool = False


class WorkspacePack(KibotosModel):
    id: str
    revision: str
    provided_capabilities: list[str]


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
    volumes: list[WorkspaceVolume] = Field(default_factory=list)
    packs: list[WorkspacePack] = Field(default_factory=list)
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


class WorkspaceGuestMetricsPoint(KibotosModel):
    time: datetime
    cpu_count: int
    cpu_used_percent: float
    disk_total_bytes: int
    disk_used_bytes: int
    memory_cache_bytes: int
    memory_total_bytes: int
    memory_used_bytes: int


class WorkspaceHostMetricsPoint(KibotosModel):
    time: datetime
    cpu_cores: float | None = None
    cpu_pressure_some_ratio: float | None = None
    io_pressure_full_ratio: float | None = None
    io_pressure_some_ratio: float | None = None
    memory_bytes: int | None = None
    memory_pressure_full_ratio: float | None = None
    memory_pressure_some_ratio: float | None = None
    network_receive_bytes_per_second: float | None = None
    network_receive_dropped_per_second: float | None = None
    network_receive_packets_per_second: float | None = None
    network_transmit_bytes_per_second: float | None = None
    network_transmit_dropped_per_second: float | None = None
    network_transmit_packets_per_second: float | None = None


class WorkspaceMetrics(KibotosModel):
    earliest_time: datetime | None
    guest: list[WorkspaceGuestMetricsPoint]
    host: list[WorkspaceHostMetricsPoint]
    step_seconds: int


class _PackRelease(KibotosModel):
    id: str
    architecture: str
    guest_requirements: list[str]
    provided_capabilities: list[str]
    published_at: datetime
    version: str
    zones: list[str]


class Pack(_PackRelease):
    default_revision: str
    description: str
    display_name: str


class PackRevision(_PackRelease):
    pack_id: str


class PackPage(KibotosModel):
    data: list[Pack]
    next_page_token: str | None = None
    unavailable_zones: list[str] = Field(default_factory=list)


class EgressTLS(KibotosModel):
    ca_cert: str | None = None
    server_name: str | None = None


class EgressSocks5(KibotosModel):
    address: str
    tls: EgressTLS | None = None
    username: str | None = None


class EgressSocks5Input(EgressSocks5):
    password: str | None = Field(default=None, repr=False)


class _ZonedNetworkResource(KibotosModel):
    id: str
    name: str | None = None
    zone: str
    labels: dict[str, str] = Field(default_factory=dict)
    revision: int
    created_at: datetime
    updated_at: datetime


class Egress(_ZonedNetworkResource):
    protocol: Literal["socks5"]
    socks5: EgressSocks5
    udp_enabled: bool


class EgressPage(KibotosModel):
    data: list[Egress]
    next_page_token: str | None = None
    unavailable_zones: list[str] = Field(default_factory=list)


class ResolverAddress(KibotosModel):
    address: str


class ResolverHTTPS(KibotosModel):
    url: str


ResolverProtocol = Literal["udp", "tcp", "tls", "https"]


class Resolver(_ZonedNetworkResource):
    protocol: ResolverProtocol
    udp: ResolverAddress | None = None
    tcp: ResolverAddress | None = None
    tls: ResolverAddress | None = None
    https: ResolverHTTPS | None = None


class ResolverPage(KibotosModel):
    data: list[Resolver]
    next_page_token: str | None = None
    unavailable_zones: list[str] = Field(default_factory=list)


class ExecRequest(KibotosModel):
    argv: list[str]
    env: dict[str, str] | None = None
    max_output_bytes: int | None = None
    stdin: str | None = None
    timeout_seconds: int | None = None
    user: str | None = None
    working_dir: str | None = None


class ExecResponse(KibotosModel):
    duration_ms: int
    exit_code: int | None = None
    signal: int | None = None
    stdout: str
    stderr: str
    termination_reason: Literal[
        "exited", "signaled", "timed_out", "canceled", "failed",
        "memory_saturated", "out_of_memory",
    ] | str
    truncated: bool


ExecEventName = Literal["started", "stdout", "stderr", "heartbeat", "exited", "error"]


class ExecEvent(KibotosModel):
    event: ExecEventName
    data: Any


class FileEntry(KibotosModel):
    path: str
    name: str
    type: str
    mode: str
    modified_at: datetime | None = None
    size_bytes: int


class FileList(KibotosModel):
    path: str
    entries: list[FileEntry]


class FileWriteResult(KibotosModel):
    path: str
    size_bytes: int
    sha256: str
