# Architecture

## Components

| Component | Type | Description |
|---|---|---|
| **Agent** | HTTP server (`:8080`) | Manages btrfs subvolumes, NFS exports, quota. Runs on storage host. |
| **Controller** | gRPC server (CSI) | Translates CSI calls to agent HTTP API. K8s Deployment. |
| **Node Driver** | gRPC server (CSI) | NFS mount + bind mount. Privileged DaemonSet. |

## Volume Lifecycle

```
CreateVolume   → POST /v1/volumes (btrfs subvolume create, compression, quota, chown)
Publish        → POST /v1/volumes/:name/export (exportfs, retry 3x)
NodeStage      → mount -t nfs server:path staging
NodePublish    → mount --bind staging/data target + fsGroup
NodeUnpublish  → umount target
NodeUnstage    → umount staging
Unpublish      → DELETE /v1/volumes/:name/export
DeleteVolume   → DELETE /v1/volumes/:name (subvolume delete)
```

## ID Formats

| ID | Format | Example |
|---|---|---|
| Volume | `{storageClassName}\|{name}` | `btrfs-nfs\|pvc-abc123` |
| Snapshot | `{storageClassName}\|{name}` | `btrfs-nfs\|snap-abc123` |
| Node | `{nodeName}\|{nodeIP}` | `worker-1\|10.1.0.50` |

## Directory Structure

```
{AGENT_BASE_PATH}/{tenant}/
├── {volume}/
│   ├── data/              ← btrfs subvolume
│   └── metadata.json
├── snapshots/{name}/
│   ├── data/              ← read-only btrfs snapshot
│   └── metadata.json
└── {clone}/
    ├── data/              ← writable btrfs snapshot
    └── metadata.json
```

## CSI Capabilities

**Controller:** `CREATE_DELETE_VOLUME`, `CREATE_DELETE_SNAPSHOT`, `EXPAND_VOLUME`, `CLONE_VOLUME`, `PUBLISH_UNPUBLISH_VOLUME`

**Node:** `STAGE_UNSTAGE_VOLUME`, `GET_VOLUME_STATS`, `VOLUME_MOUNT_GROUP`

**Plugin:** `CONTROLLER_SERVICE`

## Sidecars

| Sidecar | Version | Component |
|---|---|---|
| csi-provisioner | v5.3.0 | Controller |
| csi-attacher | v4.11.0 | Controller |
| csi-snapshotter | v8.5.0 | Controller |
| csi-resizer | v2.1.0 | Controller |
| node-driver-registrar | v2.16.0 | Node |
| livenessprobe | v2.18.0 | Both |

All controller sidecars use `--leader-election`.

## RBAC

**Controller** (`btrfs-nfs-csi-controller`): PV/PVC/SC/VolumeAttachment/events/nodes/pods/secrets/leases + snapshot CRDs (full access)

**Node** (`btrfs-nfs-csi-node`): PVC get/patch only

## HA

- **Controller:** 1 replica + leader election. Sidecars elect independently.
- **Node:** DaemonSet, rolling update max 1 unavailable.
- **Agent:** Stateful (local btrfs). NFS reconciler re-exports on restart.

| Failure | Impact | Recovery |
|---|---|---|
| Agent down | No new volumes/exports | Existing NFS mounts continue. Restart agent. |
| Controller down | No provisioning | Leader election recovers. |
| Node driver down | No new mounts on node | DaemonSet restarts. |

## StorageClass Model

Each StorageClass defines one agent + tenant pair:

- `agentURL` parameter → which agent to talk to
- `agentToken` secret → which tenant on that agent (token → tenant mapping via `AGENT_TENANTS`)

Volume IDs use the StorageClass name (`{storageClassName}|{name}`), not the agent URL. The controller resolves the agent URL at runtime from the StorageClass cache. This means agent URLs can change (IP, port) without breaking existing volumes.

## Multi-Tenancy

- One directory per tenant under `AGENT_BASE_PATH`
- Token → tenant mapping via `AGENT_TENANTS`
- All API ops scoped to authenticated tenant
- For stronger isolation: separate agents + separate StorageClasses
