# Scripts

## Setup

| Script | Description |
|--------|-------------|
| `quickstart-agent.sh` | One-line installer for the agent (Podman Quadlet). Handles install, update, and uninstall. See [installation.md](../docs/installation.md). |
| `agent-dev-setup.sh` | Creates a 1G btrfs loopback image for local development. `up` to mount, `down` to unmount. |

## Testing

| Script | Description |
|--------|-------------|
| `create-100-pvcs.sh` | Bulk create/delete PVCs. Usage: `create-100-pvcs.sh [count] [size] [namespace] [storageclass]` |
| `stress-test.sh` | API stress + lifecycle chaos test. Creates PVCs, snapshots, clones, resizes, and deletes in rapid succession. Usage: `stress-test.sh [count] [rapid] [namespace]` |
| `mixed-load.sh` | fio I/O load + API chaos. Runs fio pods with mixed read/write while snapshotting, resizing, and deleting. Usage: `mixed-load.sh [pods] [read_rate] [write_rate] [namespace]` |
| `stress.sh` | Simple StatefulSet-based stress test with a single fio pod. Usage: `stress.sh [namespace] [storageclass] [timeout]` |

All of these scripts support a `cleanup` subcommand to remove created resources.
