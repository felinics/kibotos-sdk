"""Resource namespaces for Kibotos sync and async clients."""

from .egresses import AsyncEgressesResource, EgressesResource
from .packs import AsyncPacksResource, PacksResource
from .resolvers import AsyncResolversResource, ResolversResource
from .exposures import AsyncExposuresResource, ExposuresResource
from .templates import AsyncTemplatesResource, TemplatesResource
from .volumes import AsyncVolumesResource, VolumesResource
from .workspaces import AsyncWorkspacesResource, WorkspacesResource
from .zones import AsyncZonesResource, ZonesResource

__all__ = [
    "AsyncResolversResource",
    "ResolversResource",
    "AsyncPacksResource",
    "PacksResource",
    "AsyncEgressesResource",
    "EgressesResource",
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
