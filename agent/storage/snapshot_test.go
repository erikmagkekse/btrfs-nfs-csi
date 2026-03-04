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

// --- TestCreateSnapshot ---

func TestCreateSnapshot(t *testing.T) {
	ctx := context.Background()

	// setupSrcVol creates a source volume with data/ dir and metadata.
	setupSrcVol := func(t *testing.T, bp, name string) {
		t.Helper()
		volDir := filepath.Join(bp, name)
		dataDir := filepath.Join(volDir, config.DataDir)
		require.NoError(t, os.MkdirAll(dataDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: name, Path: volDir, SizeBytes: 1024})
	}

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name  string
			req   SnapshotCreateRequest
			setup bool
			code  string
		}{
			{name: "invalid_name", req: SnapshotCreateRequest{Name: "bad!", Volume: "vol"}, code: ErrInvalid},
			{name: "invalid_volume", req: SnapshotCreateRequest{Name: "snap", Volume: "bad!"}, code: ErrInvalid},
			{name: "source_not_found", req: SnapshotCreateRequest{Name: "snap", Volume: "nonexistent"}, code: ErrNotFound},
			{name: "already_exists", req: SnapshotCreateRequest{Name: "existing", Volume: "srcvol"}, setup: true, code: ErrAlreadyExists},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, bp, _, _ := newTestStorage(t)
				if tt.setup || tt.req.Volume == "srcvol" {
					setupSrcVol(t, bp, "srcvol")
				}
				if tt.name == "already_exists" {
					snapDir := filepath.Join(bp, config.SnapshotsDir, "existing")
					require.NoError(t, os.MkdirAll(snapDir, 0o755))
				}
				_, err := s.CreateSnapshot(ctx, "test", tt.req)
				requireStorageError(t, err, tt.code)
			})
		}
	})

	t.Run("success", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupSrcVol(t, bp, "srcvol")

		meta, err := s.CreateSnapshot(ctx, "test", SnapshotCreateRequest{
			Name: "mysnap", Volume: "srcvol",
		})
		require.NoError(t, err, "CreateSnapshot")
		assert.Equal(t, "mysnap", meta.Name)
		assert.Equal(t, "srcvol", meta.Volume)
		assert.True(t, meta.ReadOnly, "snapshot should be readonly")
		assert.Equal(t, uint64(1024), meta.SizeBytes)
		assert.False(t, meta.CreatedAt.IsZero(), "CreatedAt should be set")

		snapDir := filepath.Join(bp, config.SnapshotsDir, "mysnap")
		var ondisk SnapshotMetadata
		require.NoError(t, ReadMetadata(filepath.Join(snapDir, config.MetadataFile), &ondisk))
		assert.Equal(t, "mysnap", ondisk.Name)
		assert.True(t, ondisk.ReadOnly, "on-disk snapshot should be readonly")

		// btrfs snapshot called with -r (readonly) flag
		srcData := filepath.Join(bp, "srcvol", config.DataDir)
		dstData := filepath.Join(snapDir, config.DataDir)
		require.Len(t, runner.Calls, 1, "expected exactly 1 btrfs call")
		assert.Equal(t, []string{"subvolume", "snapshot", "-r", srcData, dstData}, runner.Calls[0])
	})

	t.Run("btrfs_fails_cleanup", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("snapshot error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)
		setupSrcVol(t, bp, "srcvol")

		_, err := s.CreateSnapshot(ctx, "test", SnapshotCreateRequest{
			Name: "failsnap", Volume: "srcvol",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs snapshot failed")

		snapDir := filepath.Join(bp, config.SnapshotsDir, "failsnap")
		_, statErr := os.Stat(snapDir)
		assert.True(t, os.IsNotExist(statErr), "snapDir should be cleaned up after failure")
	})
}

// --- TestListSnapshots ---

func TestListSnapshots(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		snaps, err := s.ListSnapshots("test", "")
		require.NoError(t, err, "ListSnapshots")
		assert.Empty(t, snaps)
	})

	t.Run("multiple", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		snapBase := filepath.Join(bp, config.SnapshotsDir)
		for _, name := range []string{"snap1", "snap2"} {
			dir := filepath.Join(snapBase, name)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			writeSnapshotMetadata(t, dir, SnapshotMetadata{Name: name, Volume: "vol1"})
		}

		snaps, err := s.ListSnapshots("test", "")
		require.NoError(t, err, "ListSnapshots")
		assert.Len(t, snaps, 2)
	})

	t.Run("filter_by_volume", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		snapBase := filepath.Join(bp, config.SnapshotsDir)
		for _, pair := range [][2]string{{"snap-a", "vol1"}, {"snap-b", "vol2"}, {"snap-c", "vol1"}} {
			dir := filepath.Join(snapBase, pair[0])
			require.NoError(t, os.MkdirAll(dir, 0o755))
			writeSnapshotMetadata(t, dir, SnapshotMetadata{Name: pair[0], Volume: pair[1]})
		}

		snaps, err := s.ListSnapshots("test", "vol1")
		require.NoError(t, err, "ListSnapshots(vol1)")
		assert.Len(t, snaps, 2, "should only return snapshots for vol1")
		for _, sn := range snaps {
			assert.Equal(t, "vol1", sn.Volume, "all snapshots should be for vol1")
		}
	})

	t.Run("corrupt_skipped", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		snapBase := filepath.Join(bp, config.SnapshotsDir)

		goodDir := filepath.Join(snapBase, "good")
		require.NoError(t, os.MkdirAll(goodDir, 0o755))
		writeSnapshotMetadata(t, goodDir, SnapshotMetadata{Name: "good", Volume: "vol1"})

		badDir := filepath.Join(snapBase, "bad")
		require.NoError(t, os.MkdirAll(badDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(badDir, config.MetadataFile), []byte("{broken"), 0o644))

		require.NoError(t, os.WriteFile(filepath.Join(snapBase, "afile"), []byte("x"), 0o644))

		snaps, err := s.ListSnapshots("test", "")
		require.NoError(t, err, "ListSnapshots")
		require.Len(t, snaps, 1, "only valid snapshot should be returned")
		assert.Equal(t, "good", snaps[0].Name)
	})

	t.Run("no_snapshots_dir", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)
		require.NoError(t, os.RemoveAll(filepath.Join(bp, config.SnapshotsDir)))

		snaps, err := s.ListSnapshots("test", "")
		require.NoError(t, err, "ListSnapshots without snapshots dir")
		assert.Nil(t, snaps)
	})
}

// --- TestGetSnapshot ---

func TestGetSnapshot(t *testing.T) {
	s, bp, _, _ := newTestStorage(t)

	snapDir := filepath.Join(bp, config.SnapshotsDir, "mysnap")
	require.NoError(t, os.MkdirAll(snapDir, 0o755))
	writeSnapshotMetadata(t, snapDir, SnapshotMetadata{Name: "mysnap", Volume: "vol1", ReadOnly: true})

	tests := []struct {
		name   string
		snap   string
		code   string
		expect string
	}{
		{name: "found", snap: "mysnap", expect: "mysnap"},
		{name: "not_found", snap: "nonexistent", code: ErrNotFound},
		{name: "invalid_name", snap: "bad name!", code: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := s.GetSnapshot("test", tt.snap)
			if tt.code != "" {
				requireStorageError(t, err, tt.code)
				return
			}
			require.NoError(t, err, "GetSnapshot")
			assert.Equal(t, tt.expect, meta.Name)
			assert.True(t, meta.ReadOnly, "snapshot should be readonly")
		})
	}
}

// --- TestDeleteSnapshot ---

func TestDeleteSnapshot(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)

		snapDir := filepath.Join(bp, config.SnapshotsDir, "mysnap")
		require.NoError(t, os.MkdirAll(snapDir, 0o755))
		writeSnapshotMetadata(t, snapDir, SnapshotMetadata{Name: "mysnap"})

		err := s.DeleteSnapshot(ctx, "test", "mysnap")
		require.NoError(t, err, "DeleteSnapshot")

		_, statErr := os.Stat(snapDir)
		assert.True(t, os.IsNotExist(statErr), "snapDir should be removed")

		dataDir := filepath.Join(snapDir, config.DataDir)
		require.Len(t, runner.Calls, 1, "expected subvolume delete call")
		assert.Equal(t, []string{"subvolume", "delete", dataDir}, runner.Calls[0])
	})

	t.Run("not_found", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		err := s.DeleteSnapshot(ctx, "test", "nonexistent")
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("subvol_delete_fails", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("subvol error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)

		snapDir := filepath.Join(bp, config.SnapshotsDir, "mysnap")
		require.NoError(t, os.MkdirAll(snapDir, 0o755))
		writeSnapshotMetadata(t, snapDir, SnapshotMetadata{Name: "mysnap"})

		err := s.DeleteSnapshot(ctx, "test", "mysnap")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs subvolume delete failed")

		_, statErr := os.Stat(snapDir)
		assert.False(t, os.IsNotExist(statErr), "snapDir should still exist when subvol delete fails")
	})
}
