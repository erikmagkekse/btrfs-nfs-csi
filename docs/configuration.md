# Configuration

## Agent Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENT_BASE_PATH` | `./storage` | btrfs mount point |
| `AGENT_TENANTS` | **required** | `name:token,name:token` |
| `AGENT_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `AGENT_TLS_CERT` | - | TLS certificate path |
| `AGENT_TLS_KEY` | - | TLS key path |
| `AGENT_FEATURE_QUOTA_ENABLED` | `true` | btrfs quota tracking |
| `AGENT_FEATURE_QUOTA_UPDATE_INTERVAL` | `1m` | Usage update interval |
| `AGENT_NFS_EXPORTER` | `kernel` | NFS exporter type |
| `AGENT_EXPORTFS_BIN` | `exportfs` | exportfs binary path |
| `AGENT_BTRFS_BIN` | `btrfs` | btrfs binary path |
| `AGENT_NFS_RECONCILE_INTERVAL` | `10m` | Export reconciliation (`0` = off) |
| `AGENT_DASHBOARD_REFRESH_SECONDS` | `5` | Dashboard refresh |
| `AGENT_DEFAULT_DIR_MODE` | `0700` | Default mode for volume/snapshot/clone directories |
| `AGENT_DEFAULT_DATA_MODE` | `2770` | Default mode for data subvolumes (setgid + group rwx) |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

## Controller Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |

## Node Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DRIVER_NODE_ID` | **required** | Node name (`spec.nodeName`) |
| `DRIVER_NODE_IP` | - | Static IP (fallback) |
| `DRIVER_STORAGE_INTERFACE` | - | Storage NIC name (priority 1) |
| `DRIVER_STORAGE_CIDR` | - | Storage subnet CIDR (priority 2) |
| `DRIVER_ENDPOINT` | `unix:///csi/csi.sock` | gRPC socket |
| `DRIVER_METRICS_ADDR` | `:9090` | Metrics address |

**IP resolution order:** `DRIVER_STORAGE_INTERFACE` > `DRIVER_STORAGE_CIDR` > `DRIVER_NODE_IP`. At least one required.

**Note:** `DRIVER_STORAGE_INTERFACE` and `DRIVER_STORAGE_CIDR` resolve IPs from the host's network interfaces. The node DaemonSet must have `hostNetwork: true` for this to work.

## StorageClass Parameters

Each StorageClass binds one agent + one tenant. The SC name is used in volume IDs (`{storageClassName}|{volumeName}`) - do not rename it after creating volumes. The `agentURL` can be changed safely (e.g. IP change, port change).

| Parameter | Required | Description |
|---|---|---|
| `nfsServer` | yes | NFS server IP |
| `agentURL` | yes | Agent REST API URL |
| `nfsMountOptions` | no | NFS mount options |
| `nocow` | no | `"true"` / `"false"` |
| `compression` | no | `zstd`, `lzo`, `zlib`, `none` (with level: `zstd:3`) |
| `uid` / `gid` | no | Volume owner |
| `mode` | no | Octal permissions (default `"2770"`) |

## PVC Annotations

| Annotation | Values |
|---|---|
| `btrfs-nfs-csi/nocow` | `"true"`, `"false"` |
| `btrfs-nfs-csi/compression` | `"zstd"`, `"lzo"`, `"zlib"`, `"none"` |
| `btrfs-nfs-csi/uid` | integer |
| `btrfs-nfs-csi/gid` | integer |
| `btrfs-nfs-csi/mode` | octal string |

Annotations override StorageClass defaults. Applied at create and on every attach.

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

## TLS

Set `AGENT_TLS_CERT` + `AGENT_TLS_KEY` → agent listens HTTPS (min TLS 1.2). Use `https://` in `agentURL`.
