package storage

import (
	"context"
	"fmt"
	"hash/crc32"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

func (s *Storage) CreateVolumeExport(ctx context.Context, tenant, name, client string, labels map[string]string) error {
	if _, err := s.tenantPath(tenant); err != nil {
		return err
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}
	if err := validateClientIP(client); err != nil {
		return err
	}
	if err := config.ValidateLabels(labels); err != nil {
		return err
	}
	if err := requireImmutableLabels(s.immutableLabelKeys, labels); err != nil {
		return err
	}

	// Lock spans metadata+kernel export so a concurrent unexport can't interleave.
	release, err := s.volumes.Lock(ctx, tenant, name)
	if err != nil {
		return &StorageError{Code: ErrBusy, Message: fmt.Sprintf("lock contention for volume %q: %v", name, err)}
	}
	defer release()

	volDir, err := s.volumes.Dir(tenant, name)
	if err != nil {
		return &StorageError{Code: ErrInvalid, Message: err.Error()}
	}

	var firstRef bool
	updated, err := s.volumes.UpdateLocked(tenant, name, func(meta *VolumeMetadata) {
		now := time.Now().UTC()
		meta.LastAttachAt = &now
		meta.UpdatedAt = now
		if hasExport(meta.Exports, client, labels) {
			return
		}
		firstRef = exportsForIP(meta.Exports, client) == 0
		meta.Exports = append(meta.Exports, ExportMetadata{IP: client, Labels: labels, CreatedAt: now})
	})
	if err != nil {
		if os.IsNotExist(err) {
			return &StorageError{Code: ErrNotFound, Message: fmt.Sprintf("volume %q not found", name)}
		}
		log.Error().Err(err).Msg("failed to persist client in metadata")
		return fmt.Errorf("failed to persist client in metadata: %w", err)
	}

	if firstRef {
		if err := s.exporter.Export(ctx, volDir, client, updated.exportFSID(client, volDir)); err != nil {
			log.Error().Err(err).Str("name", name).Str("client", client).Msg("failed to export, reconciler will retry")
			return &StorageError{Code: ErrInternal, Message: "nfs export failed"}
		}
	}

	log.Ctx(ctx).Info().Str("name", name).Str("client", client).Msg("NFS export added")
	return nil
}

func (s *Storage) DeleteVolumeExport(ctx context.Context, tenant, name, client string, labels map[string]string) error {
	if _, err := s.tenantPath(tenant); err != nil {
		return err
	}
	if err := config.ValidateName(name); err != nil {
		return err
	}
	if err := validateClientIP(client); err != nil {
		return err
	}
	if err := config.ValidateLabels(labels); err != nil {
		return err
	}

	release, err := s.volumes.Lock(ctx, tenant, name)
	if err != nil {
		return &StorageError{Code: ErrBusy, Message: fmt.Sprintf("lock contention for volume %q: %v", name, err)}
	}
	defer release()

	volDir, err := s.volumes.Dir(tenant, name)
	if err != nil {
		return &StorageError{Code: ErrInvalid, Message: err.Error()}
	}

	var lastRef bool
	if _, err := s.volumes.UpdateLocked(tenant, name, func(meta *VolumeMetadata) {
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
			return &StorageError{Code: ErrInternal, Message: "nfs unexport failed"}
		}
	}

	log.Ctx(ctx).Info().Str("name", name).Str("client", client).Msg("NFS export removed")
	return nil
}

func (s *Storage) ListVolumeExports(tenant string) ([]ExportEntry, error) {
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
	return entries, nil
}

func uniqueExportIPs(clients []ExportMetadata) []string {
	seen := map[string]struct{}{}
	for _, c := range clients {
		seen[c.IP] = struct{}{}
	}
	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	slices.Sort(ips)
	return ips
}

func CountUniqueExportIPs(clients []ExportMetadata) int {
	seen := map[string]struct{}{}
	for _, c := range clients {
		seen[c.IP] = struct{}{}
	}
	return len(seen)
}

// clientLabels strips the agent-owned fsid label, which stored entries
// carry and a client never sends.
func clientLabels(labels map[string]string) map[string]string {
	if _, ok := labels[config.LabelExportFSIDCRC32]; !ok {
		return labels
	}
	out := maps.Clone(labels)
	delete(out, config.LabelExportFSIDCRC32)
	return out
}

// hasExport reports whether an entry for ip with these labels exists, fsid label aside.
func hasExport(clients []ExportMetadata, ip string, labels map[string]string) bool {
	for _, c := range clients {
		if c.IP == ip && maps.Equal(clientLabels(c.Labels), labels) {
			return true
		}
	}
	return false
}

func exportsForIP(clients []ExportMetadata, ip string) int {
	n := 0
	for _, c := range clients {
		if c.IP == ip {
			n++
		}
	}
	return n
}

// labelsContain reports whether stored contains all key-value pairs from match.
func labelsContain(stored, match map[string]string) bool {
	for k, v := range match {
		if stored[k] != v {
			return false
		}
	}
	return true
}

// exportFSID for one client: the subvolume UUID, unless its own export entries
// were created with the path-derived fsid, or the volume has no UUID. nfsd keys
// exports on (client, path), so each client can hold its own fsid.
func (m *VolumeMetadata) exportFSID(ip, volDir string) string {
	marked := func(c ExportMetadata) bool {
		return c.IP == ip && c.Labels[config.LabelExportFSIDCRC32] == "true"
	}
	if m.UUID == "" || slices.ContainsFunc(m.Exports, marked) {
		return pathFSID(volDir)
	}
	return strings.ReplaceAll(m.UUID, "-", "")
}

// pathFSID is the 31-bit numeric fsid used before subvolume UUIDs were recorded.
// nfsd keeps numeric and UUID fsids apart, they cannot collide.
func pathFSID(path string) string {
	fsid := crc32.ChecksumIEEE([]byte(path)) & 0x7FFFFFFF
	if fsid == 0 {
		fsid = 1
	}
	return strconv.FormatUint(uint64(fsid), 10)
}
