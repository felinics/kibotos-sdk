"""Resource namespaces for Kibotos sync and async clients."""

from .exposures import AsyncExposuresResource, ExposuresResource
from .templates import AsyncTemplatesResource, TemplatesResource
from .volumes import AsyncVolumesResource, VolumesResource
from .workspaces import AsyncWorkspacesResource, WorkspacesResource
from .zones import AsyncZonesResource, ZonesResource

__all__ = [
    "AsyncExposuresResource",
    "AsyncTemplatesResource",
    "AsyncVolumesResource",
    "AsyncWorkspacesResource",
    "AsyncZonesResource",
    "ExposuresResource",
    "TemplatesResource",
    "VolumesResource",
    "WorkspacesResource",
    "ZonesResource",
]
