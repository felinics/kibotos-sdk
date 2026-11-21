"""End-to-end quickstart example for the Kibotos Python SDK.

Usage:
    export KIBOTOS_API_KEY="kb_..."
    python3 examples/python/quickstart.py
"""

from __future__ import annotations

import os
import sys

from kibotos import (
    Kibotos,
    KibotosError,
    WorkspaceResources,
    WorkspaceTimeoutSpec,
)


def main() -> int:
    if not os.environ.get("KIBOTOS_API_KEY"):
        print("Set KIBOTOS_API_KEY=kb_... before running this example.", file=sys.stderr)
        return 1

    with Kibotos() as client:
        # 1. Discover available zones and platform templates.
        zones = client.zones.list()
        print("Available zones:", [f"{z.id} ({'default' if z.default else z.status})" for z in zones.data])

        templates = list(client.templates.iterate(owner="platform", limit=20))
        if not templates:
            print("No templates available.", file=sys.stderr)
            return 1
        template_id = templates[0].id
        print(f"Using template: {template_id}")

        # 2. Provision a persistent volume and wait until it is available.
        volume = client.volumes.create(
            name="quickstart-data",
            size_gib=10,
            labels={"env": "quickstart"},
        )
        volume = client.volumes.wait(volume.id, status=["available", "failed"], timeout=90)
        print(f"Volume ready: {volume.id} ({volume.size_gib} GiB, status={volume.status})")

        workspace_id: str | None = None
        try:
            # 3. Create a workspace attached to the volume and wait until running.
            workspace = client.workspaces.create(
                template=template_id,
                name="quickstart-workspace",
                resources=WorkspaceResources(vcpus=2, memory_mib=4096),
                volume={
                    "id": volume.id,
                    "mount_path": "/data",
                    "delete_with_workspace": False,
                },
                timeout=WorkspaceTimeoutSpec(seconds=1800, action="stop"),
                labels={"env": "quickstart"},
            )
            workspace_id = workspace.id
            workspace = client.workspaces.wait(
                workspace.id,
                status=["running", "failed"],
                timeout=120,
            )
            print(f"Workspace running: {workspace.id} in zone {workspace.zone}")

            # 4. Expose port 8080 over HTTPS.
            exposure = client.exposures.create(workspace.id, port=8080, ttl_seconds=1800)
            print(f"HTTPS exposure active: {exposure.url} (id={exposure.id})")

            # 5. Revoke exposure before tearing down.
            revoked = client.exposures.revoke(workspace.id, exposure.id)
            print(f"Exposure status after revoke: {revoked.status}")
        finally:
            if workspace_id:
                print(f"Deleting workspace {workspace_id}...")
                client.workspaces.delete(workspace_id)
            print(f"Deleting volume {volume.id}...")
            client.volumes.delete(volume.id)

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KibotosError as err:
        print(f"Kibotos API error: {err}", file=sys.stderr)
        raise SystemExit(1)
