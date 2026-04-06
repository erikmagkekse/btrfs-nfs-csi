# Kubernetes CSI Configuration

See also: [Agent and CLI configuration](../../configuration.md) for shared environment variables (API client, TLS, etc.)

## Controller Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |
| `DRIVER_DEFAULT_LABELS` | - | Default labels for all volumes (`key=value,key=value`) |

## Node Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_NODE_ID` | **required** | Node name (`spec.nodeName`) |
| `DRIVER_NODE_IP` | - | Static IP (fallback) |
| `DRIVER_STORAGE_INTERFACE` | - | Storage NIC name (priority 1) |
| `DRIVER_STORAGE_CIDR` | - | Storage subnet CIDR (priority 2) |
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |
| `DRIVER_HEALTH_CHECK_INTERVAL` | `30s` | NFS mount health check interval. Set to `0` to disable. |

**IP resolution order:** `DRIVER_STORAGE_INTERFACE` > `DRIVER_STORAGE_CIDR` > `DRIVER_NODE_IP`. At least one required.

**Note:** `hostNetwork: true` is the default and recommended setting. NFS4 client sessions are tied to the pod's hostname; without host networking, every DaemonSet rolling update (e.g. `helm upgrade`) orphans all active NFS4 sessions, causing stale mounts.

## StorageClass Parameters

Each StorageClass binds one agent + one tenant. See [StorageClass model](architecture.md#storageclass-model) for details.

| Parameter | Required | Description |
|---|---|---|
| `nfsServer` | yes | NFS server IP |
| `agentURL` | yes | Agent REST API URL |
| `nfsMountOptions` | no | NFS mount options |
| `nocow` | no | `"true"` / `"false"` |
| `compression` | no | `zstd`, `lzo`, `zlib`, `none` (with level: `zstd:3`, `zlib:6`) |
| `uid` / `gid` | no | Volume owner |
| `mode` | no | Octal permissions (default `"2770"`) |

## PVC Annotations

| Annotation | Values |
|---|---|
| `btrfs-nfs-csi/nocow` | `"true"`, `"false"` |
| `btrfs-nfs-csi/compression` | `"zstd"`, `"lzo"`, `"zlib"`, `"none"` |
| `btrfs-nfs-csi/uid` | integer (0-65534) |
| `btrfs-nfs-csi/gid` | integer (0-65534) |
| `btrfs-nfs-csi/mode` | octal string (0000-7777) |
| `btrfs-nfs-csi/labels` | `key=value,key=value` (max 8 user labels) |

Annotations override StorageClass defaults. Applied at create and on every attach.

## Default Labels

The CSI controller automatically sets these labels on every agent volume it creates:

| Label | Value |
|---|---|
| `kubernetes.pvc.name` | PVC name |
| `kubernetes.pvc.namespace` | PVC namespace |
| `kubernetes.pvc.storageclassname` | StorageClass name |
| `created-by` | `k8s` |

On snapshots:

| Label | Value |
|---|---|
| `kubernetes.source.pvc.name` | Source PVC name |
| `kubernetes.source.pvc.namespace` | Source PVC namespace |
| `kubernetes.snapshot.name` | VolumeSnapshot name |
| `kubernetes.snapshot.namespace` | VolumeSnapshot namespace |
| `created-by` | `k8s` |

On NFS exports:

| Label | Value |
|---|---|
| `kubernetes.pv.name` | PV name |
| `kubernetes.pvc.name` | PVC name |
| `kubernetes.node.name` | Node hostname |
| `kubernetes.volumeattachment.name` | VolumeAttachment name |
| `created-by` | `k8s` |

Clones always receive `clone.source.type` and `clone.source.name` automatically. All `kubernetes.*` and `clone.*` keys are reserved and cannot be overridden via annotations. Max 8 user labels, max 32 total.

## User Labels via Annotations

```yaml
annotations:
  btrfs-nfs-csi/labels: "env=prod,team=backend"
```

User labels are never inherited. Each resource reads only its own annotation.

## Custom Default Labels

Set `DRIVER_DEFAULT_LABELS` on the controller:

```yaml
env:
  - name: DRIVER_DEFAULT_LABELS
    value: "kubernetes.cluster=my-cluster,env=prod"
```

Merged after built-in defaults but before user annotations. User annotations win on key conflict.

## Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: btrfs-nfs-creds
  namespace: btrfs-nfs-csi
type: Opaque
stringData:
  agentToken: "changeme"  # must match AGENT_TENANTS token
```
