package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateSnapshot(ctx context.Context, tenant string, req SnapshotCreateRequest) (*SnapshotMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	// validation
	if err := validateName(req.Name); err != nil {
		return nil, err
	}
	if err := validateName(req.Volume); err != nil {
		return nil, err
	}
	if err := validateLabels(req.Labels); err != nil {
		return nil, err
	}
	volDir := filepath.Join(bp, req.Volume)
	srcData := filepath.Join(volDir, config.DataDir)
	if _, err := os.Stat(srcData); os.IsNotExist(err) {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source volume %q not found", req.Volume)}
	}
	var volMeta VolumeMetadata
	if err := ReadMetadata(filepath.Join(volDir, config.MetadataFile), &volMeta); err != nil {
		return nil, fmt.Errorf("read volume metadata: %w", err)
	}

	snapDir := filepath.Join(bp, config.SnapshotsDir, req.Name)
	if _, err := os.Stat(snapDir); err == nil {
		return nil, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("snapshot %q already exists", req.Name)}
	}

	// operations
	if err := os.MkdirAll(snapDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create snapshot directory")
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	dstData := filepath.Join(snapDir, config.DataDir)
	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, dstData, true); err != nil {
		_ = os.RemoveAll(snapDir)
		log.Error().Err(err).Msg("failed to create snapshot")
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	now := time.Now().UTC()
	meta := SnapshotMetadata{
		Name:      req.Name,
		Volume:    req.Volume,
		Path:      filepath.Join(filepath.Dir(volMeta.Path), config.SnapshotsDir, req.Name),
		SizeBytes: volMeta.SizeBytes,
		ReadOnly:  true,
		Labels:    req.Labels,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := writeMetadataAtomic(filepath.Join(snapDir, config.MetadataFile), meta); err != nil {
		log.Error().Err(err).Msg("failed to write snapshot metadata")
		if delErr := s.btrfs.SubvolumeDelete(ctx, dstData); delErr != nil {
			log.Warn().Err(delErr).Str("path", dstData).Msg("cleanup: failed to delete subvolume")
		}
		_ = os.RemoveAll(snapDir)
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", req.Name).Str("volume", req.Volume).Msg("snapshot created")
	return &meta, nil
}

func (s *Storage) ListSnapshots(tenant, volume string) ([]SnapshotMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	snapBaseDir := filepath.Join(bp, config.SnapshotsDir)
	entries, err := os.ReadDir(snapBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		log.Error().Err(err).Msg("failed to read snapshots directory")
		return nil, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	var snaps []SnapshotMetadata
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(snapBaseDir, e.Name(), config.MetadataFile)
		var meta SnapshotMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}
		if volume != "" && meta.Volume != volume {
			continue
		}
		snaps = append(snaps, meta)
	}
	log.Debug().Str("tenant", tenant).Int("count", len(snaps)).Msg("snapshots listed")
	return snaps, nil
}

func (s *Storage) ListSnapshotsPaginated(tenant, volume, after string, limit int) (*PaginatedResult[SnapshotMetadata], error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	snapBaseDir := filepath.Join(bp, config.SnapshotsDir)
	entries, err := os.ReadDir(snapBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &PaginatedResult[SnapshotMetadata]{}, nil
		}
		log.Error().Err(err).Msg("failed to read snapshots directory")
		return nil, fmt.Errorf("failed to read snapshots directory: %w", err)
	}

	// when volume filter is set, total requires reading all metadata, so we
	// count directories as an approximation (exact when unfiltered)
	total := 0
	for _, e := range entries {
		if e.IsDir() {
			total++
		}
	}

	var snaps []SnapshotMetadata
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if after != "" && e.Name() <= after {
			continue
		}
		metaPath := filepath.Join(snapBaseDir, e.Name(), config.MetadataFile)
		var meta SnapshotMetadata
		if err := ReadMetadata(metaPath, &meta); err != nil {
			continue
		}
		if volume != "" && meta.Volume != volume {
			continue
		}
		snaps = append(snaps, meta)
		if limit > 0 && len(snaps) > limit {
			break
		}
	}

	result := &PaginatedResult[SnapshotMetadata]{Total: total}
	if limit > 0 && len(snaps) > limit {
		result.Next = snaps[limit-1].Name
		result.Items = snaps[:limit]
	} else {
		result.Items = snaps
	}
	return result, nil
}

func (s *Storage) GetSnapshot(tenant, name string) (*SnapshotMetadata, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}
	if err := validateName(name); err != nil {
		return nil, err
	}

	metaPath := filepath.Join(bp, config.SnapshotsDir, name, config.MetadataFile)
	var meta SnapshotMetadata
	if err := ReadMetadata(metaPath, &meta); err != nil {
		if os.IsNotExist(err) {
			return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("snapshot %q not found", name)}
		}
		return nil, &StorageError{Code: ErrMetadata, Message: fmt.Sprintf("snapshot %q: failed to read metadata: %v", name, err)}
	}
	return &meta, nil
}

func (s *Storage) DeleteSnapshot(ctx context.Context, tenant, name string) error {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	snapDir := filepath.Join(bp, config.SnapshotsDir, name)
	if _, err := os.Stat(snapDir); os.IsNotExist(err) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("snapshot %q not found", name)}
	}

	dataDir := filepath.Join(snapDir, config.DataDir)
	if err := s.btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
		log.Error().Err(err).Msg("failed to delete snapshot subvolume")
		return fmt.Errorf("btrfs subvolume delete failed: %w", err)
	}

	if err := os.RemoveAll(snapDir); err != nil {
		log.Error().Err(err).Msg("failed to remove snapshot directory")
		return fmt.Errorf("failed to remove snapshot directory: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Msg("snapshot deleted")
	return nil
}
