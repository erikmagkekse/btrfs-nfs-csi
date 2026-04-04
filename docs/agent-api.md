# Agent API

## Authentication

`Authorization: Bearer <token>` or `Authorization: Basic <base64(user:token)>` (password = token, username ignored).

Token resolves to tenant via `AGENT_TENANTS`. All `/v1/*` endpoints require auth.

## Error Format

```json
{
  "error": "message",
  "code": "ERROR_CODE"
}
```

| Code | Status | Description |
|---|---|---|
| `BAD_REQUEST` | 400 | Malformed body |
| `INVALID` | 400 | Invalid parameter |
| `UNAUTHORIZED` | 401 | Bad/missing token |
| `NOT_FOUND` | 404 | Resource missing |
| `ALREADY_EXISTS` | 409 | Conflict (returns existing record) |
| `BUSY` | 423 | Resource locked (e.g. active NFS exports, scrub running) |
| `METADATA_ERROR` | 500 | Volume/snapshot metadata corrupt or unreadable |
| `INTERNAL_ERROR` | 500 | Server error |

## Labels

Volumes, snapshots, clones, and tasks support user-defined labels (`map[string]string`). Labels are optional on create requests and returned in detail responses.

| Constraint | Rule |
|---|---|
| Key format | `^[a-z0-9][a-z0-9._-]{0,62}$` |
| Value format | `^[a-zA-Z0-9._-]{0,128}$` (empty allowed) |
| Max labels | 12 per resource |
| Max user labels | 4 via PVC annotation or CLI `--label` |

Filter with `?label=key=value` (exact match, repeatable, AND logic) or `?label=key` (key exists, any value).

## Pagination

All list endpoints support cursor-based pagination via query parameters:

| Parameter | Description |
|---|---|
| `after` | Cursor: return items after this name (exclusive) |
| `limit` | Max items per page (0 or omitted = all) |

Responses include:

| Field | Description |
|---|---|
| `total` | Total number of resources (approximate when volume-filtered) |
| `next` | Cursor for the next page (empty = last page) |

Example: `GET /v1/volumes?limit=10` returns the first 10 volumes. Use the `next` value as `after` for the next page: `GET /v1/volumes?limit=10&after=<next>`.

## Volumes

### POST /v1/volumes

`name`: 1-128 chars `[a-zA-Z0-9_-]`. `nocow` + `compression` mutually exclusive. 409 returns existing volume.

```json
// Request
{
  "name": "vol-1",
  "size_bytes": 1073741824,
  "nocow": false,
  "compression": "zstd",
  "quota_bytes": 1073741824,
  "uid": 1000,
  "gid": 1000,
  "mode": "0750",
  "labels": {"env": "prod", "team": "backend"}
}

// Response 201
{
  "name": "vol-1",
  "path": "/srv/csi/default/vol-1",
  "size_bytes": 1073741824,
  "nocow": false,
  "compression": "zstd",
  "quota_bytes": 1073741824,
  "used_bytes": 0,
  "uid": 1000,
  "gid": 1000,
  "mode": "0750",
  "labels": {"env": "prod", "team": "backend"},
  "clients": [],
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:30:00Z",
  "last_attach_at": "2025-01-15T11:00:00Z"
}
```

```bash
curl -X POST http://10.0.0.5:8080/v1/volumes \
  -H "Authorization: Bearer changeme" \
  -H "Content-Type: application/json" \
  -d '{"name":"vol-1","size_bytes":1073741824}'
```

### GET /v1/volumes

Returns a summary list. Supports pagination (`?after=&limit=`), label filtering (`?label=key=value` or `?label=key`), and `?detail=true` for full details.

```json
{
  "volumes": [
    {
      "name": "vol-1",
      "size_bytes": 1073741824,
      "used_bytes": 16384,
      "clients": 1,
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 1
}
```

With `?detail=true`, each volume includes the full detail fields (same as `GET /v1/volumes/:name`): `path`, `quota_bytes`, `compression`, `nocow`, `uid`, `gid`, `mode`, `labels`, `clients` (as array), `updated_at`, `last_attach_at`.

### GET /v1/volumes/:name

```json
{
  "name": "vol-1",
  "path": "/srv/csi/default/vol-1",
  "size_bytes": 1073741824,
  "nocow": false,
  "compression": "zstd",
  "quota_bytes": 1073741824,
  "used_bytes": 16384,
  "uid": 1000,
  "gid": 1000,
  "mode": "0750",
  "labels": {"env": "prod", "team": "backend"},
  "clients": ["10.1.0.50"],
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:30:00Z",
  "last_attach_at": "2025-01-15T11:00:00Z"
}
```

### PATCH /v1/volumes/:name

All fields optional. `size_bytes` must be larger than current. `labels` replaces all labels when present.

```json
{
  "size_bytes": 2147483648,
  "nocow": true,
  "compression": "lzo",
  "uid": 2000,
  "gid": 2000,
  "mode": "0755",
  "labels": {"env": "staging"}
}
```

### DELETE /v1/volumes/:name

204 No Content. 404 if not found. 423 if the volume still has active NFS exports, unexport all clients first.

## NFS Exports

### POST /v1/volumes/:name/export

```json
{
  "client": "10.1.0.50"
}
```

204 No Content. Reconciler retries on failure.

### DELETE /v1/volumes/:name/export

```json
{
  "client": "10.1.0.50"
}
```

204 No Content.

### GET /v1/exports

```json
{
  "exports": [
    {
      "path": "/srv/csi/default/vol-1",
      "client": "10.1.0.50"
    }
  ]
}
```

## Snapshots

### POST /v1/snapshots

```json
// Request
{
  "volume": "vol-1",
  "name": "snap-1",
  "labels": {"env": "prod"}
}

// Response 201
{
  "name": "snap-1",
  "volume": "vol-1",
  "path": "/srv/csi/default/snapshots/snap-1",
  "size_bytes": 1073741824,
  "used_bytes": 16384,
  "exclusive_bytes": 0,
  "readonly": true,
  "labels": {"env": "prod"},
  "created_at": "2025-01-15T12:00:00Z",
  "updated_at": "2025-01-15T12:00:00Z"
}
```

### GET /v1/snapshots

Returns a summary list of all snapshots. Supports pagination (`?after=&limit=`), label filtering (`?label=key=value` or `?label=key`), and `?detail=true` for full details.

```json
{
  "snapshots": [
    {
      "name": "snap-1",
      "volume": "vol-1",
      "size_bytes": 1073741824,
      "used_bytes": 16384,
      "created_at": "2025-01-15T12:00:00Z"
    }
  ],
  "total": 1
}
```

With `?detail=true`, each snapshot includes the full detail fields (same as `GET /v1/snapshots/:name`): `path`, `exclusive_bytes`, `readonly`, `labels`, `updated_at`.

### GET /v1/volumes/:name/snapshots

Returns a summary list of snapshots for a specific volume. Same response format as `GET /v1/snapshots`. Supports pagination, label filtering, and `?detail=true`.

### GET /v1/snapshots/:name

```json
{
  "name": "snap-1",
  "volume": "vol-1",
  "path": "/srv/csi/default/snapshots/snap-1",
  "size_bytes": 1073741824,
  "used_bytes": 16384,
  "exclusive_bytes": 0,
  "readonly": true,
  "labels": {"env": "prod"},
  "created_at": "2025-01-15T12:00:00Z",
  "updated_at": "2025-01-15T12:00:00Z"
}
```

### DELETE /v1/snapshots/:name

204 No Content. 404 if not found.

## Volume Clone (PVC-to-PVC)

### POST /v1/volumes/clone

Direct volume-to-volume clone via a single atomic btrfs snapshot. No intermediate snapshot needed. 409 returns existing volume. If `labels` is omitted, source volume labels are inherited.

```json
// Request
{
  "source": "my-volume",
  "name": "my-clone",
  "labels": {"env": "staging"}
}

// Response 201
{
  "name": "my-clone",
  "path": "/srv/csi/default/my-clone",
  "size_bytes": 10737418240,
  "nocow": false,
  "compression": "zstd",
  "quota_bytes": 10737418240,
  "used_bytes": 0,
  "uid": 0,
  "gid": 0,
  "mode": "2770",
  "labels": {"env": "staging"},
  "clients": [],
  "created_at": "2025-01-15T12:30:00Z",
  "updated_at": "2025-01-15T12:30:00Z"
}
```

## Clones (from Snapshot)

### POST /v1/clones

Clone from a read-only snapshot. 409 returns existing clone. If `labels` is omitted, source snapshot labels are inherited.

```json
// Request
{
  "snapshot": "snap-1",
  "name": "clone-1",
  "labels": {"env": "dev"}
}

// Response 201
{
  "name": "clone-1",
  "path": "/srv/csi/default/clone-1",
  "size_bytes": 0,
  "nocow": false,
  "compression": "",
  "quota_bytes": 0,
  "used_bytes": 0,
  "uid": 0,
  "gid": 0,
  "mode": "",
  "labels": {"env": "dev"},
  "clients": [],
  "created_at": "2025-01-15T12:30:00Z",
  "updated_at": "2025-01-15T12:30:00Z"
}
```

## Stats

### GET /v1/stats

Filesystem space usage, per-device IO counters (from sysfs), per-device btrfs error counters, and btrfs filesystem allocation. Devices are discovered dynamically, hot-added devices appear automatically. Missing devices (e.g. physically removed drives in a RAID setup) are included with `"missing": true`.

```json
{
  "statfs": {
    "total_bytes": 1099511627776,
    "used_bytes": 10737418240,
    "free_bytes": 1088774209536
  },
  "btrfs": {
    "total_bytes": 107374182400,
    "used_bytes": 42949672960,
    "free_bytes": 64424509440,
    "unallocated_bytes": 53687091200,
    "metadata_used_bytes": 2147483648,
    "metadata_total_bytes": 5368709120,
    "data_ratio": 1.0,
    "devices": [
      {
        "devid": "1",
        "device": "/dev/sdb",
        "missing": false,
        "size_bytes": 10737418240,
        "allocated_bytes": 1354235904,
        "io": {
          "read_bytes_total": 126418944,
          "read_ios_total": 12345,
          "read_time_ms_total": 5678,
          "write_bytes_total": 1011357696,
          "write_ios_total": 54321,
          "write_time_ms_total": 8765,
          "ios_in_progress": 0,
          "io_time_ms_total": 45678,
          "weighted_io_time_ms_total": 56789
        },
        "errors": {
          "read_errs": 0,
          "write_errs": 0,
          "flush_errs": 0,
          "corruption_errs": 0,
          "generation_errs": 0
        }
      }
    ]
  }
}
```

## Tasks

Background task system for long-running operations (scrub, future: transfers). Tasks are persisted to disk and survive agent restarts. Running tasks that were interrupted by an agent restart are marked as `failed`.

### POST /v1/tasks/:type

Starts a background task. `type` is `scrub` or `test`. Returns 423 if a scrub is already running.

```json
// Request (all fields optional)
{
  "timeout": "1h",
  "opts": {"sleep": "10s"},
  "labels": {"created-by": "cron"}
}
```

`timeout` overrides the server default (scrub: 24h, test: 6h). `opts` is task-type-specific: scrub has no opts, test accepts `{"sleep": "10s"}`. `labels` are optional metadata (same rules as volume labels).

```json
// Response 202
{
  "task_id": "bba30993dee31318f016f5350718cffa",
  "status": "pending"
}
```

```bash
# Start scrub
curl -X POST http://10.0.0.5:8080/v1/tasks/scrub \
  -H "Authorization: Bearer changeme"

# Start scrub with 30m timeout
curl -X POST http://10.0.0.5:8080/v1/tasks/scrub \
  -H "Authorization: Bearer changeme" \
  -H "Content-Type: application/json" \
  -d '{"timeout": "30m"}'

# Start test task with sleep
curl -X POST http://10.0.0.5:8080/v1/tasks/test \
  -H "Authorization: Bearer changeme" \
  -H "Content-Type: application/json" \
  -d '{"opts": {"sleep": "10s"}, "timeout": "1m"}'
```

### GET /v1/tasks

List all tasks. Supports pagination (`?after=&limit=`), `?type=` filter, label filtering (`?label=key=value` or `?label=key`), and `?detail=true` for full details including `result` and `labels`.

```json
{
  "tasks": [
    {
      "id": "bba30993dee31318f016f5350718cffa",
      "type": "scrub",
      "status": "completed",
      "progress": 100,
      "timeout": "24h0m0s",
      "created_at": "2025-01-15T02:00:00Z",
      "started_at": "2025-01-15T02:00:00Z",
      "completed_at": "2025-01-15T02:00:05Z"
    }
  ],
  "total": 1
}
```

With `?detail=true`, each task includes `result`, `opts`, and `labels` fields.

### GET /v1/tasks/:id

Returns a single task with full details including `result`, `opts`, and `labels`. 404 if not found.

```json
{
  "id": "bba30993dee31318f016f5350718cffa",
  "type": "scrub",
  "status": "completed",
  "progress": 100,
  "timeout": "24h0m0s",
  "result": {
    "data_bytes_scrubbed": 3145728000,
    "tree_bytes_scrubbed": 6979584,
    "read_errors": 0,
    "csum_errors": 0,
    "verify_errors": 0,
    "super_errors": 0,
    "uncorrectable_errors": 0,
    "corrected_errors": 0,
    "running": false
  },
  "created_at": "2025-01-15T02:00:00Z",
  "started_at": "2025-01-15T02:00:00Z",
  "completed_at": "2025-01-15T02:00:05Z"
}
```

### DELETE /v1/tasks/:id

Cancels a running task. 204 No Content. 404 if not found. Cancelling a finished task is a no-op.

Task statuses: `pending`, `running`, `completed`, `failed`, `cancelled`.

## Dashboard

### GET /v1/dashboard

HTML dashboard (requires auth, use Basic in browser).

## Unauthenticated

### GET /healthz

`status` is `"ok"` or `"degraded"` (missing device or btrfs device errors).

```json
{
  "status": "ok",
  "version": "0.9.9",
  "commit": "abc123",
  "uptime_seconds": 3600,
  "features": {
    "nfs_exporter": "kernel",
    "quota": "enabled",
    "nfs_reconcile": "10m0s"
  }
}
```

### GET /metrics

Prometheus text format.
