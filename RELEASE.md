# Release v0.11.0

**Previous: v0.10.0** | **Date: 2026-04-25**

Security and operations release. Adds three-level RBAC (roles, identity, ownership), bcrypt-hashable agent tokens, denial telemetry, token introspection, and four new btrfs maintenance task types (balance, defragment, quota-rescan, plus expanded scrub flags).

No breaking changes for existing CSI users. Helm values, StorageClasses, PVCs, VolumeSnapshots, and the in-tree CSI driver work without modification. One PATCH-path behavior change in `Behavior Changes` does not affect the CSI driver or CLI.

> **Heads up:** a new project named **ButterStore** will be forked from this repo in the coming weeks. `btrfs-nfs-csi` started as a Kubernetes CSI driver, ButterStore reframes the codebase as a general-purpose btrfs storage backend (NFS plus FUSE access plus btrfs send/receive replication) with room for Nomad, Proxmox, Docker, and other integrations on top of the same agent. The name leans into the long-running "butter-FS" colloquial pronunciation of btrfs, plus the storage-backend framing. It is a fresh repo, not an in-place rename, `btrfs-nfs-csi` continues to live here. Sorry for the friction this puts on your environment. ButterStore stays backward-compatible with v0.11.0. Upgrading from here will be easy. v0.11.0 ships as `btrfs-nfs-csi` everywhere. Nothing for you to do here.

---

## Highlights

A quick tour of what's new with copy-paste examples. Detailed reference for each item is further down.

### Role-based access control

Tokens in `AGENT_TENANTS` can declare a role and an identity using the format `name:token[:role[:identity]]`. Roles (`readonly`, `mounter`, `user`, `admin`) gate which endpoints the token can call. Identity stamps `created-by` on every resource the token creates and restricts later changes to the same identity. Plain `name:token` entries keep the v0.10.0 behavior.

```bash
AGENT_TENANTS="ops:s3cret:admin,ci:tok-ci:user:ci-bot,nodes:tok-n1:mounter:node-1,dash:tok-dash:readonly"
```

### Bcrypt-hashable tokens

Store bcrypt hashes in `AGENT_TENANTS` instead of plaintext. The agent verifies them in constant time, caches successful verifies for hot-path performance, and never logs the raw value. Generate hashes locally with the bundled CLI (no `htpasswd` or `openssl` needed).

```bash
echo -n "my-token" | btrfs-nfs-csi hash-token --cost 12   # prints $2a$12$...
```

### Token introspection

Two new endpoints let operators check who is authenticated and what tokens are configured for a tenant, without ever exposing the raw token values.

```bash
btrfs-nfs-csi whoami    # this token: tenant, role, identity, fingerprint
btrfs-nfs-csi tokens    # all configured tokens in this tenant (admin only)
```

### btrfs maintenance task suite

Three new task types join `scrub` for filesystem-wide and per-volume maintenance, all with a shared mutex (no two filesystem-wide tasks at once) and progress tracking.

```bash
btrfs-nfs-csi task create balance --dusage=75 -W              # rebalance lightly-used chunks
btrfs-nfs-csi task create balance --dconvert=raid1 --force -W # convert RAID profile
btrfs-nfs-csi task create defragment --volume my-vol -W       # per-volume defrag
btrfs-nfs-csi task create quota-rescan -W                     # rebuild qgroup accounting
```

### Scrub flag set

Scrub gained `--readonly`, `--force`, `--ioprio-class`, and `--ioprio-classdata` to control repair behavior and I/O priority. Useful for off-hours scheduled scrubs that should not starve foreground workloads.

```bash
btrfs-nfs-csi task create scrub --readonly --ioprio-class=3 -W
```

---

## New Features (detailed)

### Balance Task

- **`balance` task type**, `task create balance` on the CLI, `POST /v1/tasks/balance` on the API. Same lifecycle, progress, and cancellation as scrub.
- **Filters**, Usage filters via `--dusage`, `--musage`, `--susage`. Profile filters via `--dprofiles`, `--mprofiles`. Device filters via `--ddevid`, `--mdevid`.
- **RAID conversion**, `--dconvert`, `--mconvert`, `--sconvert` for data, metadata, and system block group profile conversion. `--force` allows redundancy reduction.
- **Cannot run with scrub or quota-rescan**, Balance, scrub, and quota-rescan all touch the whole filesystem and only one runs at a time. Starting a second one while another is running returns `423 BUSY`.
- **Cancel propagation**, Cancelling the task runs `btrfs balance cancel` so the kernel-level operation stops with the task.

### Scrub Improvements

- **`--readonly`**, Reports errors without attempting repair.
- **`--force`**, Bypasses the kernel check that prevents starting a scrub on a filesystem that was recently scrubbed.
- **`--ioprio-class`**, IO priority class (0=none, 1=realtime, 2=best-effort, 3=idle).
- **`--ioprio-classdata`**, IO priority within class (0-7).
- **Validation**, The agent rejects invalid values with `400 INVALID` before they reach the kernel. The agent always passes `btrfs scrub`'s `-B` flag itself because progress tracking depends on it, so this is not exposed as an option.

### Defragment Task

- **`defragment` task type**, `task create defragment --volume <name>` defragments a specific tenant volume.
- **Path scope**, `--path` narrows defragment to a sub-directory under the volume's data root.
- **Options**, `--compress <algo>` rewrites extents with the given compression, `--threshold <bytes>` sets minimum extent size, `--no-recursive` skips subdirectories.
- **Path validation**, The agent rejects path traversal (`..`) and symlinks that point outside the volume before any defragment runs against the disk.
- **Snapshots not supported**, The agent creates snapshots read-only. Clones are writable and defragment normally.

### Quota Rescan Task

- **`quota-rescan` task type**, `task create quota-rescan` rebuilds btrfs qgroup accounting filesystem-wide.
- **Cannot run with scrub or balance**, Quota rescan touches the whole filesystem and refuses to start while a scrub or balance task is already running, and vice versa.
- **Squota error**, Filesystems on simple quotas (`squota`) return a clear error immediately. btrfs does not support rescan in that mode. Only classic qgroups can be rebuilt.

---

## Security & Identity

### Three-Level RBAC

- **Roles**, Four roles (`readonly`, `mounter`, `user`, `admin`) gate endpoint access. Per-endpoint matrix in `docs/rbac.md`.
- **Identity**, Optional identity string (`a-zA-Z0-9_-`, 1-32). The agent writes it into a `created-by` label on every resource the token creates, and only that identity can change or delete those resources later.
- **`AGENT_TENANTS` syntax**, `name:token[:role[:identity]]`. Trailing fields are optional, omission gives admin without identity.
- **Mixed configs**, A tenant can declare any combination of roles and identities in a single `AGENT_TENANTS` value.

### Resource Ownership

- **Source ownership**, Snapshot, clone, export, and defragment verify the caller's identity matches the source's `created-by` label.
- **`created-by` enforcement**, On every endpoint that creates, updates, or deletes something, the agent compares the calling token's identity to the resource's owner before letting the request through.
- **PATCH boundary**, `created-by` cannot be cleared or changed via update, including by admins.
- **Hard-reserved labels**, `tenant`, `clone.source.type`, and `clone.source.name` are rejected with `400 INVALID` if set by a client.
- **Server-injected `created-by`**, Clients with no token identity may set `created-by` (used by CSI controller and CLI). Clients with a token identity must either omit it or send the matching value.

### Bcrypt Tokens

- **Hashed tokens**, `AGENT_TENANTS` token field accepts bcrypt hashes (`$2a$`, `$2b$`, `$2y$`) alongside plaintext.
- **`hash-token` subcommand**, `btrfs-nfs-csi hash-token --cost N` reads from stdin and prints a bcrypt hash. No `htpasswd` or `openssl` required.
- **Verify cache**, The slow bcrypt check runs only on the first request per token. After that the agent remembers the result, so working clients keep their normal latency.
- **Timing-safe compare**, Plaintext tokens are compared in constant time, so the agent's response time does not leak information about the token to an attacker.

**Side note.** Bcrypt is intentionally slow, which is the point if your `AGENT_TENANTS` ever leaks. The catch: the agent runs that slow check on every rejected request, once per bcrypt entry, so an attacker can spam random tokens to burn CPU without ever holding a real one (with ten entries at the default cost, one bad request costs ~2.5 s). Keep the bcrypt list short, its good for user provided passwords or extended hardening. A planned follow-up will integrate hash generation using the new root_secret.

### Telemetry & Introspection

- **Denial reason metric**, Every `403` increments `http_requests_total` with a `reason` label of `invalid_token`, `role_denied`, `identity_mismatch`, or `ownership`.
- **Denial log**, Each `403` emits a warn log with client IP, tenant, path, token fingerprint, role, and identity.
- **`GET /v1/whoami`**, Returns the caller's tenant, role, identity, and a stable fingerprint that maps the token to its audit log entries without exposing the token itself.
- **`GET /v1/tokens`**, Admin-only. Lists every configured token in the caller's tenant with fingerprint, role, and identity. Raw tokens are never returned.
- **Root secret**, Fingerprints come from a per-installation secret stored at `AGENT_BASE_PATH/metadata/root_secret`, generated on first boot. A `root_secret.bak` copy sits next to it. If the two ever drift apart, the agent refuses to start so it does not silently invalidate every fingerprint in your audit log.

---

## Bug Fixes

- **Reserved internal names**, `tasks`, `snapshots`, `data`, and `metadata` (case-insensitive) are rejected as tenant names and as volume/clone names. `AGENT_TENANTS` is validated at boot, volume/clone names are validated at the API boundary.
- **`clone.source.*` labels hard-rejected** (#152), `clone.source.type` and `clone.source.name` are rejected on volume create, clone, snapshot, export, task, and update endpoints. Previously plain `POST /v1/volumes` accepted them as user labels and the dedicated clone endpoints silently overwrote them.
- **Pagination defaults applied** (#153), `AGENT_DEFAULT_PAGE_LIMIT` (server) and `AGENT_HTTP_CLIENT_PAGE_LIMIT` (client) now apply when no explicit limit is provided. Previously they only took effect on explicit negative values.
- **Pagination cursor errors return 400** (#153), Invalid, expired, or cross-list cursors return `400 INVALID` instead of restarting from page 1.

---

## Behavior Changes

- **PATCH rejects same-value `clone.source.*`**, A `PATCH` request that includes `clone.source.type` or `clone.source.name` returns `400 INVALID` even when the supplied value matches the current one. The CSI driver and CLI send delta updates and rely on the server-side label auto-merge, so no in-tree client is affected.

---

## Upgrade Guide

### Kubernetes CSI users

Drop-in upgrade. Helm values, StorageClasses, PVCs, VolumeSnapshots, and clones continue to work. The CSI driver does not set any of the affected labels and uses no reserved names. Bump the image tag to `0.11.0`.

### `AGENT_TENANTS`

Existing `name:token` entries continue to behave as admin without identity. No config change required to keep the v0.10.0 behavior. Opt into RBAC by adding `:role` or `:role:identity` per token. See `docs/rbac.md`.

### Reserved internal names

`tasks`, `snapshots`, `data`, `metadata` (case-insensitive) are now rejected as tenant names and as volume/clone names. Rename any existing tenant or volume with one of these names before upgrading. The agent fails at boot if `AGENT_TENANTS` contains a reserved tenant name.

### Server-managed labels

`tenant`, `clone.source.type`, `clone.source.name` are rejected if set by a client. The CSI driver and CLI never set these. Custom REST automation that sends them must stop doing so.

### Manual `setup.yaml` users

Update the container image tag in `deploy/driver/setup.yaml` to `0.11.0`.

---

## Deprecations

None.
