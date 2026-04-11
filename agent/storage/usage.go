// TODO: We should add some metrics here for failed stats in the future

package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"

	"github.com/rs/zerolog/log"
)

// StartUsageUpdater periodically updates used_bytes in each volume's metadata.json.
func (s *Storage) startUsageUpdater(ctx context.Context, mgr *btrfs.Manager, interval time.Duration, tenant string) {
	go func() {
		s.updateAll(ctx, mgr, tenant)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.updateAll(ctx, mgr, tenant)
			}
		}
	}()
}

func (s *Storage) updateAll(ctx context.Context, mgr *btrfs.Manager, tenant string) {
	start := time.Now()
	log.Debug().Str("tenant", tenant).Msg("usage updater: starting scan")

	// bulk-fetch qgroup usage: 2 btrfs commands total instead of 2*N
	var usageMap map[string]btrfs.QgroupInfo
	if s.quotaEnabled {
		var err error
		usageMap, err = mgr.QgroupUsageBulk(ctx, s.mountPoint)
		if err != nil {
			log.Warn().Err(err).Str("tenant", tenant).Msg("usage updater: bulk qgroup query failed, skipping usage updates")
		}
	}

	var updated, failed, count int
	s.volumes.Range(func(t, name string, cached *VolumeMetadata) bool {
		if t != tenant {
			return true
		}

		dataDir := s.volumes.DataPath(tenant, name)
		meta := *cached
		count++
		changed := false

		// detect filesystem ownership/mode drift (nodes may chown/chmod via NFS)
		info, err := os.Stat(dataDir)
		if err != nil {
			log.Warn().Err(err).Str("volume", name).Msg("usage updater: stat failed, skipping volume")
			failed++
			return true
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			log.Warn().Str("volume", name).Msg("usage updater: syscall stat error, skipping volume")
			failed++
			return true
		}
		fsUID, fsGID := int(stat.Uid), int(stat.Gid)
		fsMode := fmt.Sprintf("%o", unixMode(info.Mode()))
		changed = fsUID != meta.UID || fsGID != meta.GID || fsMode != meta.Mode

		VolumeSizeBytes.WithLabelValues(tenant, name).Set(float64(meta.QuotaBytes))
		VolumeUsedBytes.WithLabelValues(tenant, name).Set(float64(meta.UsedBytes))

		// detect usage drift
		var used uint64
		if meta.QuotaBytes > 0 && usageMap != nil {
			relPath, _ := filepath.Rel(s.mountPoint, dataDir)
			if qi, ok := usageMap[relPath]; ok {
				used = qi.Referenced
			} else {
				log.Warn().Str("volume", name).Str("path", relPath).Msg("usage updater: volume not found in bulk qgroup data")
				failed++
				return true
			}
			if used != meta.UsedBytes {
				changed = true
			}
		}

		if !changed {
			return true
		}

		ev := log.Debug().Str("volume", name)
		if fsUID != meta.UID {
			ev = ev.Int("oldUID", meta.UID).Int("newUID", fsUID)
		}
		if fsGID != meta.GID {
			ev = ev.Int("oldGID", meta.GID).Int("newGID", fsGID)
		}
		if fsMode != meta.Mode {
			ev = ev.Str("oldMode", meta.Mode).Str("newMode", fsMode)
		}
		if used != meta.UsedBytes {
			ev = ev.Uint64("oldUsedBytes", meta.UsedBytes).Uint64("newUsedBytes", used)
		}
		ev.Msg("usage updater: updating metadata")

		if _, err := s.volumes.Update(tenant, name, func(m *VolumeMetadata) {
			m.UID = fsUID
			m.GID = fsGID
			m.Mode = fsMode
			m.UsedBytes = used
			m.UpdatedAt = time.Now().UTC()
		}); err != nil {
			log.Error().Err(err).Str("volume", name).Msg("usage updater: failed to write metadata")
			failed++
			return true
		}
		updated++
		return true
	})

	VolumesGauge.WithLabelValues(tenant).Set(float64(count))
	log.Info().Str("tenant", tenant).Int("volumes", count).Int("updated", updated).Int("failed", failed).Str("took", time.Since(start).String()).Msg("usage updater: scan complete")

	// update snapshot usage
	var snapUpdated, snapFailed, snapCount int
	s.snapshots.Range(func(t, name string, cached *SnapshotMetadata) bool {
		if t != tenant {
			return true
		}
		snapCount++

		dataDir := s.snapshots.DataPath(tenant, name)

		if usageMap == nil {
			return true
		}
		relPath, _ := filepath.Rel(s.mountPoint, dataDir)
		qi, ok := usageMap[relPath]
		if !ok {
			log.Warn().Str("snapshot", name).Str("path", relPath).Msg("usage updater: snapshot not found in bulk qgroup data")
			snapFailed++
			return true
		}

		if qi.Referenced == cached.UsedBytes && qi.Exclusive == cached.ExclusiveBytes {
			return true
		}

		if _, err := s.snapshots.Update(tenant, name, func(m *SnapshotMetadata) {
			m.UsedBytes = qi.Referenced
			m.ExclusiveBytes = qi.Exclusive
			m.UpdatedAt = time.Now().UTC()
		}); err != nil {
			log.Error().Err(err).Str("snapshot", name).Msg("usage updater: failed to write snapshot metadata")
			snapFailed++
			return true
		}
		snapUpdated++
		return true
	})

	log.Debug().Str("tenant", tenant).Int("snapshots", snapCount).Int("updated", snapUpdated).Int("failed", snapFailed).Msg("usage updater: snapshot scan complete")
}
