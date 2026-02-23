#!/bin/bash
set -euo pipefail

IMG="/tmp/btrfs-nfs-csi-dev.img"
MNT="/tmp/btrfs-nfs-csi-dev"

if [ "$EUID" -ne 0 ]; then
    exec sudo "$0" "$@"
fi

case "${1:-up}" in
    up)
        if [ ! -f "$IMG" ]; then
            echo "Creating btrfs loopback image..."
            truncate -s 1G "$IMG"
            mkfs.btrfs -f "$IMG"
        fi

        if ! mountpoint -q "$MNT" 2>/dev/null; then
            echo "Mounting btrfs image..."
            mkdir -p "$MNT"
            mount -o loop "$IMG" "$MNT"
        fi

        btrfs quota enable "$MNT" 2>/dev/null || true

        echo "btrfs ready at $MNT"
        ;;
    down)
        if mountpoint -q "$MNT" 2>/dev/null; then
            umount "$MNT"
            echo "btrfs unmounted"
        else
            echo "not mounted"
        fi
        ;;
    *)
        echo "Usage: $0 [up|down]"
        exit 1
        ;;
esac
