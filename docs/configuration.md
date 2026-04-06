# Configuration

## Agent Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENT_BASE_PATH` | `./storage` | btrfs mount point (quickstart sets `/export/data`) |
| `AGENT_TENANTS` | **required** | `name:token,name:token` |
| `AGENT_LISTEN_ADDR` | `:8080` | HTTP listen address |
| `AGENT_METRICS_ADDR` | `127.0.0.1:9090` | Metrics server address |
| `AGENT_TLS_CERT` | - | TLS certificate path |
| `AGENT_TLS_KEY` | - | TLS key path |
| `AGENT_FEATURE_QUOTA_ENABLED` | `true` | btrfs quota tracking |
| `AGENT_FEATURE_QUOTA_UPDATE_INTERVAL` | `1m` | Usage update interval |
| `AGENT_NFS_EXPORTER` | `kernel` | NFS exporter type |
| `AGENT_EXPORTFS_BIN` | `exportfs` | exportfs binary path |
| `AGENT_KERNEL_EXPORT_OPTIONS` | `rw,nohide,crossmnt,no_root_squash,no_subtree_check` | NFS export options (fsid is always appended automatically) |
| `AGENT_BTRFS_BIN` | `btrfs` | btrfs binary path |
| `AGENT_NFS_RECONCILE_INTERVAL` | `60s` | Export reconciliation (`0` = off) |
| `AGENT_DEVICE_IO_INTERVAL` | `5s` | Device IO stats update interval |
| `AGENT_DEVICE_STATS_INTERVAL` | `1m` | btrfs device errors + filesystem usage update interval |
| `AGENT_DEFAULT_DIR_MODE` | `0700` | Default mode for volume/snapshot/clone directories |
| `AGENT_DEFAULT_DATA_MODE` | `2770` | Default mode for data subvolumes (setgid + group rwx) |
| `AGENT_TASK_CLEANUP_INTERVAL` | `24h` | Remove completed/failed tasks after this duration |
| `AGENT_TASK_MAX_CONCURRENT` | `2` | Max concurrent tasks (`0` = unlimited) |
| `AGENT_TASK_DEFAULT_TIMEOUT` | `6h` | Default timeout for tasks (e.g. test). `0` = no timeout |
| `AGENT_TASK_SCRUB_TIMEOUT` | `24h` | Timeout for btrfs scrub tasks. `0` = no timeout |
| `AGENT_TASK_POLL_INTERVAL` | `5s` | Progress update interval for background tasks |
| `AGENT_IMMUTABLE_LABELS` | - | Comma-separated label keys that cannot be changed after creation |
| `AGENT_DEFAULT_PAGE_LIMIT` | `0` | Default page size for list API responses (0 = pagination disabled) |
| `AGENT_API_PAGINATION_SNAPSHOT_TTL` | `30s` | TTL for cursor-based pagination snapshots |
| `AGENT_API_PAGINATION_MAX_SNAPSHOTS` | `100` | Max concurrent pagination snapshots |
| `AGENT_API_SWAGGER_ENABLED` | `false` | Enable `GET /swagger.json` endpoint |
| `LOG_LEVEL` | `info` | `trace`, `debug`, `info`, `warn`, `error` |

## API Client Environment Variables

Shared by CLI and all integrations (any `v1.Client` user).

| Variable | Default | Description |
|---|---|---|
| `AGENT_CSI_IDENTITY` | `cli` (CLI), `k8s` (controller) | Caller identity for `created-by` label, injected automatically on every create |
| `AGENT_HTTP_CLIENT_TIMEOUT` | `30s` | API request timeout (Go duration) |
| `AGENT_HTTP_CLIENT_TLS_SKIP_VERIFY` | `false` | Skip TLS certificate verification |
| `AGENT_HTTP_CLIENT_PAGE_LIMIT` | `0` | Items per page for auto-pagination (0 = pagination disabled) |
| `AGENT_HTTP_CLIENT_PREFETCH` | `8` | Max pages to prefetch concurrently (`0` = sequential) |
| `AGENT_HTTP_CLIENT_PREFETCH_MB` | `4` | Prefetch byte budget in MB (`0` = unlimited) |

## CLI Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENT_URL` | - | Agent API URL |
| `AGENT_TOKEN` | - | Tenant token |
| `BTRFS_NFS_CSI_FORCE` | `false` | Skip delete protection when `true` |

Also configurable via `--agent-url` and `--agent-token` flags.

## TLS

Set `AGENT_TLS_CERT` + `AGENT_TLS_KEY` and the agent listens on HTTPS (min TLS 1.2). Use `https://` in `AGENT_URL` or `agentURL`.

For self-signed certificates, set `AGENT_HTTP_CLIENT_TLS_SKIP_VERIFY=true`.
