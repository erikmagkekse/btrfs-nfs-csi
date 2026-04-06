# btrfs-nfs-csi

[![Build](https://github.com/erikmagkekse/btrfs-nfs-csi/actions/workflows/release.yml/badge.svg)](https://github.com/erikmagkekse/btrfs-nfs-csi/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/erikmagkekse/btrfs-nfs-csi)](https://goreportcard.com/report/github.com/erikmagkekse/btrfs-nfs-csi)
[![License](https://img.shields.io/github/license/erikmagkekse/btrfs-nfs-csi)](LICENSE)

**Turn any Linux box with a btrfs disk into a very capable storage backend.**
Instant snapshots, writable clones, per-volume quotas, compression, NoCOW (no copy-on-write) for databases, and automatic NFS exports. All from a single Go binary and a REST API. 

> **Pre-1.0.** Stable and used, but minor breaking changes may occur before v1.0. Feedback welcome.

---

## Why btrfs-nfs-csi?

Most storage solutions are built for the data center. Ceph, Longhorn, and OpenEBS bring clustering overhead that doesn't make sense when you have one server (or two for HA) and a btrfs filesystem.

- **Minimal footprint.** Single Go binary, multiple integrations, runs on minimal hardware.
- **Zero infrastructure.** No etcd, no database, no distributed consensus.
- **Leverages btrfs.** Subvolumes become volumes, snapshots stay snapshots, qgroups enforce quotas.
- **Data integrity.** btrfs checksums every block, scrub detects silent corruption.
- **NFS just works.** ReadWriteMany out of the box, not a special case.
- **Multi-tenant.** One agent serves multiple teams or clusters, token-isolated.
- **Homelab HA.** Optional active/passive failover with DRBD + Pacemaker.

btrfs-nfs-csi is not a distributed storage system. If you need data replication across many nodes, look at Longhorn or Ceph. If you have one server with good disks and want storage that stays out of your way, this is it.

**Know your filesystem.** btrfs is powerful but has trade-offs. RAID 5/6 is not production-ready. Quotas (qgroups) add overhead on write-heavy workloads. CoW causes fragmentation over time (use NoCOW for databases). Regular scrubs are recommended to catch silent corruption early. None of these are deal-breakers, but you should be aware of them.

### Your Classic Kubernetes Storage Options

| | btrfs-nfs-csi | Longhorn | democratic-csi | csi-driver-nfs | local-dir |
|---|---|---|---|---|---|
| **Min. nodes** | 1 (2+ with DRBD) | 1 (3 for HA) | 1 | 1 (2+ with DRBD) | 1 |
| **ReadWriteOnce** | Yes | Yes | Yes | Yes | Yes |
| **ReadWriteMany** | Native (NFS) | NFSv4 (userspace) | NFS | Native (NFS) | -- |
| **Snapshots** | btrfs CoW (instant) | Incremental CoW | ZFS | Copy-based | -- |
| **Clones** | Zero-copy | Full copy | ZFS clone | Copy-based | -- |
| **Compression** | zstd/lzo/zlib per-volume | -- | ZFS (dataset) | -- | -- |
| **Checksums** | btrfs built-in | Optional (snapshots) | ZFS built-in | -- | -- |
| **NoCOW (DB mode)** | Per-volume | -- | -- | -- | -- |
| **Online expand** | Yes (+5Gi relative) | Yes | Yes | -- | -- |
| **Quotas** | btrfs qgroups | Scheduling | ZFS | -- | -- |
| **Multi-tenant** | Built-in | -- | -- | -- | -- |
| **Overhead** | <128 MB (1k volumes) | 500+ MB | ~50 MB | ~50 MB | ~10 MB |
| **Setup** | Single container + Helm chart | Helm | Helm | Helm | Helm |
| **Integrations** | REST API, CLI, Kubernetes, more planned | Kubernetes | Kubernetes | Kubernetes | Kubernetes |
| **Best for** | Homelab, single-server, small teams | Multi-node HA | ZFS users | Existing NFS server | Local disk |

> This comparison represents my personal point of view. No offense intended to any of these great projects. Improvements are welcome.

---

## See It in Action

### Volumes

Create, expand, compress, and label volumes with per-volume quotas and NoCOW for databases.

![Volumes](docs/assets/vhs/volumes.gif)

### Snapshots & Clones

Instant btrfs snapshots. Writable clones from snapshots or volumes, zero-copy and zero-wait.

![Snapshots & Clones](docs/assets/vhs/snapshots.gif)

### NFS Exports

Add and remove NFS exports per volume, per client, directly from the CLI.

![NFS Exports](docs/assets/vhs/exports.gif)

### Stats & Health

Per-device I/O stats, error tracking, and filesystem scrubs, all from the CLI.

![Stats & Health](docs/assets/vhs/stats.gif)

---

## Quick Start

### 1. Install the agent

![Install](docs/assets/vhs/install.gif)

One command on any Linux host with a btrfs filesystem (Debian, RHEL, Arch, SUSE):

```bash
curl -fsSL https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh \
  | sudo -E bash
```

The script installs Podman, NFS, btrfs-progs, enables quotas, and starts the agent as a Quadlet container. Save the tenant token printed at the end. For advanced setups see the [Installation docs](docs/installation.md).

To auto-format a block device as btrfs:

```bash
AGENT_BLOCK_DISK=/dev/sdb curl -fsSL \
  https://raw.githubusercontent.com/erikmagkekse/btrfs-nfs-csi/main/scripts/quickstart-agent.sh \
  | sudo -E bash
```

<details>
<summary>Environment variables</summary>

| Variable | Default | Description |
|---|---|---|
| `AGENT_BASE_PATH` | `/export/data` | btrfs mount point |
| `AGENT_TENANTS` | `default:<random>` | tenant:token pairs |
| `AGENT_LISTEN_ADDR` | `:8080` | Listen address |
| `AGENT_BLOCK_DISK` | | Optional block device to auto-format as btrfs |
| `VERSION` | `0.10.0` | Image tag |

</details>

### 2. Use the CLI

```bash
export AGENT_URL=http://10.0.0.5:8080
export AGENT_TOKEN=your-tenant-token    # from step 1
# export AGENT_CSI_IDENTITY=cli         # optional, default: cli
```

```bash
btrfs-nfs-csi volume create my-app 10Gi
btrfs-nfs-csi volume list
btrfs-nfs-csi snapshot create my-app before-deploy
btrfs-nfs-csi stats
```

That's it. The agent manages btrfs subvolumes, NFS exports, and quotas. The CLI talks to the agent via REST API. Everything else (container orchestrator integrations, automation, custom tooling) builds on top.

---

## Integrations

The agent exposes a REST API. Any system that can make HTTP calls can manage volumes, snapshots, and exports. The CLI and all integrations use the same API.

| Integration | Status | Description |
|---|---|---|
| [**Kubernetes (CSI Driver)**](docs/integrations/kubernetes/) | Beta | This is where it all started. PVCs, VolumeSnapshots, ReadWriteMany via NFS. |
| **Nomad** | Idea | CSI plugin for HashiCorp Nomad. |
| **Docker** | Idea | `docker volume create` support. |
| **Proxmox** | Idea | Storage plugin for Proxmox VE. |

### API Example

The Go client makes it easy to build your own integrations:

```go
import (
  "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/client"
  "github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
)

c, _ := client.NewClient("http://10.0.0.5:8080", "your-token", "my-app")

vol, _ := c.CreateVolume(ctx, models.VolumeCreateRequest{
  Name:        "my-volume",
  SizeBytes:   10 * 1024 * 1024 * 1024,
  Compression: "zstd",
  Labels:      map[string]string{"env": "prod"},
})

c.CreateVolumeExport(ctx, vol.Name, models.ExportCreateRequest{
  Client: "10.0.1.1",
})
```

Enable `AGENT_API_SWAGGER_ENABLED=true` and the agent serves the full spec at `/swagger.json`. Want to build an integration? We'd love a PR.

---

## Features

### Storage
- **Instant snapshots & writable clones.** btrfs copy-on-write, zero-copy.
- **Online volume expansion.** Absolute or relative (`+5Gi`).
- **Per-volume quotas.** Enforced at the filesystem level via btrfs qgroups.
- **Compression.** `zstd`, `lzo`, `zlib` with levels, configurable per volume.
- **NoCOW mode.** `chattr +C` for databases (PostgreSQL, MySQL, etcd).
- **Multi-device.** RAID 0/1/10 with per-device I/O stats and error tracking.

### Networking
- **Automatic NFS exports.** Managed per volume, per client.
- **ReadWriteMany.** The default access mode, not a special case.
- **NFS health checker.** Detects stale mounts, auto-heals without restarts.
- **Dedicated storage network.** Select NIC by name or subnet CIDR.

### Operations
- **Labels.** On volumes, snapshots, clones, exports, and tasks.
- **Multi-tenant.** Token-isolated tenants, one agent serves many consumers.
- **Background tasks.** Scrub, progress tracking, configurable timeouts.
- **Prometheus metrics.** On all components.
- **TLS & Swagger.** API with OpenAPI spec.
- **HA.** DRBD + Pacemaker active/passive failover.

### CLI
- **Watch mode** (`-w`). Auto-refreshing output for any list/get command.
- **Column filter** (`-c name,size,used`). Show only what you need.
- **Label filter** (`-l env=prod`). Filter resources by label.
- **Output formats.** Table, wide, JSON.
- **Relative resize.** `expand my-vol +5Gi`.
- **Identity switching.** Switch between identities via `AGENT_CSI_IDENTITY` to view or manage resources created by other integrations. Names stay unique across identities.

---

```bash
btrfs-nfs-csi v l                           # list volumes
btrfs-nfs-csi v l -o wide -w               # all columns, live refresh
btrfs-nfs-csi v c my-app 10Gi -l env=prod  # create with quota + label
btrfs-nfs-csi v l -l env=prod              # filter by label
btrfs-nfs-csi v clone my-app staging       # instant CoW clone
btrfs-nfs-csi s c my-app pre-deploy        # snapshot
btrfs-nfs-csi s clone pre-deploy rollback
btrfs-nfs-csi e a my-app 10.0.1.1          # NFS export
btrfs-nfs-csi stats -w                     # filesystem stats, live
```

Full CLI reference: [docs/operations.md](docs/operations.md#cli)

---

## Roadmap

### Planned for v1.0

- [ ] Stable API with no more breaking changes
- [ ] CSI sanity test suite
- [ ] End-to-end test suite
- [ ] Grafana dashboards and Prometheus alerting rules

### Under Consideration

- [ ] **squota.** btrfs simple quotas (faster, less overhead than qgroups).
- [ ] **VolumeGroupSnapshot.** Consistent multi-volume snapshots with fsfreeze via API and CLI, including Kubernetes CRD support.
- [ ] **FUSE mount backend.** Mount volumes via WebSocket or REST-FUSE through agent API and CLI, no kernel NFS required.
- [ ] **mTLS.** Mutual TLS authentication between agent, CLI, and integrations.
- [ ] **Multi-agent manager.** Central control plane for managing multiple agents across hosts.
- [ ] **btrfs send/receive.** Stream snapshots between agents via CLI and API.
- [ ] **Replication.** Scheduled, recurring send/receive to a second agent via task system.
- [ ] **Separate CLI binary.** Maybe split the CLI from the agent into its own lightweight binary.

Have an idea or want to build an integration? [Open an issue](https://github.com/erikmagkekse/btrfs-nfs-csi/issues) or submit a PR.

---

## Documentation

| Document | Description |
|---|---|
| [Installation](docs/installation.md) | Agent setup, container build |
| [Configuration](docs/configuration.md) | Environment variables, parameters, TLS |
| [Architecture](docs/architecture.md) | Volume lifecycle, ID formats, directory structure, HA |
| [Operations](docs/operations.md) | Snapshots, clones, expansion, compression, NoCOW, quotas, NFS exports |
| [Metrics](docs/metrics.md) | Prometheus metrics, PromQL examples |
| [Integrations](docs/integrations/) | Kubernetes CSI driver (more coming) |
| [Release](docs/release.md) | Release process, versioning, CI pipeline |

## Handbook

> Work in progress.

The handbook will cover real-world recipes, best practices, and operational guides. Stay tuned.

---

## Building

```bash
go build -ldflags "-X main.version=$(cat VERSION) -X main.commit=$(git rev-parse --short HEAD)" \
  -o btrfs-nfs-csi ./cmd/btrfs-nfs-csi
```

---

## Contributing

Contributions are herzlich willkommen! Whether it's a bug fix, a new integration, or improved docs, feel free to [open an issue](https://github.com/erikmagkekse/btrfs-nfs-csi/issues) or submit a PR.

See [docs/release.md](docs/release.md) for the release process and CI pipeline.
