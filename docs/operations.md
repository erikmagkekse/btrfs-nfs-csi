# Operations

## Snapshots

Read-only btrfs snapshots. Instant, CoW-efficient.

```bash
btrfs-nfs-csi snapshot create my-app before-deploy
btrfs-nfs-csi snapshot list
btrfs-nfs-csi snapshot clone before-deploy my-app-restored
```

Agent: `btrfs subvolume snapshot -r <src>/data <dst>/data`, stored under `{basePath}/{tenant}/snapshots/{name}/`.

Usage updater tracks `used_bytes` (referenced) and `exclusive_bytes` (unique blocks).

## Clones

### From Snapshot

Writable clone from a read-only snapshot. Instant, independent of source. The clone inherits the source volume's properties (size, quota, compression, nocow, uid, gid, mode) and has qgroup limits applied.

```bash
btrfs-nfs-csi snapshot clone before-deploy my-app-restored --label env=dev
```

### From Volume (Volume-to-Volume)

Direct clone from an existing volume. No intermediate snapshot needed, a single atomic `btrfs subvolume snapshot` under the hood.

```bash
btrfs-nfs-csi volume clone my-app my-app-staging --label env=staging
```

Both clone types are instant (btrfs CoW), independent of the source, and stored at `{basePath}/{tenant}/{name}/`.

## Expansion

Online volume expansion. Updates btrfs qgroup limit only. New size must be larger than current size. Supports absolute and relative sizes.

```bash
btrfs-nfs-csi volume expand my-app 20Gi     # absolute
btrfs-nfs-csi volume expand my-app +5Gi     # relative
```

## Compression

| Algorithm | Notes |
|---|---|
| `zstd` | Recommended. Optional level: `zstd:3` (1-15) |
| `lzo` | Fastest, lower ratio. No level suffix (bare `lzo` only) |
| `zlib` | Highest ratio, slowest. Optional level: `zlib:6` (1-9) |
| `none` | Disable |

```bash
btrfs-nfs-csi volume create logs 5Gi --compression zstd
```

Applies to new writes only. Mutually exclusive with NoCOW.

## NoCOW

`chattr +C` disables copy-on-write. Use for databases, VM images.

```bash
btrfs-nfs-csi volume create postgres-data 50Gi --nocow
```

Trade-off: no snapshots/clones, no checksums, no compression. Better random write performance.

## Quota

Enabled by default (`AGENT_FEATURE_QUOTA_ENABLED=true`).

Both classic quotas (`btrfs quota enable`) and simple quotas (`btrfs quota enable -s`) are supported. Simple quotas (squota, kernel 6.7+) have lower write overhead because they track ownership by extent lifetime instead of backref walks. The agent uses the same qgroup commands for both modes.

- Create: `btrfs qgroup limit <bytes> <path>`
- Usage updater: polls `btrfs qgroup show` at `AGENT_FEATURE_QUOTA_UPDATE_INTERVAL`

## UID / GID / Mode

Set ownership and permissions at volume creation via API. Default mode: `2770` (configurable via `AGENT_DEFAULT_DATA_MODE`). Applied at creation via `chown`/`chmod`.

UID and GID must be between 0 and 65534. Mode must be valid octal between 0000 and 7777.

## NFS Exports

Export options: `rw,nohide,crossmnt,no_root_squash,no_subtree_check,fsid=<crc32>`

```bash
btrfs-nfs-csi export add my-app 10.0.1.1
btrfs-nfs-csi export list
btrfs-nfs-csi export remove my-app 10.0.1.1
```

Exports are reference-counted per client IP. The NFS kernel export is only created on the first reference for an IP and removed when the last reference is gone.

**Reconciler** (every `AGENT_NFS_RECONCILE_INTERVAL`, default 60s):
- Removes orphaned exports (path deleted)
- Re-adds missing exports from metadata (agent restart recovery)

**Recommended mount options:**
```
nfsvers=4.2,hard,noatime,rsize=1048576,wsize=1048576,nconnect=8
```

> **Planned:** A FUSE mount backend will allow mounting volumes via WebSocket through the agent API, removing the dependency on kernel NFS and enabling very dynamic integrations. Combined with mTLS, this will provide end-to-end encrypted and authenticated data transport.

## Tasks

The agent runs heavy maintenance and per-volume work as background tasks. Tasks are started via `btrfs-nfs-csi task create <type>`, persisted to disk, and survive restarts (interrupted ones are marked `failed`). Completed tasks are cleaned up after `AGENT_TASK_CLEANUP_INTERVAL` (default 24h). Every task accepts the common flags `--timeout`, `--wait` / `-W`, and `--label`. Role and ownership rules are detailed in [rbac.md](rbac.md#per-endpoint-matrix).

### Scrub

| | |
|---|---|
| **Type** | `scrub` |
| **Purpose** | Verify data integrity by reading all blocks and checking checksums. |
| **Role** | `admin` |
| **Scope** | filesystem-wide |
| **Required args** | none |
| **Optional flags** | `--readonly` / `-r`: audit-only, no repair attempt.<br>`--force` / `-f`: start even if a previous scrub record exists on the FS.<br>`--ioprio-class`: IO priority class (`0` none, `1` realtime, `2` best-effort, `3` idle).<br>`--ioprio-classdata`: priority within the class (`0`-`7`, `0` highest). |
| **Conflicts with** | scrub / balance / quota-rescan |
| **Progress** | live, kernel reports byte counter |
| **Result** | bytes scrubbed, error counts |
| **Notes** | `-B` (foreground) is always set so the agent can track progress and enforce timeouts. Externally started scrubs (e.g. `btrfs scrub start` on the host) are detected and duplicates are rejected. |

```bash
btrfs-nfs-csi task create scrub -W                  # start and wait
btrfs-nfs-csi task create scrub --readonly -W       # audit only, no repair attempt
btrfs-nfs-csi task create scrub --ioprio-class=3 -W # idle IO priority (good for scheduled scrubs)
```

### Quota Rescan

| | |
|---|---|
| **Type** | `quota-rescan` |
| **Purpose** | Rebuild qgroup accounting from scratch. Useful after qgroup inconsistencies (quotas enabled on an existing filesystem, unusual delete/snapshot patterns). |
| **Role** | `admin` |
| **Scope** | filesystem-wide, classic qgroups only (`btrfs quota enable`) |
| **Required args** | none |
| **Optional flags** | none beyond common |
| **Conflicts with** | scrub / balance / quota-rescan |
| **Progress** | coarse (`0%` pending, `50%` running, `100%` done). btrfs has no structured progress for rescan. |
| **Notes** | Simple quotas (squota, kernel 6.7+, `btrfs quota enable -s`) do not support rescan. Accounting there is per-extent-lifetime with no historical state to rebuild. Running rescan on a squota filesystem fails fast with a clear error. |

```bash
btrfs-nfs-csi task create quota-rescan -W
```

### Defragment

| | |
|---|---|
| **Type** | `defragment` |
| **Purpose** | Rewrite files to reduce extent fragmentation, optionally recompressing while doing so. |
| **Role** | `user` or `admin` |
| **Scope** | volume or sub-path within a volume, ownership-checked against the source volume (same rule as snapshot and clone) |
| **Required args** | `--volume` |
| **Optional flags** | `--path`: sub-path relative to the volume data dir. Absolute paths and `..` traversal are rejected. Symlinks must resolve under the target dir.<br>`--compress zstd\|lzo\|zlib\|none` (default `none`): recompress during defrag.<br>`--no-recursive`: do not recurse into subdirectories (default is recursive).<br>`--threshold <bytes>`: target extent size. btrfs skips files whose extents already exceed it. |
| **Conflicts with** | none, runs concurrently with scrub or balance |
| **Progress** | coarse (`0%` pending, `50%` running, `100%` done). A defragment that had started but failed or was cancelled stays at `50%`, distinguishing it from one that never got going. |
| **Notes** | Snapshots are read-only and cannot be defragmented, clone first if you need a point-in-time defrag. Defragment writes new extents before freeing the old ones, so the volume needs free quota headroom (100% qgroup quota fails with `Disk quota exceeded`). Can break block-sharing between snapshots and clones, so disk usage may grow noticeably afterwards. |

```bash
btrfs-nfs-csi task create defragment --volume my-vol -W                   # whole volume
btrfs-nfs-csi task create defragment --volume my-vol --path logs          # sub-path
btrfs-nfs-csi task create defragment --volume pg-main --compress=zstd -W  # recompress while defragging
```

### Balance

| | |
|---|---|
| **Type** | `balance` |
| **Purpose** | Rewrite chunks to reclaim wasted space, consolidate half-empty chunks, or change the RAID profile. Routine `--dusage`/`--musage` runs are what fix "No space left on device" errors on btrfs while `df` still shows free space. |
| **Role** | `admin` |
| **Scope** | filesystem-wide |
| **Required args** | none (unfiltered balance rewrites every chunk and logs a warning, almost never what you want on a running FS) |
| **Optional flags** | `--dusage <pct>`, `--musage <pct>`: consolidate data/metadata chunks below `pct` full.<br>`--dconvert <profile>`, `--mconvert <profile>`: change RAID profile (`raid1`, `single`, ...).<br>`--ddevid <id>`, `--mdevid <id>`: drain a specific device by ID.<br>`--force` / `-f`: required when reducing redundancy.<br>`--soft`: only convert chunks not already in the target profile. |
| **Conflicts with** | scrub / balance / quota-rescan |
| **Progress** | live, kernel reports chunks remaining |
| **Notes** | `task cancel <id>` triggers `btrfs balance cancel` on the kernel side, not just a process kill. Balance state is not persisted across reboots, so interrupted balances do not resume automatically. A scheduled monthly balance is analogous to the scrub timer above with `ExecStart=btrfs-nfs-csi task create balance --dusage=75 -W`. |

```bash
# routine maintenance, scheduled monthly: consolidate sparsely-filled chunks
btrfs-nfs-csi task create balance --dusage=75 --musage=30 -W

# after adding a second disk: convert single to raid1 across both devices
btrfs-nfs-csi task create balance --dconvert=raid1 --mconvert=raid1 -W

# drain device 3 before removing it (find the devid in `btrfs-nfs-csi stats -o wide`)
btrfs-nfs-csi task create balance --ddevid=3 --mdevid=3 -W
```

## Saved Agents

Saved agent endpoints, so the CLI does not need `AGENT_URL`/`AGENT_TOKEN` in every shell. `agents login <name> --url ...` verifies the token via `/v1/whoami` and persists it to `~/.btrfs-nfs-csi/config.json` (file `0600`, dir `0700`). Token reads from no-echo prompt or stdin pipe. Subsequent CLI commands fall back per field to the active entry.

```bash
echo "$TOKEN" | btrfs-nfs-csi agents login prod --url https://agent.example.com:8080
btrfs-nfs-csi agents ls
btrfs-nfs-csi agents use prod
btrfs-nfs-csi volume list                  # no env vars needed
btrfs-nfs-csi agents verify --all
```

Subcommands: `login`, `logout [<name>]`, `ls` (`-o wide`/`-o json`), `use <name>`, `verify [<name>] [--all]`. Precedence: flag > env > active entry. `agents login --tls-skip-verify` persists a per-agent TLS toggle for self-signed endpoints. Token is never written to any stdout, only the on-disk file. Override the path with `BTRFS_NFS_CSI_CONFIG_FILE`.

## More CLI examples

```bash
# When no agent is saved, fall back to env vars:
export AGENT_URL=http://10.0.0.5:8080
export AGENT_TOKEN=changeme
export AGENT_CSI_IDENTITY=cli              # optional, default: cli

btrfs-nfs-csi volume list
btrfs-nfs-csi volume ls -o wide
btrfs-nfs-csi volume ls -o json
btrfs-nfs-csi volume ls -c name              # single column, no header (pipeable)
btrfs-nfs-csi volume ls -c name,size         # selected columns
btrfs-nfs-csi volume ls -w                   # watch mode (2s refresh)
btrfs-nfs-csi volume ls -w 500ms             # watch with custom interval
btrfs-nfs-csi volume get my-vol
btrfs-nfs-csi volume create my-vol 10Gi --compression zstd
btrfs-nfs-csi volume create my-vol 10Gi --label env=prod --label team=backend
btrfs-nfs-csi volume expand my-vol 20Gi      # absolute size
btrfs-nfs-csi volume expand my-vol +5Gi      # relative expand
btrfs-nfs-csi volume clone source-vol new-vol --label env=staging
btrfs-nfs-csi volume ls -l env=prod          # filter by label
btrfs-nfs-csi volume ls -l env=prod,team=be  # comma-separated (AND)
btrfs-nfs-csi volume delete my-vol           # safe: only deletes if created-by matches caller
btrfs-nfs-csi volume delete my-vol --confirm --yes  # force delete any volume
btrfs-nfs-csi volume set my-vol --uid 1000 --gid 1000  # change owner
btrfs-nfs-csi volume set my-vol --mode 0755            # change permissions
btrfs-nfs-csi volume set my-vol --compression zstd     # change compression
btrfs-nfs-csi volume set my-vol --nocow                # disable copy-on-write
btrfs-nfs-csi volume label list my-vol       # show labels
btrfs-nfs-csi volume label add my-vol env=prod tier=hot  # add/update labels
btrfs-nfs-csi volume label remove my-vol tier            # remove by key
btrfs-nfs-csi volume label remove my-vol tier=hot        # remove by key+value (must match)
btrfs-nfs-csi volume label patch my-vol env=staging  # replace all labels (preserves reserved labels)

# xargs pipeline: delete all CLI-created volumes matching a pattern
btrfs-nfs-csi volume ls -c name | grep '^test-' | xargs btrfs-nfs-csi volume delete

btrfs-nfs-csi snapshot list
btrfs-nfs-csi snapshot ls my-vol             # filter by volume
btrfs-nfs-csi snapshot ls -l env=prod
btrfs-nfs-csi snapshot create my-vol snap-1 --label env=prod
btrfs-nfs-csi snapshot clone snap-1 new-vol --label env=dev
btrfs-nfs-csi snapshot delete snap-1
btrfs-nfs-csi snapshot label list snap-1     # show labels

btrfs-nfs-csi export list
btrfs-nfs-csi export add my-vol 10.1.0.50
btrfs-nfs-csi export remove my-vol 10.1.0.50

btrfs-nfs-csi task list
btrfs-nfs-csi task ls -t scrub
btrfs-nfs-csi task ls -l created-by=cron
btrfs-nfs-csi task ls -w                     # watch tasks live
btrfs-nfs-csi task get <id>
btrfs-nfs-csi task cancel <id>
btrfs-nfs-csi task create scrub
btrfs-nfs-csi task create scrub -W           # wait for completion
btrfs-nfs-csi task create test --sleep 10s -W   # no-op smoke test, useful to verify task plumbing
btrfs-nfs-csi stats
btrfs-nfs-csi stats -o wide                  # per-device IO and error details
btrfs-nfs-csi stats -w                       # watch stats live
btrfs-nfs-csi health
btrfs-nfs-csi version
```

**Global flags:** `--agent-url`, `--agent-token`, `--output` / `-o` (table, wide, json).

**Output formats:** `table` (default), `wide` (extra columns), `json` (raw API response). Combine with `-o json,wide` for detailed JSON. `-o json` suppresses output for commands that change state without an API response (delete, export add/rm, task cancel).

**Column filter:** `--columns` / `-c` selects which columns to display. Single column omits the header for clean piping to `xargs`, `wc`, etc.

**Watch mode:** `--watch` / `-w` enables live-refresh in an alternate screen. Default 2s, configurable (e.g. `-w 500ms`). Available on all list commands, get commands, `stats`, and `health`.

**Sorting:** `--sort` / `-s` with `--asc` (default descending). Volume default: `used%`. Snapshot/task default: `created`.

**Default filter:** List commands filter by `created-by=cli` by default (only show resources created by the CLI). Use `--all` / `-A` to show all resources regardless of creator.

**Label filter:** `--label` / `-l`, repeatable (AND). Supports comma-separated values: `-l env=prod,team=be`.

**Size values:** Supports `Ki`, `Mi`, `Gi` (binary) and `K`, `M`, `G` (decimal). `volume expand` accepts relative sizes with `+` prefix (e.g. `+5Gi`).

**Delete protection:** Volumes and snapshots with `created-by` != caller identity are protected. Only `--confirm --yes` or `BTRFS_NFS_CSI_FORCE=true` bypasses this. The caller identity defaults to `cli` and can be set via `AGENT_CSI_IDENTITY`.

**Default labels:** Every create command automatically adds `created-by=<identity>` (default `cli`). The `created-by` label cannot be set via `--label` flag or PVC annotations.

**Command aliases:** `volume`/`volumes`/`vol`/`v`, `snapshot`/`snapshots`/`snap`/`s`, `export`/`exports`/`e`, `task`/`tasks`/`t`. `list`/`ls`/`l`, `create`/`c`, `get`/`g`, `delete`/`rm`/`d`, `set`/`s`, `expand`/`e`, `clone`/`cl`, `label`/`labels`/`lb`, `add`/`a`, `remove`/`rm`/`r`, `patch`/`p`.
