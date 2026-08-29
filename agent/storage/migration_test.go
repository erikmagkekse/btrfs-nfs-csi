package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateSubvolumeUUIDs(t *testing.T) {
	ctx := context.Background()

	t.Run("exported_volume_gets_uuid_and_pinned_fsid", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		dir := seedVolume(t, s, "test", bp, VolumeMetadata{
			Name:    "oldvol",
			Exports: []ExportMetadata{{IP: "10.0.0.1", Labels: map[string]string{"pv": "a"}}, {IP: "10.0.0.2"}},
		})
		runner.RunFn = subvolumeListRunFn(map[string]string{"test/oldvol/data": testSubvolUUID})

		s.MigrateSubvolumeUUIDs(ctx)

		meta, err := s.GetVolume("test", "oldvol")
		require.NoError(t, err)
		assert.Equal(t, testSubvolUUID, meta.UUID)
		require.Len(t, meta.Exports, 2)
		for _, e := range meta.Exports {
			assert.Equal(t, "true", e.Labels[config.LabelExportFSIDCRC32], "every export entry keeps the crc32 scheme")
		}
		assert.Equal(t, "a", meta.Exports[0].Labels["pv"], "existing labels preserved")

		ondisk := readVolumeMeta(t, dir)
		assert.Equal(t, testSubvolUUID, ondisk.UUID, "uuid persisted")
		assert.Equal(t, "true", ondisk.Exports[1].Labels[config.LabelExportFSIDCRC32], "label persisted")
		assert.Equal(t, pathFSID(dir), ondisk.exportFSID("10.0.0.1", dir), "marked clients keep the path fsid")

		require.Len(t, runner.Calls, 1, "one btrfs call for the whole migration")
		assert.Equal(t, []string{"subvolume", "list", "-u", "-o", s.mountPoint}, runner.Calls[0])

		s.MigrateSubvolumeUUIDs(ctx)
		assert.Len(t, runner.Calls, 1, "second run must not touch btrfs")
	})

	t.Run("unexported_volume_gets_uuid_only", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		seedVolume(t, s, "test", bp, VolumeMetadata{Name: "idlevol"})
		runner.RunFn = subvolumeListRunFn(map[string]string{"test/idlevol/data": testSubvolUUID})

		s.MigrateSubvolumeUUIDs(ctx)

		meta, err := s.GetVolume("test", "idlevol")
		require.NoError(t, err)
		assert.Equal(t, testSubvolUUID, meta.UUID)
		assert.Empty(t, meta.Exports)
	})

	t.Run("nothing_to_do_makes_no_btrfs_call", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		seedVolume(t, s, "test", bp, VolumeMetadata{
			Name: "newvol", UUID: "11111111-2222-4333-8444-555555555555",
			Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})

		s.MigrateSubvolumeUUIDs(ctx)

		meta, err := s.GetVolume("test", "newvol")
		require.NoError(t, err)
		assert.Equal(t, "11111111-2222-4333-8444-555555555555", meta.UUID, "existing uuid must not be replaced")
		assert.NotContains(t, meta.Exports[0].Labels, config.LabelExportFSIDCRC32)
		assert.Empty(t, runner.Calls)
	})

	t.Run("snapshot_gets_uuid", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		seedSnapshot(t, s, "test", bp, SnapshotMetadata{Name: "oldsnap", Volume: "vol"})
		runner.RunFn = subvolumeListRunFn(map[string]string{"test/snapshots/oldsnap/data": testSubvolUUID})

		s.MigrateSubvolumeUUIDs(ctx)

		meta, err := s.GetSnapshot("test", "oldsnap")
		require.NoError(t, err)
		assert.Equal(t, testSubvolUUID, meta.UUID)
	})

	t.Run("missing_from_list_skips_entry", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		seedVolume(t, s, "test", bp, VolumeMetadata{
			Name: "brokenvol", Exports: []ExportMetadata{{IP: "10.0.0.1"}},
		})
		runner.RunFn = subvolumeListRunFn(map[string]string{"test/other/data": testSubvolUUID})

		s.MigrateSubvolumeUUIDs(ctx)

		meta, err := s.GetVolume("test", "brokenvol")
		require.NoError(t, err)
		assert.Empty(t, meta.UUID, "no uuid when the subvolume is not listed")
		assert.NotContains(t, meta.Exports[0].Labels, config.LabelExportFSIDCRC32, "no marker without uuid")
	})

	t.Run("list_error_changes_nothing", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		dir := seedVolume(t, s, "test", bp, VolumeMetadata{Name: "oldvol"})
		runner.RunFn = func([]string) (string, error) { return "", fmt.Errorf("list failed") }

		s.MigrateSubvolumeUUIDs(ctx)

		assert.Empty(t, readVolumeMeta(t, dir).UUID)
	})
}
