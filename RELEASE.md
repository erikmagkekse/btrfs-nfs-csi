# Release v0.11.2

**Previous: v0.11.1** | **Date: 2026-05-12**

CLI saved-agent endpoints so the bearer token no longer has to live in shell history, plus a Go 1.26.3 toolchain bump that pulls in the GO-2026-4918 stdlib fix and refreshes the Kubernetes client family to v0.36.0. No breaking changes.

> **Successor:** Active development moves to a hard fork named **[ButterStore](https://github.com/butterstore)**, which reframes the project around what it has actually become: a general-purpose btrfs storage backend where the Kubernetes CSI driver is one of several integrations. Migration is a drop-in: tokens, REST API, CLI, Helm values, StorageClasses, PVCs, and VolumeSnapshots all keep working. A migration guide ships with the first ButterStore release. Once ButterStore ships, this repo gets archived. Published artifacts (container images, Helm charts) stay available.
>
> *The name plays on the colloquial pronunciation of btrfs as "butter-FS" plus its role as a storage backend.*

---

## Highlights

### Saved CLI agents

`btrfs-nfs-csi agents login <name> --url ...` verifies the token via `/v1/whoami`, saves the endpoint to `~/.btrfs-nfs-csi/config.json` (file `0600`, dir `0700`), and makes it active. Every subsequent CLI command falls back per field to the active entry, so plain `btrfs-nfs-csi volume list` works in a fresh shell without exporting `AGENT_URL`/`AGENT_TOKEN`.

```bash
echo "$TOKEN" | btrfs-nfs-csi agents login prod --url https://agent.example.com:8080
btrfs-nfs-csi agents ls
btrfs-nfs-csi agents use prod
btrfs-nfs-csi volume list
btrfs-nfs-csi agents verify --all
```

Subcommands: `login`, `logout [<name>]`, `ls` (`-o wide`/`-o json`), `use <name>`, `verify [<name>] [--all]`. The token reads from a no-echo prompt or a stdin pipe, never as a flag value that would land in shell history. Precedence is `flag > env > active entry`, so the existing env-based workflow keeps working for one-off shells and CI. Tenant, role, and token fingerprint are cached at login time so `agents ls` works offline. `verify` re-checks the token against the running agent and flags any cached field that drifted. Per-agent `--tls-skip-verify` is persisted for self-signed endpoints. Override the config path with `BTRFS_NFS_CSI_CONFIG_FILE`.

### Go 1.26.3 and GO-2026-4918

The toolchain moves from 1.25.0 to 1.26.3 and picks up the stdlib fix for `GO-2026-4918`.

### Kubernetes client family to v0.36.0

`k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, and `k8s.io/mount-utils` jump from `0.35.4` to `0.36.0`. Cluster-side API compatibility is unchanged, this is a routine library refresh that follows upstream's stable release cadence.

---

## Features

- `agents` subcommand for managing saved remote endpoints (#166). See Highlights.

## Security

- Bump Go to 1.26.3, fixes GO-2026-4918 (#168).

## Dependencies

- Bump `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, `k8s.io/mount-utils` from 0.35.4 to 0.36.0 (#168)
- Bump `google.golang.org/grpc` from 1.80.0 to 1.81.0 (#165)
- Bump `golang.org/x/term` from 0.42.0 to 0.43.0 (#167)
- Bump `golang.org/x/crypto` from 0.50.0 to 0.51.0 (#168)
- Refresh remaining direct and indirect dependencies, including `caarlos0/env`, `labstack/echo`, the swagger/go-openapi stack, and the gnostic/cbor toolchain (#168)

---

## Upgrade Guide

Drop-in from v0.11.1. Bump the image tag to `0.11.2`.

- **Kubernetes CSI users:** no action required. The new `k8s.io/*` client libraries stay backwards compatible with the same cluster versions, and there are no manifest, RBAC, or StorageClass changes.
- **CLI users:** existing `AGENT_URL`/`AGENT_TOKEN` workflows keep working unchanged. To switch to the new saved-agent flow, run `agents login <name> --url ...` once per endpoint and pipe the token in. The file under `~/.btrfs-nfs-csi/` is plain JSON with the bearer token, `0600` on the file and `0700` on the directory, treat it like an SSH private key.
- **CI / one-off shells:** keep using env vars or `--agent-url`/`--agent-token` flags. Flags and env still take precedence over the saved entry, so a single CI shell can override the active agent without touching the config file.
- **Operators rebuilding from source:** Go 1.26.3 is now the minimum toolchain, the `go.mod` directive is `go 1.26.3`.

---

## Deprecations

None.
