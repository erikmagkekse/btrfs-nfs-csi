package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestCreateClone ---

func TestCreateClone(t *testing.T) {
	ctx := context.Background()

	// setupSrcSnap creates a source snapshot with data/ dir and metadata,
	// plus the source volume it references.
	setupSrcSnap := func(t *testing.T, s *Storage, bp, name string) {
		t.Helper()
		snapDir := filepath.Join(bp, config.SnapshotsDir, name)
		dataDir := filepath.Join(snapDir, config.DataDir)
		require.NoError(t, os.MkdirAll(dataDir, 0o755))
		writeSnapshotMetadata(t, s, snapDir, SnapshotMetadata{Name: name, Volume: "srcvol", ReadOnly: true})
		seedVolume(t, s, "test", bp, VolumeMetadata{
			Name: "srcvol", SizeBytes: 4096, QuotaBytes: 4096,
			Compression: "zstd", UID: 1000, GID: 1000, Mode: "0755",
		})
	}

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name  string
			req   CloneCreateRequest
			setup bool
			code  string
		}{
			{name: "invalid_name", req: CloneCreateRequest{Name: "bad!", Snapshot: "snap"}, code: ErrInvalid},
			{name: "invalid_snapshot", req: CloneCreateRequest{Name: "clone", Snapshot: "bad!"}, code: ErrInvalid},
			{name: "snapshot_not_found", req: CloneCreateRequest{Name: "clone", Snapshot: "nonexistent"}, code: ErrNotFound},
			{name: "already_exists", req: CloneCreateRequest{Name: "existing", Snapshot: "mysnap"}, setup: true, code: ErrAlreadyExists},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, bp, _, _ := newTestStorage(t)
				if tt.setup || tt.req.Snapshot == "mysnap" {
					setupSrcSnap(t, s, bp, "mysnap")
				}
				if tt.name == "already_exists" {
					seedVolume(t, s, "test", bp, VolumeMetadata{Name: "existing"})
				}
				_, err := s.CreateClone(ctx, "test", tt.req)
				requireStorageError(t, err, tt.code)
			})
		}
	})

	t.Run("success", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupSrcSnap(t, s, bp, "mysnap")

		meta, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "myclone", Snapshot: "mysnap",
		})
		require.NoError(t, err, "CreateClone")
		assert.Equal(t, "myclone", meta.Name)
		assert.Equal(t, filepath.Join(bp, "myclone"), meta.Path)
		assert.False(t, meta.CreatedAt.IsZero(), "CreatedAt should be set")

		var ondisk VolumeMetadata
		readTestJSON(t, filepath.Join(bp, "myclone", config.MetadataFile), &ondisk)
		assert.Equal(t, "myclone", ondisk.Name, "on-disk metadata should match")

		// btrfs snapshot called WITHOUT -r flag (writable clone)
		srcData := filepath.Join(bp, config.SnapshotsDir, "mysnap", config.DataDir)
		dstData := filepath.Join(bp, "myclone", config.DataDir)
		require.Len(t, runner.Calls, 1, "expected exactly 1 btrfs call")
		assert.Equal(t, []string{"subvolume", "snapshot", srcData, dstData}, runner.Calls[0])
	})

	t.Run("btrfs_fails_cleanup", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("snapshot error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)
		setupSrcSnap(t, s, bp, "mysnap")

		_, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "failclone", Snapshot: "mysnap",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs snapshot failed")

		cloneDir := filepath.Join(bp, "failclone")
		_, statErr := os.Stat(cloneDir)
		assert.True(t, os.IsNotExist(statErr), "cloneDir should be cleaned up after failure")
	})

	t.Run("copies_source_volume_metadata", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)
		setupSrcSnap(t, s, bp, "mysnap")

		meta, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "metaclone", Snapshot: "mysnap",
		})
		require.NoError(t, err, "CreateClone")
		assert.Equal(t, uint64(4096), meta.SizeBytes, "SizeBytes should be copied from source volume")
		assert.Equal(t, uint64(4096), meta.QuotaBytes, "QuotaBytes should be copied from source volume")
		assert.Equal(t, "zstd", meta.Compression, "Compression should be copied from source volume")
		assert.Equal(t, 1000, meta.UID, "UID should be copied from source volume")
		assert.Equal(t, 1000, meta.GID, "GID should be copied from source volume")
		assert.Equal(t, "0755", meta.Mode, "Mode should be copied from source volume")

		// verify on-disk metadata also has the fields
		var ondisk VolumeMetadata
		readTestJSON(t, filepath.Join(bp, "metaclone", config.MetadataFile), &ondisk)
		assert.Equal(t, uint64(4096), ondisk.SizeBytes)
		assert.Equal(t, "zstd", ondisk.Compression)
		assert.Equal(t, 1000, ondisk.UID)
	})

	t.Run("applies_qgroup_limit_when_quota_enabled", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		s.quotaEnabled = true
		setupSrcSnap(t, s, bp, "mysnap")

		meta, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "quotaclone", Snapshot: "mysnap",
		})
		require.NoError(t, err, "CreateClone")
		assert.Equal(t, uint64(4096), meta.QuotaBytes)

		dstData := filepath.Join(bp, "quotaclone", config.DataDir)
		require.Len(t, runner.Calls, 2, "expected snapshot + qgroup limit calls")
		assert.Equal(t, []string{"qgroup", "limit", "4096", dstData}, runner.Calls[1])
	})

	t.Run("source_volume_not_found", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		// Create snapshot pointing to a non-existent volume
		snapDir := filepath.Join(bp, config.SnapshotsDir, "orphansnap")
		dataDir := filepath.Join(snapDir, config.DataDir)
		require.NoError(t, os.MkdirAll(dataDir, 0o755))
		writeSnapshotMetadata(t, s, snapDir, SnapshotMetadata{Name: "orphansnap", Volume: "missing", ReadOnly: true})

		_, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "clone", Snapshot: "orphansnap",
		})
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("invalid_tenant", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		_, err := s.CreateClone(ctx, "bad tenant!", CloneCreateRequest{
			Name: "clone", Snapshot: "snap",
		})
		requireStorageError(t, err, ErrInvalid)
	})
}
