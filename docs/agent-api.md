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
| `INTERNAL_ERROR` | 500 | Server error |

## Pagination

I don't see why I should add this as a feature. List endpoints return all results without pagination. The CSI controller handles gRPC message size limits (4 MB default) via its own pagination (`starting_token`/`max_entries`) independently of the agent API. A single agent is not expected to host more than ~5k volumes, so returning everything keeps the agent and controller code simple.

## Volumes

### POST /v1/volumes

`name`: 1-64 chars `[a-zA-Z0-9_-]`. `nocow` + `compression` mutually exclusive. 409 returns existing volume.

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
  "mode": "0750"
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
  "clients": 0,
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

Slim list response. Use GET /v1/volumes/:name for full details.

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

### GET /v1/volumes/:name

Detail response with full client list (list endpoint returns client count only). 404 if not found.

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
  "clients": ["10.1.0.50"],
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:30:00Z",
  "last_attach_at": "2025-01-15T11:00:00Z"
}
```

### PATCH /v1/volumes/:name

All fields optional. `size_bytes` must be larger than current.

```json
{
  "size_bytes": 2147483648,
  "nocow": true,
  "compression": "lzo",
  "uid": 2000,
  "gid": 2000,
  "mode": "0755"
}
```

### DELETE /v1/volumes/:name

204 No Content. 404 if not found.

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
  "name": "snap-1"
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
  "created_at": "2025-01-15T12:00:00Z",
  "updated_at": "2025-01-15T12:00:00Z"
}
```

### GET /v1/snapshots

Slim list response. Use GET /v1/snapshots/:name for full details.

```json
{
  "snapshots": [
    {
      "name": "snap-1",
      "volume": "vol-1",
      "size_bytes": 1073741824,
      "created_at": "2025-01-15T12:00:00Z"
    }
  ],
  "total": 1
}
```

### GET /v1/snapshots/:name

Detail response. 404 if not found.

```json
{
  "name": "snap-1",
  "volume": "vol-1",
  "path": "/srv/csi/default/snapshots/snap-1",
  "size_bytes": 1073741824,
  "used_bytes": 16384,
  "exclusive_bytes": 0,
  "readonly": true,
  "created_at": "2025-01-15T12:00:00Z",
  "updated_at": "2025-01-15T12:00:00Z"
}
```

### GET /v1/volumes/:name/snapshots

Lists snapshots for a specific volume. Same response format as GET /v1/snapshots.

### DELETE /v1/snapshots/:name

204 No Content. 404 if not found.

## Clones

### POST /v1/clones

409 returns existing clone.

```json
// Request
{
  "snapshot": "snap-1",
  "name": "clone-1"
}

// Response 201
{
  "name": "clone-1",
  "source_snapshot": "snap-1",
  "path": "/srv/csi/default/clone-1",
  "created_at": "2025-01-15T12:30:00Z"
}
```

## Stats

### GET /v1/stats

```json
{
  "total_bytes": 1099511627776,
  "used_bytes": 10737418240,
  "free_bytes": 1088774209536
}
```

## Dashboard

### GET /v1/dashboard

HTML dashboard (requires auth, use Basic in browser).

## Unauthenticated

### GET /healthz

```json
{
  "status": "ok",
  "version": "0.9.6",
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
