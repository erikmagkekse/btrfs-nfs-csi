package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestTenantPath ---

func TestTenantPath(t *testing.T) {
	s, bp, _, _ := newTestStorage(t)

	tests := []struct {
		name   string
		tenant string
		want   string
		code   string
	}{
		{name: "valid", tenant: "test", want: bp},
		{name: "invalid_name", tenant: "bad name!", code: ErrInvalid},
		{name: "not_found", tenant: "nonexistent", code: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := s.tenantPath(tt.tenant)
			if tt.code != "" {
				requireStorageError(t, err, tt.code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, path)
		})
	}
}

// --- TestLoadCache ---

func TestLoadCache(t *testing.T) {
	t.Run("skips_phantom_volume_without_data_dir", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		// Create a volume directory with metadata but no data/ subdir
		phantomDir := filepath.Join(bp, "phantom")
		require.NoError(t, os.MkdirAll(phantomDir, 0o755))
		data, err := json.MarshalIndent(VolumeMetadata{Name: "phantom", SizeBytes: 1024}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(phantomDir, config.MetadataFile), data, 0o644))

		// Create a valid volume directory with both metadata and data/
		validDir := filepath.Join(bp, "valid")
		require.NoError(t, os.MkdirAll(filepath.Join(validDir, config.DataDir), 0o755))
		data, err = json.MarshalIndent(VolumeMetadata{Name: "valid", SizeBytes: 2048}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(validDir, config.MetadataFile), data, 0o644))

		s.loadCache()

		assert.False(t, s.volumes.Exists("test", "phantom"), "phantom volume should not be loaded")
		assert.True(t, s.volumes.Exists("test", "valid"), "valid volume should be loaded")
	})

	t.Run("skips_phantom_snapshot_without_data_dir", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		// Create a snapshot directory with metadata but no data/ subdir
		phantomDir := filepath.Join(bp, config.SnapshotsDir, "phantomsnap")
		require.NoError(t, os.MkdirAll(phantomDir, 0o755))
		data, err := json.MarshalIndent(SnapshotMetadata{Name: "phantomsnap", Volume: "vol"}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(phantomDir, config.MetadataFile), data, 0o644))

		// Create a valid snapshot with both metadata and data/
		validDir := filepath.Join(bp, config.SnapshotsDir, "validsnap")
		require.NoError(t, os.MkdirAll(filepath.Join(validDir, config.DataDir), 0o755))
		data, err = json.MarshalIndent(SnapshotMetadata{Name: "validsnap", Volume: "vol"}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(validDir, config.MetadataFile), data, 0o644))

		s.loadCache()

		assert.False(t, s.snapshots.Exists("test", "phantomsnap"), "phantom snapshot should not be loaded")
		assert.True(t, s.snapshots.Exists("test", "validsnap"), "valid snapshot should be loaded")
	})
}

// --- TestStats ---

func TestStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		stats, err := s.Stats("test")
		require.NoError(t, err, "Stats")
		assert.NotZero(t, stats.TotalBytes, "TotalBytes should be > 0")
		assert.Equal(t, stats.TotalBytes, stats.UsedBytes+stats.FreeBytes,
			"Total = Used + Free: %d != %d + %d", stats.TotalBytes, stats.UsedBytes, stats.FreeBytes)
	})

	t.Run("invalid_tenant", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		_, err := s.Stats("bad name!")
		requireStorageError(t, err, ErrInvalid)
	})
}
