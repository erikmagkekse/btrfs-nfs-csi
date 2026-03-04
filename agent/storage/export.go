package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"

	"github.com/rs/zerolog/log"
)

func (s *Storage) ExportVolume(ctx context.Context, tenant, name, client string) error {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	volDir := filepath.Join(bp, name)
	if _, err := os.Stat(volDir); os.IsNotExist(err) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
	}

	// metadata first - if export fails, reconciler will re-export
	metaPath := filepath.Join(volDir, config.MetadataFile)
	if err := UpdateMetadata(metaPath, func(meta *VolumeMetadata) {
		found := false
		for _, c := range meta.Clients {
			if c == client {
				found = true
				break
			}
		}
		now := time.Now().UTC()
		meta.LastAttachAt = &now
		meta.UpdatedAt = now
		if !found {
			meta.Clients = append(meta.Clients, client)
		}
	}); err != nil {
		log.Error().Err(err).Msg("failed to persist client in metadata")
		return fmt.Errorf("failed to persist client in metadata: %w", err)
	}

	if err := s.exporter.Export(ctx, volDir, client); err != nil {
		log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to export, reconciler will retry")
		return fmt.Errorf("nfs export failed: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export added")
	return nil
}

func (s *Storage) UnexportVolume(ctx context.Context, tenant, name, client string) error {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}

	volDir := filepath.Join(bp, name)
	if _, err := os.Stat(volDir); os.IsNotExist(err) {
		return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
	}

	// metadata first - if unexport fails, reconciler will clean up
	metaPath := filepath.Join(volDir, config.MetadataFile)
	if err := UpdateMetadata(metaPath, func(meta *VolumeMetadata) {
		filtered := meta.Clients[:0]
		for _, c := range meta.Clients {
			if c != client {
				filtered = append(filtered, c)
			}
		}
		meta.Clients = filtered
		meta.UpdatedAt = time.Now().UTC()
	}); err != nil {
		log.Error().Err(err).Msg("failed to update client list in metadata")
		return fmt.Errorf("failed to update client list in metadata: %w", err)
	}

	if err := s.exporter.Unexport(ctx, volDir, client); err != nil {
		log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to unexport, reconciler will clean up")
		return fmt.Errorf("nfs unexport failed: %w", err)
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export removed")
	return nil
}

func (s *Storage) ListExports(ctx context.Context, tenant string) ([]ExportEntry, error) {
	bp, err := s.tenantPath(tenant)
	if err != nil {
		return nil, err
	}

	exports, err := s.exporter.ListExports(ctx)
	if err != nil {
		return nil, fmt.Errorf("list exports failed: %w", err)
	}

	var entries []ExportEntry
	for _, e := range exports {
		if strings.HasPrefix(e.Path, bp+"/") {
			entries = append(entries, ExportEntry{Path: e.Path, Client: e.Client})
		}
	}
	log.Debug().Str("tenant", tenant).Int("count", len(entries)).Msg("exports listed")
	return entries, nil
}
