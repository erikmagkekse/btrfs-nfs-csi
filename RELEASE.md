# Release v0.11.1

**Previous: v0.11.0** | **Date: 2026-04-29**

Patch release. The CLI now respects `AGENT_CSI_IDENTITY` in its default list filter, so non-`cli` identities (Ansible, ad-hoc operator scripts, second CLI in a different role) see their own resources by default instead of always falling back to `created-by=cli`. Plus a CodeQL hardening pass on path handling, a zerolog patch bump, and documentation fixes.

No breaking changes. `AGENT_TENANTS`, Helm values, StorageClasses, PVCs, VolumeSnapshots, and the in-tree CSI driver work without modification.

> **`btrfs-nfs-csi` is being deprecated, ButterStore is the successor.** Active development is moving to a new project, **ButterStore**, which will be a hard fork of this repository. The fork is happening because the codebase has long outgrown the "Kubernetes CSI driver" framing, the agent is already a general-purpose btrfs storage backend with REST API, CLI, multi-tenancy, RBAC, tasks, and metrics, and the Kubernetes CSI driver is one of several integrations sitting on top of it. ButterStore reframes the project around that reality (NFS plus a planned FUSE backend plus btrfs send/receive replication) and opens room for Nomad, Proxmox, Docker, and other integrations alongside the existing CSI driver. The name leans into the long-running "butter-FS" colloquial pronunciation of btrfs.
>
> **What this means for you.** A few more `btrfs-nfs-csi` releases will ship until ButterStore is public. After that, this repo is archived, no further releases or fixes. All forward work moves to ButterStore. Repo and images stay online for archival only.
>
> **Migration.** ButterStore drops in over `btrfs-nfs-csi` v0.11.x. Agent state on disk, `AGENT_TENANTS`, the REST API, the CLI, Helm values, StorageClasses, PVCs, and VolumeSnapshots all keep working as-is. The migration is a container-image swap and a Helm chart rename, no data movement, no API rewrites. A migration guide ships with the first ButterStore release.
>
> **Nothing breaks today.** v0.11.1 ships as `btrfs-nfs-csi` everywhere. You do not need to do anything for this release.

---

## Highlights

### Default list filter follows the active identity

Before v0.11.1 the CLI hardcoded `created-by=cli` as its default filter on every `list` command. Set `AGENT_CSI_IDENTITY=ansible` and the CLI still hid your own resources unless you passed `--all`. The CLI now resolves its identity at startup (via `/v1/whoami`) and filters by whatever the agent sees on the wire.

```bash
export AGENT_CSI_IDENTITY=ansible
btrfs-nfs-csi volume list                # shows volumes created by ansible
btrfs-nfs-csi volume list --all          # show every volume in the tenant
```

Identical for the agent. The only thing that changes is what shows up by default in the CLI table.

---

## Bug Fixes

- **`AGENT_CSI_IDENTITY` honoured in default list filter** (#158). The CLI default filter was always `created-by=cli`, regardless of the configured identity. Lists now filter by the resolved identity from `/v1/whoami`, falling back to `cli` if no identity is set on the token. The `--all` / `-A` flag still bypasses the filter entirely.

## Improvements

- **CodeQL path-injection false positives silenced** (#157). Storage-layer file operations now route through a small `meta/store` boundary that re-validates paths under the configured base. No behaviour change at runtime, the agent already rejected traversal at the API layer, but CodeQL can no longer point at a long list of `os.ReadFile`/`os.WriteFile` call sites as suspect.

## Documentation

- **Quickstart curl typo fixed** (`docs/installation.md`). 
- **Task system architecture page brought up to date** (`docs/architecture.md`). Lists all five task types (`scrub`, `balance`, `quota-rescan`, `defragment`, `test`) and the filesystem-wide mutex.

## Dependencies

- Bump `github.com/rs/zerolog` from 1.35.0 to 1.35.1 (#145)

---

## Upgrade Guide

Drop-in upgrade from v0.11.0. Bump the image tag to `0.11.1`.

### Kubernetes CSI users

No action required. The CSI controller and node driver run with `AGENT_CSI_IDENTITY=k8s` and were never affected by the `created-by=cli` default filter (it only ran in the CLI binary).

### CLI users with a custom identity

If you set `AGENT_CSI_IDENTITY` to anything other than `cli` and worked around the old filter with `--all`, you can drop the flag, the default now matches your identity. The agent makes one extra `GET /v1/whoami` call when the CLI starts to resolve the identity.

---

## Deprecations

None.
