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
func StartUsageUpdater(ctx context.Context, basePath string, interval time.Duration, tenant string) {
	go func() {
		updateAll(ctx, basePath, tenant)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updateAll(ctx, basePath, tenant)
			}
		}
	}()
}

func updateAll(ctx context.Context, basePath string, tenant string) {
	log.Debug().Str("tenant", tenant).Msg("usage updater: starting scan")

	entries, err := os.ReadDir(basePath)
	if err != nil {
		log.Error().Err(err).Msg("usage updater: failed to read base path")
		return
	}

	var updated, failed, count int
	for _, e := range entries {
		if !e.IsDir() || e.Name() == SnapshotsDir {
			continue
		}

		metaPath := filepath.Join(basePath, e.Name(), MetadataFile)
		dataDir := filepath.Join(basePath, e.Name(), DataDir)

		var meta VolumeMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}

		count++
		changed := false

		// detect filesystem ownership/mode drift (nodes may chown/chmod via NFS)
		var fsUID, fsGID int
		var fsMode string
		if info, err := os.Stat(dataDir); err == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok {
				fsUID, fsGID = int(stat.Uid), int(stat.Gid)
				fsMode = fmt.Sprintf("%o", info.Mode().Perm()|info.Mode()&os.ModeSetgid)
				changed = fsUID != meta.UID || fsGID != meta.GID || fsMode != meta.Mode
			}
		}

		VolumeSizeBytes.WithLabelValues(tenant, e.Name()).Set(float64(meta.QuotaBytes))
		VolumeUsedBytes.WithLabelValues(tenant, e.Name()).Set(float64(meta.UsedBytes))

		// detect usage drift
		var used uint64
		if meta.QuotaBytes > 0 {
			u, err := btrfs.QgroupUsage(ctx, dataDir)
			if err != nil {
				log.Debug().Err(err).Str("volume", e.Name()).Msg("usage updater: qgroup query failed")
				failed++
			} else {
				log.Debug().Str("volume", e.Name()).Uint64("used", u).Uint64("quota", meta.QuotaBytes).Msg("usage updater: volume stats")
				used = u
				if used != meta.UsedBytes {
					changed = true
				}
			}
		}

		if !changed {
			continue
		}

		if err := UpdateMetadata(metaPath, func(m *VolumeMetadata) {
			if fsMode != "" {
				m.UID = fsUID
				m.GID = fsGID
				m.Mode = fsMode
			}
			if used != 0 {
				m.UsedBytes = used
			}
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
	snapDir := filepath.Join(basePath, SnapshotsDir)
	snapEntries, err := os.ReadDir(snapDir)
	if err != nil {
		return
	}

	var snapUpdated, snapFailed int
	for _, e := range snapEntries {
		if !e.IsDir() {
			continue
		}

		metaPath := filepath.Join(snapDir, e.Name(), MetadataFile)
		dataDir := filepath.Join(snapDir, e.Name(), DataDir)

		var meta SnapshotMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}

		info, err := btrfs.QgroupUsageEx(ctx, dataDir)
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
