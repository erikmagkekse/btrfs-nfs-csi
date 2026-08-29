package storage

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

// Not a migration framework. As soon as the metadata format changes more than
// once, entries need a schema version so migrations can be ordered and skipped.
// This is something that should be added in the future.

// Handling of older metadata, with the version that introduced it:
//
//	v0.10.0  model.go, UnmarshalJSON              legacy clients IP list, converted on read
//	v0.10.0  utils.go, protectImmutableLabels     allows the first created-by on old volumes
//	v0.11.0  api/v1/policy.go, checkOwnership     a missing created-by counts as unowned
//	v0.12.0  api/v1/policy.go, preserveCreatedBy  an update supplying created-by adopts
//	v0.12.0  MigrateSubvolumeUUIDs                records the UUID, pins the crc32 fsid
//	v0.12.0  fsid.go, pathFSID                    the pre-UUID fsid, alive while a client stays pinned

type migrationEntry struct{ tenant, name string }

// MigrateSubvolumeUUIDs records the subvolume UUID on volumes and snapshots that
// predate the field. Clients attached at that point keep the path-derived fsid
// they mounted, marked on their export entries with config.LabelExportFSIDCRC32.
//
// Call once at startup, before the workers: the reconciler must not re-export
// anything before the fsid pins are in place.
//
// Entries missing from the subvolume list keep an empty UUID, stay on the crc32
// fsid and are retried on the next start. Rewriting an entry also converts the
// legacy clients list, since the whole metadata file is written back.
func (s *Storage) MigrateSubvolumeUUIDs(ctx context.Context) {
	start := time.Now()
	var vols, snaps []migrationEntry
	s.volumes.Range(func(t, name string, m *VolumeMetadata) bool {
		if m.UUID == "" {
			vols = append(vols, migrationEntry{t, name})
		}
		return true
	})
	s.snapshots.Range(func(t, name string, m *SnapshotMetadata) bool {
		if m.UUID == "" {
			snaps = append(snaps, migrationEntry{t, name})
		}
		return true
	})
	if len(vols) == 0 && len(snaps) == 0 {
		return
	}

	uuids, err := s.btrfs.SubvolumeUUIDs(ctx, s.mountPoint) // one list for the whole filesystem
	if err != nil {
		log.Error().Err(err).Msg("migration: failed to list subvolume uuids")
		return
	}
	var nVols, nSnaps int
	for _, e := range vols {
		dataDir, err := s.volumes.DataPath(e.tenant, e.name)
		if err != nil {
			log.Error().Err(err).Str("tenant", e.tenant).Str("volume", e.name).Msg("migration: invalid volume path")
			continue
		}
		uuid := uuids[s.relToMount(dataDir)]
		if uuid == "" {
			log.Error().Str("tenant", e.tenant).Str("volume", e.name).Msg("migration: subvolume not listed, uuid not recorded")
			continue
		}
		if _, err := s.volumes.Update(e.tenant, e.name, func(m *VolumeMetadata) {
			m.UUID = uuid
			// Mark what these clients mounted. Clone, UpdateLocked shares the backing array.
			m.Exports = slices.Clone(m.Exports)
			for i := range m.Exports {
				labels := maps.Clone(m.Exports[i].Labels)
				if labels == nil {
					labels = map[string]string{}
				}
				labels[config.LabelExportFSIDCRC32] = "true"
				m.Exports[i].Labels = labels
			}
		}); err != nil {
			log.Error().Err(err).Str("tenant", e.tenant).Str("volume", e.name).Msg("migration: uuid not recorded")
			continue
		}
		nVols++
	}

	for _, e := range snaps {
		dataDir, err := s.snapshots.DataPath(e.tenant, e.name)
		if err != nil {
			log.Error().Err(err).Str("tenant", e.tenant).Str("snapshot", e.name).Msg("migration: invalid snapshot path")
			continue
		}
		uuid := uuids[s.relToMount(dataDir)]
		if uuid == "" {
			log.Error().Str("tenant", e.tenant).Str("snapshot", e.name).Msg("migration: subvolume not listed, uuid not recorded")
			continue
		}
		if _, err := s.snapshots.Update(e.tenant, e.name, func(m *SnapshotMetadata) { m.UUID = uuid }); err != nil {
			log.Error().Err(err).Str("tenant", e.tenant).Str("snapshot", e.name).Msg("migration: uuid not recorded")
			continue
		}
		nSnaps++
	}

	log.Ctx(ctx).Info().Int("volumes", nVols).Int("snapshots", nSnaps).
		Str("took", time.Since(start).String()).Msg("migration: uuids recorded")
}
