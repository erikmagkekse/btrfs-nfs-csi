package storage

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateVolumeExport(ctx context.Context, tenant, name, client string, labels map[string]string) error {
	if _, err := s.tenantPath(tenant); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if err := validateLabels(labels); err != nil {
		return err
	}
	if err := requireImmutableLabels(s.immutableLabelKeys,labels); err != nil {
		return err
	}

	volDir := s.volumes.Dir(tenant, name)

	// metadata first - if export fails, reconciler will re-export
	var firstRef bool
	if _, err := s.volumes.Update(tenant, name, func(meta *VolumeMetadata) {
		now := time.Now().UTC()
		meta.LastAttachAt = &now
		meta.UpdatedAt = now
		if hasExport(meta.Exports, client, labels) {
			return
		}
		firstRef = exportsForIP(meta.Exports, client) == 0
		meta.Exports = append(meta.Exports, ExportMetadata{IP: client, Labels: labels, CreatedAt: now})
	}); err != nil {
		if os.IsNotExist(err) {
			return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
		}
		log.Error().Err(err).Msg("failed to persist client in metadata")
		return fmt.Errorf("failed to persist client in metadata: %w", err)
	}

	if firstRef {
		if err := s.exporter.Export(ctx, volDir, client); err != nil {
			log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to export, reconciler will retry")
			return fmt.Errorf("nfs export failed: %w", err)
		}
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export added")
	return nil
}

func (s *Storage) DeleteVolumeExport(ctx context.Context, tenant, name, client string, labels map[string]string) error {
	if _, err := s.tenantPath(tenant); err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	if err := validateLabels(labels); err != nil {
		return err
	}

	volDir := s.volumes.Dir(tenant, name)

	// metadata first - if unexport fails, reconciler will clean up
	var lastRef bool
	if _, err := s.volumes.Update(tenant, name, func(meta *VolumeMetadata) {
		var removed bool
		filtered := meta.Exports[:0]
		for _, c := range meta.Exports {
			if c.IP != client {
				filtered = append(filtered, c)
				continue
			}
			// labels == nil: remove all refs for this IP
			// labels != nil: remove only matching entry
			if labels != nil && !labelsContain(c.Labels, labels) {
				filtered = append(filtered, c)
			} else {
				removed = true
			}
		}
		meta.Exports = filtered
		lastRef = removed && exportsForIP(filtered, client) == 0
		meta.UpdatedAt = time.Now().UTC()
	}); err != nil {
		if os.IsNotExist(err) {
			return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
		}
		log.Error().Err(err).Msg("failed to update client list in metadata")
		return fmt.Errorf("failed to update client list in metadata: %w", err)
	}

	if lastRef {
		if err := s.exporter.Unexport(ctx, volDir, client); err != nil {
			log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to unexport, reconciler will clean up")
			return fmt.Errorf("nfs unexport failed: %w", err)
		}
	}

	log.Info().Str("tenant", tenant).Str("name", name).Str("client", client).Msg("NFS export removed")
	return nil
}

func (s *Storage) ListVolumeExportsPaginated(tenant, after string, limit int) (*PaginatedResult[ExportEntry], error) {
	if _, err := s.tenantPath(tenant); err != nil {
		return nil, err
	}

	var entries []ExportEntry
	s.volumes.Range(func(t, name string, meta *VolumeMetadata) bool {
		if t != tenant {
			return true
		}
		for _, c := range meta.Exports {
			entries = append(entries, ExportEntry{
				Name:      name,
				Client:    c.IP,
				Labels:    c.Labels,
				CreatedAt: c.CreatedAt,
			})
		}
		return true
	})
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return paginateSlice(entries, func(e ExportEntry) string { return e.Name + "|" + e.Client }, after, limit), nil
}
