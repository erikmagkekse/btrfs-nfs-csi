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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- TestCreateVolume ---

func TestCreateVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name string
			req  VolumeCreateRequest
			code string
		}{
			{name: "empty_name", req: VolumeCreateRequest{SizeBytes: 1024}, code: ErrInvalid},
			{name: "invalid_name", req: VolumeCreateRequest{Name: "bad name!", SizeBytes: 1024}, code: ErrInvalid},
			{name: "name_too_long", req: VolumeCreateRequest{
				Name:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				SizeBytes: 1024,
			}, code: ErrInvalid},
			{name: "zero_size", req: VolumeCreateRequest{Name: "vol", SizeBytes: 0}, code: ErrInvalid},
			{name: "nocow_with_compression", req: VolumeCreateRequest{
				Name: "vol", SizeBytes: 1024, NoCOW: true, Compression: "zstd",
			}, code: ErrInvalid},
			{name: "invalid_compression", req: VolumeCreateRequest{
				Name: "vol", SizeBytes: 1024, Compression: "brotli",
			}, code: ErrInvalid},
			{name: "invalid_compression_level", req: VolumeCreateRequest{
				Name: "vol", SizeBytes: 1024, Compression: "zstd:99",
			}, code: ErrInvalid},
			{name: "invalid_mode", req: VolumeCreateRequest{
				Name: "vol", SizeBytes: 1024, Mode: "nope",
			}, code: ErrInvalid},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, _, _, _ := newTestStorage(t)
				_, err := s.CreateVolume(ctx, "test", tt.req)
				requireStorageError(t, err, tt.code)
			})
		}
	})

	t.Run("nocow_with_compression_none_allowed", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "vol", SizeBytes: 1024, NoCOW: true, Compression: "none",
		})
		require.NoError(t, err, "nocow+compression=none should be allowed")
		assert.True(t, meta.NoCOW)
	})

	t.Run("success_minimal", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "myvol", SizeBytes: 1024 * 1024,
		})
		require.NoError(t, err, "CreateVolume")
		assert.Equal(t, "myvol", meta.Name)
		assert.Equal(t, filepath.Join(bp, "myvol"), meta.Path)
		assert.Equal(t, uint64(1024*1024), meta.SizeBytes)
		assert.Equal(t, uint64(1024*1024), meta.QuotaBytes, "QuotaBytes should default to SizeBytes")
		assert.Equal(t, "2770", meta.Mode, "Mode should default to defaultDataMode")
		assert.False(t, meta.NoCOW)
		assert.Empty(t, meta.Compression)
		assert.False(t, meta.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, meta.UpdatedAt.IsZero(), "UpdatedAt should be set")

		ondisk := readVolumeMeta(t, filepath.Join(bp, "myvol"))
		assert.Equal(t, meta.Name, ondisk.Name, "on-disk metadata should match")

		dataDir := filepath.Join(bp, "myvol", config.DataDir)
		require.Len(t, runner.Calls, 1, "expected exactly 1 btrfs call")
		assert.Equal(t, []string{"subvolume", "create", dataDir}, runner.Calls[0])
	})

	t.Run("success_with_compression", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "compvol", SizeBytes: 2048, Compression: "zstd",
		})
		require.NoError(t, err, "CreateVolume")
		assert.Equal(t, "zstd", meta.Compression)

		dataDir := filepath.Join(bp, "compvol", config.DataDir)
		require.Len(t, runner.Calls, 2, "expected subvolume create + set compression")
		assert.Equal(t, []string{"subvolume", "create", dataDir}, runner.Calls[0])
		assert.Equal(t, []string{"property", "set", dataDir, "compression", "zstd"}, runner.Calls[1])
	})

	t.Run("success_with_nocow", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "cowvol", SizeBytes: 2048, NoCOW: true,
		})
		require.NoError(t, err, "CreateVolume")
		assert.True(t, meta.NoCOW)

		dataDir := filepath.Join(bp, "cowvol", config.DataDir)
		require.Len(t, runner.Calls, 2, "expected subvolume create + chattr")
		assert.Equal(t, []string{"subvolume", "create", dataDir}, runner.Calls[0])
		assert.Equal(t, []string{"+C", dataDir}, runner.Calls[1])
	})

	t.Run("success_with_quota", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		s.quotaEnabled = true

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "quotavol", SizeBytes: 2048, QuotaBytes: 4096,
		})
		require.NoError(t, err, "CreateVolume")
		assert.Equal(t, uint64(4096), meta.QuotaBytes)

		dataDir := filepath.Join(bp, "quotavol", config.DataDir)
		require.Len(t, runner.Calls, 2, "expected subvolume create + qgroup limit")
		assert.Equal(t, []string{"subvolume", "create", dataDir}, runner.Calls[0])
		assert.Equal(t, []string{"qgroup", "limit", "4096", dataDir}, runner.Calls[1])
	})

	t.Run("already_exists", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		volDir := filepath.Join(bp, "existing")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "existing", SizeBytes: 512})

		meta, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "existing", SizeBytes: 1024,
		})
		requireStorageError(t, err, ErrAlreadyExists)
		require.NotNil(t, meta, "should return existing metadata")
		assert.Equal(t, "existing", meta.Name)
		assert.Equal(t, uint64(512), meta.SizeBytes, "should return original size, not requested")
	})

	t.Run("cleanup_on_subvolume_create_failure", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("btrfs error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)

		_, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "failvol", SizeBytes: 1024,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs subvolume create failed")

		_, statErr := os.Stat(filepath.Join(bp, "failvol"))
		assert.True(t, os.IsNotExist(statErr), "volDir should be cleaned up after failure")
	})

	t.Run("cleanup_on_nocow_failure", func(t *testing.T) {
		runner := &utils.MockRunner{
			RunFn: func(args []string) (string, error) {
				if len(args) >= 1 && args[0] == "+C" {
					return "", fmt.Errorf("chattr failed")
				}
				return "", nil
			},
		}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)

		_, err := s.CreateVolume(ctx, "test", VolumeCreateRequest{
			Name: "failnocow", SizeBytes: 1024, NoCOW: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chattr +C failed")

		_, statErr := os.Stat(filepath.Join(bp, "failnocow"))
		assert.True(t, os.IsNotExist(statErr), "volDir should be cleaned up after failure")

		dataDir := filepath.Join(bp, "failnocow", config.DataDir)
		assert.True(t, containsCall(runner.Calls, "subvolume", "delete", dataDir),
			"cleanup should call subvolume delete, got: %v", runner.Calls)
	})
}

// --- TestListVolumes ---

func TestListVolumes(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		vols, err := s.ListVolumes("test")
		require.NoError(t, err, "ListVolumes")
		assert.Empty(t, vols)
	})

	t.Run("multiple", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		for _, name := range []string{"vol1", "vol2", "vol3"} {
			dir := filepath.Join(bp, name)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			writeTestMetadata(t, dir, VolumeMetadata{Name: name, SizeBytes: 1024})
		}

		vols, err := s.ListVolumes("test")
		require.NoError(t, err, "ListVolumes")
		assert.Len(t, vols, 3)

		names := make(map[string]bool)
		for _, v := range vols {
			names[v.Name] = true
		}
		assert.True(t, names["vol1"], "vol1 should be in list")
		assert.True(t, names["vol2"], "vol2 should be in list")
		assert.True(t, names["vol3"], "vol3 should be in list")
	})

	t.Run("skips_snapshots_files_corrupt", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)

		// valid volume
		vol := filepath.Join(bp, "good")
		require.NoError(t, os.MkdirAll(vol, 0o755))
		writeTestMetadata(t, vol, VolumeMetadata{Name: "good", SizeBytes: 1024})

		// file (not dir) - skipped
		require.NoError(t, os.WriteFile(filepath.Join(bp, "somefile"), []byte("x"), 0o644))

		// snapshots dir already exists from utils - skipped

		// corrupt metadata - skipped
		corrupt := filepath.Join(bp, "corrupt")
		require.NoError(t, os.MkdirAll(corrupt, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(corrupt, config.MetadataFile), []byte("{broken"), 0o644))

		// dir without metadata - skipped
		require.NoError(t, os.MkdirAll(filepath.Join(bp, "nometa"), 0o755))

		vols, err := s.ListVolumes("test")
		require.NoError(t, err, "ListVolumes")
		require.Len(t, vols, 1, "only valid volume should be returned")
		assert.Equal(t, "good", vols[0].Name)
	})
}

// --- TestGetVolume ---

func TestGetVolume(t *testing.T) {
	s, bp, _, _ := newTestStorage(t)

	volDir := filepath.Join(bp, "myvol")
	require.NoError(t, os.MkdirAll(volDir, 0o755))
	writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol", SizeBytes: 2048})

	tests := []struct {
		name   string
		vol    string
		code   string
		expect string
	}{
		{name: "found", vol: "myvol", expect: "myvol"},
		{name: "not_found", vol: "nonexistent", code: ErrNotFound},
		{name: "invalid_name", vol: "bad name!", code: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := s.GetVolume("test", tt.vol)
			if tt.code != "" {
				requireStorageError(t, err, tt.code)
				return
			}
			require.NoError(t, err, "GetVolume")
			assert.Equal(t, tt.expect, meta.Name)
		})
	}

	t.Run("corrupt_metadata", func(t *testing.T) {
		corrupt := filepath.Join(bp, "corrupt")
		require.NoError(t, os.MkdirAll(corrupt, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(corrupt, config.MetadataFile), []byte("{bad"), 0o644))

		_, err := s.GetVolume("test", "corrupt")
		requireStorageError(t, err, ErrNotFound)
	})
}

// --- TestUpdateVolume ---

func TestUpdateVolume(t *testing.T) {
	ctx := context.Background()

	// setupVol creates a volume dir with metadata and data/ subdir.
	setupVol := func(t *testing.T, bp, name string, meta VolumeMetadata) {
		t.Helper()
		volDir := filepath.Join(bp, name)
		dataDir := filepath.Join(volDir, config.DataDir)
		require.NoError(t, os.MkdirAll(dataDir, 0o755))
		writeTestMetadata(t, volDir, meta)
	}

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name string
			vol  string
			meta VolumeMetadata
			req  VolumeUpdateRequest
			code string
		}{
			{
				name: "not_found",
				vol:  "nonexistent",
				code: ErrNotFound,
			},
			{
				name: "invalid_name",
				vol:  "bad name!",
				code: ErrInvalid,
			},
			{
				name: "size_must_increase",
				vol:  "vol",
				meta: VolumeMetadata{Name: "vol", SizeBytes: 1024},
				req:  VolumeUpdateRequest{SizeBytes: ptrUint64(512)},
				code: ErrInvalid,
			},
			{
				name: "size_equal",
				vol:  "vol",
				meta: VolumeMetadata{Name: "vol", SizeBytes: 1024},
				req:  VolumeUpdateRequest{SizeBytes: ptrUint64(1024)},
				code: ErrInvalid,
			},
			{
				name: "invalid_compression",
				vol:  "vol",
				meta: VolumeMetadata{Name: "vol", SizeBytes: 1024},
				req:  VolumeUpdateRequest{Compression: ptrString("brotli")},
				code: ErrInvalid,
			},
			{
				name: "nocow_with_compression",
				vol:  "vol",
				meta: VolumeMetadata{Name: "vol", SizeBytes: 1024, NoCOW: true},
				req:  VolumeUpdateRequest{Compression: ptrString("zstd")},
				code: ErrInvalid,
			},
			{
				name: "invalid_mode",
				vol:  "vol",
				meta: VolumeMetadata{Name: "vol", SizeBytes: 1024},
				req:  VolumeUpdateRequest{Mode: ptrString("nope")},
				code: ErrInvalid,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, bp, _, _ := newTestStorage(t)
				if tt.meta.Name != "" {
					setupVol(t, bp, tt.vol, tt.meta)
				}
				_, err := s.UpdateVolume(ctx, "test", tt.vol, tt.req)
				requireStorageError(t, err, tt.code)
			})
		}
	})

	t.Run("update_size", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		s.quotaEnabled = true
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024})

		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			SizeBytes: ptrUint64(2048),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.Equal(t, uint64(2048), meta.SizeBytes)
		assert.Equal(t, uint64(2048), meta.QuotaBytes, "QuotaBytes should match new SizeBytes")

		dataDir := filepath.Join(bp, "vol", config.DataDir)
		require.Len(t, runner.Calls, 1, "expected qgroup limit call")
		assert.Equal(t, []string{"qgroup", "limit", "2048", dataDir}, runner.Calls[0])
	})

	t.Run("update_compression", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024})

		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			Compression: ptrString("lzo"),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.Equal(t, "lzo", meta.Compression)

		dataDir := filepath.Join(bp, "vol", config.DataDir)
		require.Len(t, runner.Calls, 1, "expected set compression call")
		assert.Equal(t, []string{"property", "set", dataDir, "compression", "lzo"}, runner.Calls[0])
	})

	t.Run("update_nocow_enable", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024, NoCOW: false})

		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			NoCOW: ptrBool(true),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.True(t, meta.NoCOW)

		dataDir := filepath.Join(bp, "vol", config.DataDir)
		require.Len(t, runner.Calls, 1, "expected chattr call")
		assert.Equal(t, []string{"+C", dataDir}, runner.Calls[0])
	})

	t.Run("nocow_revert_ignored", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024, NoCOW: true})

		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			NoCOW: ptrBool(false),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.True(t, meta.NoCOW, "nocow should remain true (irreversible)")
		assert.Empty(t, runner.Calls, "no btrfs calls expected for nocow revert")
	})

	t.Run("update_chown", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024, UID: 0, GID: 0})

		uid := os.Getuid()
		gid := os.Getgid()
		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			UID: ptrInt(uid),
			GID: ptrInt(gid),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.Equal(t, uid, meta.UID)
		assert.Equal(t, gid, meta.GID)
	})

	t.Run("update_chmod", func(t *testing.T) {
		s, bp, _, _ := newTestStorage(t)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024, Mode: "0755"})

		meta, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			Mode: ptrString("0700"),
		})
		require.NoError(t, err, "UpdateVolume")
		assert.Equal(t, "0700", meta.Mode)

		dataDir := filepath.Join(bp, "vol", config.DataDir)
		info, err := os.Stat(dataDir)
		require.NoError(t, err, "Stat data dir")
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "permissions should be updated")
	})

	t.Run("qgroup_limit_fails", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("qgroup error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)
		s.quotaEnabled = true
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024})

		_, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			SizeBytes: ptrUint64(2048),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "qgroup limit failed")
	})

	t.Run("set_compression_fails", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("property error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)
		setupVol(t, bp, "vol", VolumeMetadata{Name: "vol", SizeBytes: 1024})

		_, err := s.UpdateVolume(ctx, "test", "vol", VolumeUpdateRequest{
			Compression: ptrString("zstd"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "set compression failed")
	})
}

// --- TestDeleteVolume ---

func TestDeleteVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Unexport", mock.Anything, volDir, "").Return(nil)

		err := s.DeleteVolume(ctx, "test", "myvol")
		require.NoError(t, err, "DeleteVolume")

		_, statErr := os.Stat(volDir)
		assert.True(t, os.IsNotExist(statErr), "volDir should be removed")
		exporter.AssertExpectations(t)
	})

	t.Run("not_found", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		err := s.DeleteVolume(ctx, "test", "nonexistent")
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("unexport_failure_continues", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Unexport", mock.Anything, volDir, "").Return(fmt.Errorf("nfs error"))

		err := s.DeleteVolume(ctx, "test", "myvol")
		require.NoError(t, err, "unexport failure should not block delete")

		_, statErr := os.Stat(volDir)
		assert.True(t, os.IsNotExist(statErr), "volDir should be removed despite unexport failure")
	})

	t.Run("subvol_delete_fails", func(t *testing.T) {
		runner := &utils.MockRunner{Err: fmt.Errorf("subvol error")}
		exporter := &nfs.MockExporter{}
		s, bp := testStorageWithRunner(t, runner, exporter)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Unexport", mock.Anything, volDir, "").Return(nil)

		err := s.DeleteVolume(ctx, "test", "myvol")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs subvolume delete failed")

		_, statErr := os.Stat(volDir)
		assert.False(t, os.IsNotExist(statErr), "volDir should still exist when subvol delete fails")
	})
}
