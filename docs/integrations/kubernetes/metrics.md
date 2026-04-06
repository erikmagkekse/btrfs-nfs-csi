# Kubernetes CSI Metrics

See also: [Agent metrics](../../metrics.md) for agent Prometheus metrics and PromQL examples.

## Controller (5) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_controller_grpc_requests_total` | Counter | `method`, `code` |
| `btrfs_nfs_csi_controller_grpc_request_duration_seconds` | Histogram | `method` |
| `btrfs_nfs_csi_controller_agent_ops_total` | Counter | `operation`, `status`, `storage_class` |
| `btrfs_nfs_csi_controller_agent_duration_seconds` | Histogram | `operation`, `storage_class` |
| `btrfs_nfs_csi_controller_k8s_ops_total` | Counter | `status` |

**Operations and their status values:**

| Operation | Status |
|---|---|
| `create_volume` | `success`, `error`, `conflict` |
| `delete_volume` | `success`, `error`, `not_found` |
| `create_snapshot` | `success`, `error`, `conflict` |
| `delete_snapshot` | `success`, `error`, `not_found` |
| `create_clone` | `success`, `error`, `conflict` |
| `clone_volume` | `success`, `error`, `conflict` |
| `export` | `success`, `error` |
| `unexport` | `success`, `error`, `not_found` |
| `update_volume` | `success`, `error` |
| `list_volumes` | `success`, `error` |
| `list_snapshots` | `success`, `error` |
| `health_check` | `healthy`, `degraded`, `error`, `version_mismatch` |

**Buckets (agent_duration):** `[0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`

## Node (7) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_node_grpc_requests_total` | Counter | `method`, `code` |
| `btrfs_nfs_csi_node_grpc_request_duration_seconds` | Histogram | `method` |
| `btrfs_nfs_csi_node_mount_ops_total` | Counter | `operation`, `status` |
| `btrfs_nfs_csi_node_mount_duration_seconds` | Histogram | `operation` |
| `btrfs_nfs_csi_node_volume_stats_ops_total` | Counter | `status` |
| `btrfs_nfs_csi_node_health_checks_total` | Counter | `result` |
| `btrfs_nfs_csi_node_health_check_duration_seconds` | Histogram | - |

**Mount operations:** `nfs_mount`, `bind_mount`, `umount`, `force_umount`, `remount_ro`, `health_remount`

**Health check results:** `healthy`, `stale`, `remounted`, `remount_failed`, `error`

**Buckets (grpc_request_duration):** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`

**Buckets (mount_duration):** `[0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120]`

## PromQL Examples

```promql
# Agent error rate
sum(rate(btrfs_nfs_csi_controller_agent_ops_total{status="error"}[5m]))

# NFS mount failures
rate(btrfs_nfs_csi_node_mount_ops_total{operation="nfs_mount",status="error"}[5m])

# Force unmounts (stuck mounts)
increase(btrfs_nfs_csi_node_mount_ops_total{operation="force_umount"}[1h])

# Agent health check errors
rate(btrfs_nfs_csi_controller_agent_ops_total{operation="health_check",status="error"}[5m])

# Stale NFS mounts detected
increase(btrfs_nfs_csi_node_health_checks_total{result="stale"}[1h])

# Auto-healed mounts
increase(btrfs_nfs_csi_node_health_checks_total{result="remounted"}[1h])

# Failed remounts (needs attention)
increase(btrfs_nfs_csi_node_health_checks_total{result="remount_failed"}[1h]) > 0

# P99 mount latency
histogram_quantile(0.99, rate(btrfs_nfs_csi_node_mount_duration_seconds_bucket{operation="nfs_mount"}[5m]))
```
