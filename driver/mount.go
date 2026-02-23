package driver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

// cleanupMountPoint unmounts the path if it is a mount point and removes the directory.
// Mirrors the behavior of k8s.io/mount-utils CleanupMountPoint.
func cleanupMountPoint(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if isMountPoint(path) {
		log.Info().Str("path", path).Msg("unmounting")
		if err := forceUnmount(ctx, path); err != nil {
			return err
		}
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove mount dir: %w", err)
	}
	return nil
}

func forceUnmount(ctx context.Context, path string) error {
	start := time.Now()
	if err := exec.CommandContext(ctx, "umount", path).Run(); err == nil {
		mountOpsTotal.WithLabelValues("umount", "success").Inc()
		mountDuration.WithLabelValues("umount").Observe(time.Since(start).Seconds())
		return nil
	}
	mountOpsTotal.WithLabelValues("umount", "error").Inc()
	mountDuration.WithLabelValues("umount").Observe(time.Since(start).Seconds())

	log.Warn().Str("path", path).Msg("umount failed, trying force unmount")
	start = time.Now()
	out, err := exec.CommandContext(ctx, "umount", "-f", path).CombinedOutput()
	if err != nil {
		mountOpsTotal.WithLabelValues("force_umount", "error").Inc()
		mountDuration.WithLabelValues("force_umount").Observe(time.Since(start).Seconds())
		return fmt.Errorf("force umount: %w: %s", err, string(out))
	}
	mountOpsTotal.WithLabelValues("force_umount", "success").Inc()
	mountDuration.WithLabelValues("force_umount").Observe(time.Since(start).Seconds())
	return nil
}

func isMountPoint(path string) bool {
	var st, pst unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return false
	}
	if err := unix.Lstat(filepath.Dir(path), &pst); err != nil {
		return false
	}
	return st.Dev != pst.Dev
}
