package driver

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"k8s.io/mount-utils"
)

// newMounter returns a mount.SafeFormatAndMount with force-unmount support.
func newMounter() mount.Interface {
	return mount.New("")
}

// cleanupMountPoint unmounts the path if mounted and removes the directory.
// Uses k8s.io/mount-utils which correctly handles stale NFS mounts (ESTALE,
// EACCES, EIO) via IsCorruptedMnt instead of failing on Lstat.
func cleanupMountPoint(ctx context.Context, mounter mount.Interface, path string) error {
	start := time.Now()

	forceUnmounter, ok := mounter.(mount.MounterForceUnmounter)
	if ok {
		err := mount.CleanupMountWithForce(path, forceUnmounter, true, 30*time.Second)
		if err != nil {
			mountOpsTotal.WithLabelValues("umount", "error").Inc()
			mountDuration.WithLabelValues("umount").Observe(time.Since(start).Seconds())
			log.Error().Err(err).Str("path", path).Msg("cleanup mount point failed")
			return err
		}
	} else {
		err := mount.CleanupMountPoint(path, mounter, true)
		if err != nil {
			mountOpsTotal.WithLabelValues("umount", "error").Inc()
			mountDuration.WithLabelValues("umount").Observe(time.Since(start).Seconds())
			log.Error().Err(err).Str("path", path).Msg("cleanup mount point failed")
			return err
		}
	}

	mountOpsTotal.WithLabelValues("umount", "success").Inc()
	mountDuration.WithLabelValues("umount").Observe(time.Since(start).Seconds())
	log.Info().Str("path", path).Msg("unmounted")
	return nil
}
