package storage

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateSnapshot(ctx context.Context, tenant string, req SnapshotCreateRequest) (*SnapshotMetadata, error) {
	if _, err := s.tenantPath(tenant); err != nil {
		return nil, err
	}

	// validation
	if err := config.ValidateName(req.Name); err != nil {
		return nil, err
	}
	if err := config.ValidateName(req.Volume); err != nil {
		return nil, err
	}
	if err := config.ValidateLabels(req.Labels); err != nil {
		return nil, err
	}
	if err := requireImmutableLabels(s.immutableLabelKeys, req.Labels); err != nil {
		return nil, err
	}
	volMeta, err := s.volumes.Get(tenant, req.Volume)
	if err != nil {
		return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("source volume %q not found", req.Volume)}
	}
	srcData := s.volumes.DataPath(tenant, req.Volume)

	if existing, err := s.snapshots.Get(tenant, req.Name); err == nil {
		return existing, &StorageError{Code: ErrAlreadyExists, Message: fmt.Sprintf("snapshot %q already exists", req.Name)}
	}
	snapDir := s.snapshots.Dir(tenant, req.Name)

	// operations
	if err := os.MkdirAll(snapDir, s.defaultDirMode); err != nil {
		log.Error().Err(err).Msg("failed to create snapshot directory")
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	dstData := s.snapshots.DataPath(tenant, req.Name)
	if err := s.btrfs.SubvolumeSnapshot(ctx, srcData, dstData, true); err != nil {
		_ = os.RemoveAll(snapDir)
		log.Error().Err(err).Msg("failed to create snapshot")
		return nil, fmt.Errorf("btrfs snapshot failed: %w", err)
	}

	now := time.Now().UTC()
	meta := SnapshotMetadata{
		Name:        req.Name,
		Volume:      req.Volume,
		Path:        snapDir,
		SizeBytes:   volMeta.SizeBytes,
		QuotaBytes:  volMeta.QuotaBytes,
		NoCOW:       volMeta.NoCOW,
		Compression: volMeta.Compression,
		UID:         volMeta.UID,
		GID:         volMeta.GID,
		Mode:        volMeta.Mode,
		Labels:      req.Labels,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.snapshots.Store(tenant, req.Name, &meta); err != nil {
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
	if _, err := s.tenantPath(tenant); err != nil {
		return nil, err
	}
	var snaps []SnapshotMetadata
	s.snapshots.Range(func(t, _ string, val *SnapshotMetadata) bool {
		if t == tenant {
			if volume == "" || val.Volume == volume {
				snaps = append(snaps, *val)
			}
		}
		return true
	})
	log.Debug().Str("tenant", tenant).Int("count", len(snaps)).Msg("snapshots listed")
	return snaps, nil
}

func (s *Storage) GetSnapshot(tenant, name string) (*SnapshotMetadata, error) {
	if _, err := s.tenantPath(tenant); err != nil {
		return nil, err
	}
	if err := config.ValidateName(name); err != nil {
		return nil, err
	}
	m, err := s.snapshots.Get(tenant, name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("snapshot %q not found", name)}
		}
		return nil, &StorageError{Code: ErrMetadata, Message: fmt.Sprintf("snapshot %q: failed to read metadata: %v", name, err)}
	}
	cp := *m
	return &cp, nil
}

func (s *Storage) DeleteSnapshot(ctx context.Context, tenant, name string) error {
	if _, err := s.tenantPath(tenant); err != nil {
		return err
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}

	if !s.snapshots.Exists(tenant, name) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("snapshot %q not found", name)}
	}

	dataDir := s.snapshots.DataPath(tenant, name)
	if err := s.btrfs.SubvolumeDelete(ctx, dataDir); err != nil {
		log.Error().Err(err).Msg("failed to delete snapshot subvolume")
		return fmt.Errorf("btrfs subvolume delete failed: %w", err)
	}

	s.snapshots.Delete(tenant, name)

	snapDir := s.snapshots.Dir(tenant, name)
	if err := os.RemoveAll(snapDir); err != nil {
		log.Error().Err(err).Msg("failed to remove snapshot directory")
		return fmt.Errorf("failed to remove snapshot directory: %w", err)
	}
	log.Info().Str("tenant", tenant).Str("name", name).Msg("snapshot deleted")
	return nil
}
