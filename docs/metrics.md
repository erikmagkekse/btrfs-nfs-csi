# Metrics

## Agent (34) - port 9090

| Metric | Type | Labels |
|---|---|---|
| `btrfs_nfs_csi_agent_http_requests_total` | Counter | `method`, `path`, `code` |
| `btrfs_nfs_csi_agent_http_request_duration_seconds` | Histogram | `method`, `path` |
| `btrfs_nfs_csi_agent_volumes` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_exports` | Gauge | `tenant` |
| `btrfs_nfs_csi_agent_volume_size_bytes` | Gauge | `tenant`, `volume` |
| `btrfs_nfs_csi_agent_volume_used_bytes` | Gauge | `tenant`, `volume` |
| `btrfs_nfs_csi_agent_device_read_bytes_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_ios_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_bytes_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_ios_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_ios_in_progress` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_io_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_io_weighted_time_seconds_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_size_bytes` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_allocated_bytes` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_present` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_read_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_write_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_flush_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_corruption_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_device_generation_errs_total` | Gauge | `device` |
| `btrfs_nfs_csi_agent_filesystem_size_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_used_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_unallocated_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_metadata_used_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_metadata_total_bytes` | Gauge | `path` |
| `btrfs_nfs_csi_agent_filesystem_data_ratio` | Gauge | `path` |
| `btrfs_nfs_csi_agent_tasks_total` | Counter | `type`, `status` |
| `btrfs_nfs_csi_agent_task_duration_seconds` | Histogram | `type` |
| `btrfs_nfs_csi_agent_tasks_running` | Gauge | `type` |
| `btrfs_nfs_csi_agent_tasks_queued` | Gauge | `type` |
| `btrfs_nfs_csi_agent_tasks_workers` | Gauge | - |

**Buckets (http_request_duration):** `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5]`

Device IO metrics are updated every 5s (configurable via `AGENT_DEVICE_IO_INTERVAL`). Device errors and filesystem allocation are updated every 1m (configurable via `AGENT_DEVICE_STATS_INTERVAL`). Missing devices (e.g. physically removed drives in a RAID setup) are skipped during IO polling.

## PromQL Examples (Agent)

```promql
# Volumes near quota (> 90%)
btrfs_nfs_csi_agent_volume_used_bytes / btrfs_nfs_csi_agent_volume_size_bytes > 0.9

# Missing device alert
btrfs_nfs_csi_agent_device_present == 0

# Device allocation > 90%
btrfs_nfs_csi_agent_device_allocated_bytes / btrfs_nfs_csi_agent_device_size_bytes > 0.9

# Task queue depth (tasks waiting for a worker slot)
sum(btrfs_nfs_csi_agent_tasks_queued)

# Worker pool utilization
sum(btrfs_nfs_csi_agent_tasks_running) / btrfs_nfs_csi_agent_tasks_workers
```

Integrations may expose additional metrics. Check the documentation for your integration (e.g. [Kubernetes CSI metrics](integrations/kubernetes/metrics.md)).
