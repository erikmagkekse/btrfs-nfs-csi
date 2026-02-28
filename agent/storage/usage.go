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
	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/rs/zerolog/log"
)

// StartUsageUpdater periodically updates used_bytes in each volume's metadata.json.
func StartUsageUpdater(ctx context.Context, mgr *btrfs.Manager, basePath string, interval time.Duration, tenant string) {
	go func() {
		updateAll(ctx, mgr, basePath, tenant)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateAll(ctx, mgr, basePath, tenant)
			}
		}
	}()
}

func updateAll(ctx context.Context, mgr *btrfs.Manager, basePath string, tenant string) {
	log.Debug().Str("tenant", tenant).Msg("usage updater: starting scan")

	entries, err := os.ReadDir(basePath)
	if err != nil {
		log.Error().Err(err).Msg("usage updater: failed to read base path")
		return
	}

	var updated, failed, count int
	for _, e := range entries {
		if !e.IsDir() || e.Name() == config.SnapshotsDir {
			continue
		}

		metaPath := filepath.Join(basePath, e.Name(), config.MetadataFile)
		dataDir := filepath.Join(basePath, e.Name(), config.DataDir)

		var meta VolumeMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}

		count++
		changed := false

		// detect filesystem ownership/mode drift (nodes may chown/chmod via NFS)
		info, err := os.Stat(dataDir)
		if err != nil {
			log.Warn().Err(err).Str("volume", e.Name()).Msg("usage updater: stat failed, skipping volume")
			failed++
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			log.Warn().Str("volume", e.Name()).Msg("usage updater: syscall stat error, skipping volume")
			failed++
			continue
		}
		fsUID, fsGID := int(stat.Uid), int(stat.Gid)
		fsMode := fmt.Sprintf("%o", unixMode(info.Mode()))
		changed = fsUID != meta.UID || fsGID != meta.GID || fsMode != meta.Mode

		VolumeSizeBytes.WithLabelValues(tenant, e.Name()).Set(float64(meta.QuotaBytes))
		VolumeUsedBytes.WithLabelValues(tenant, e.Name()).Set(float64(meta.UsedBytes))

		// detect usage drift
		var used uint64
		if meta.QuotaBytes > 0 {
			u, err := mgr.QgroupUsage(ctx, dataDir)
			if err != nil {
				log.Warn().Err(err).Str("volume", e.Name()).Msg("usage updater: qgroup query failed, skipping volume - if issue persists check your quotas")
				failed++
				continue
			}
			used = u
			if used != meta.UsedBytes {
				changed = true
			}
		}

		if !changed {
			continue
		}

		ev := log.Debug().Str("volume", e.Name())
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

		if err := UpdateMetadata(metaPath, func(m *VolumeMetadata) {
			m.UID = fsUID
			m.GID = fsGID
			m.Mode = fsMode
			m.UsedBytes = used
			m.UpdatedAt = time.Now().UTC()
		}); err != nil {
			log.Error().Err(err).Str("volume", e.Name()).Msg("usage updater: failed to write metadata")
			failed++
			continue
		}
		updated++
	}

	VolumesGauge.WithLabelValues(tenant).Set(float64(count))
	log.Info().Str("tenant", tenant).Int("volumes", count).Int("updated", updated).Int("failed", failed).Msg("usage updater: volume scan complete")

	// update snapshot usage
	snapDir := filepath.Join(basePath, config.SnapshotsDir)
	snapEntries, err := os.ReadDir(snapDir)
	if err != nil {
		return
	}

	var snapUpdated, snapFailed int
	for _, e := range snapEntries {
		if !e.IsDir() {
			continue
		}

		metaPath := filepath.Join(snapDir, e.Name(), config.MetadataFile)
		dataDir := filepath.Join(snapDir, e.Name(), config.DataDir)

		var meta SnapshotMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}

		info, err := mgr.QgroupUsageEx(ctx, dataDir)
		if err != nil {
			log.Debug().Err(err).Str("snapshot", e.Name()).Msg("usage updater: snapshot qgroup query failed")
			snapFailed++
			continue
		}

		if info.Referenced == meta.UsedBytes && info.Exclusive == meta.ExclusiveBytes {
			continue
		}

		if err := UpdateMetadata(metaPath, func(m *SnapshotMetadata) {
			m.UsedBytes = info.Referenced
			m.ExclusiveBytes = info.Exclusive
			m.UpdatedAt = time.Now().UTC()
		}); err != nil {
			log.Error().Err(err).Str("snapshot", e.Name()).Msg("usage updater: failed to write snapshot metadata")
			snapFailed++
			continue
		}
		snapUpdated++
	}

	if snapUpdated > 0 || snapFailed > 0 {
		log.Info().Str("tenant", tenant).Int("updated", snapUpdated).Int("failed", snapFailed).Msg("usage updater: snapshot scan complete")
	}
}
