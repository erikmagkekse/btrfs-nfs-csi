package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

// testStorageWithRunner creates a Storage with a configurable MockRunner and MockExporter.
// Sets defaultDataMode to "2770" (matching the default agent config).
func testStorageWithRunner(t *testing.T, runner *utils.MockRunner, exporter *nfs.MockExporter) (*Storage, string) {
	t.Helper()
	base := t.TempDir()
	tenant := "test"
	tenantPath := filepath.Join(base, tenant)
	require.NoError(t, os.MkdirAll(tenantPath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tenantPath, config.SnapshotsDir), 0o755))

	mgr := btrfs.NewManagerWithRunner("btrfs", runner)
	s := &Storage{
		basePath:        base,
		btrfs:           mgr,
		exporter:        exporter,
		tenants:         []string{tenant},
		defaultDirMode:  0o755,
		defaultDataMode: "2770",
	}
	return s, tenantPath
}

// newTestStorage creates a Storage with fresh MockRunner and MockExporter.
// Returns all four components for assertions.
func newTestStorage(t *testing.T) (*Storage, string, *utils.MockRunner, *nfs.MockExporter) {
	t.Helper()
	runner := &utils.MockRunner{}
	exporter := &nfs.MockExporter{}
	s, bp := testStorageWithRunner(t, runner, exporter)
	return s, bp, runner, exporter
}

func writeSnapshotMetadata(t *testing.T, snapDir string, meta SnapshotMetadata) {
	t.Helper()
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapDir, config.MetadataFile), data, 0o644))
}

func requireStorageError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	var se *StorageError
	require.True(t, errors.As(err, &se), "expected *StorageError, got %T: %v", err, err)
	assert.Equal(t, code, se.Code)
}

// containsCall returns true if calls contains a call matching args exactly.
func containsCall(calls [][]string, args ...string) bool {
	for _, c := range calls {
		if slices.Equal(c, args) {
			return true
		}
	}
	return false
}

// readVolumeMeta reads VolumeMetadata from disk into a fresh struct (avoids omitempty pitfalls).
func readVolumeMeta(t *testing.T, volDir string) VolumeMetadata {
	t.Helper()
	var meta VolumeMetadata
	require.NoError(t, ReadMetadata(filepath.Join(volDir, config.MetadataFile), &meta))
	return meta
}

func ptrUint64(v uint64) *uint64 { return &v }
func ptrInt(v int) *int          { return &v }
func ptrString(v string) *string { return &v }
func ptrBool(v bool) *bool       { return &v }

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

		// snapshots dir already exists from helper - skipped

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

// --- TestExportVolume ---

func TestExportVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "ExportVolume")

		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1", "client should be in metadata")
		assert.NotNil(t, meta.LastAttachAt, "LastAttachAt should be set")
		exporter.AssertExpectations(t)
	})

	t.Run("idempotent_client", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "ExportVolume (idempotent)")

		meta := readVolumeMeta(t, volDir)
		count := 0
		for _, c := range meta.Clients {
			if c == "10.0.0.1" {
				count++
			}
		}
		assert.Equal(t, 1, count,
			"expected exactly 1 entry for 10.0.0.1, got %d in: %v", count, meta.Clients)
	})

	t.Run("not_found", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		err := s.ExportVolume(ctx, "test", "nonexistent", "10.0.0.1")
		requireStorageError(t, err, ErrNotFound)
	})

	t.Run("metadata_first_on_export_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{Name: "myvol"})

		exporter.On("Export", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.ExportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs export failed")

		// metadata should already have the client (written before export call)
		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1",
			"client should be persisted in metadata even though export failed")
		assert.NotNil(t, meta.LastAttachAt)
		exporter.AssertExpectations(t)
	})
}

// --- TestUnexportVolume ---

func TestUnexportVolume(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1", "10.0.0.2"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(nil)

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.NoError(t, err, "UnexportVolume")

		meta := readVolumeMeta(t, volDir)
		assert.NotContains(t, meta.Clients, "10.0.0.1", "removed client should be gone")
		assert.Contains(t, meta.Clients, "10.0.0.2", "other client should remain")
		exporter.AssertExpectations(t)
	})

	t.Run("client_not_in_list", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.99").Return(nil)

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.99")
		require.NoError(t, err, "UnexportVolume (client not in list)")

		meta := readVolumeMeta(t, volDir)
		assert.Contains(t, meta.Clients, "10.0.0.1", "existing client should be preserved")
	})

	t.Run("metadata_first_on_unexport_failure", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		volDir := filepath.Join(bp, "myvol")
		require.NoError(t, os.MkdirAll(volDir, 0o755))
		writeTestMetadata(t, volDir, VolumeMetadata{
			Name: "myvol", Clients: []string{"10.0.0.1"},
		})

		exporter.On("Unexport", mock.Anything, volDir, "10.0.0.1").Return(fmt.Errorf("nfs error"))

		err := s.UnexportVolume(ctx, "test", "myvol", "10.0.0.1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nfs unexport failed")

		// metadata should already have client removed (written before unexport call)
		meta := readVolumeMeta(t, volDir)
		assert.NotContains(t, meta.Clients, "10.0.0.1",
			"client should be removed from metadata even though unexport failed")
		exporter.AssertExpectations(t)
	})
}

// --- TestListExports ---

func TestListExports(t *testing.T) {
	ctx := context.Background()

	t.Run("filter_by_tenant", func(t *testing.T) {
		s, bp, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{
			{Path: filepath.Join(bp, "vol1"), Client: "10.0.0.1"},
			{Path: filepath.Join(bp, "vol2"), Client: "10.0.0.2"},
			{Path: "/other/tenant/vol", Client: "10.0.0.99"},
		}, nil)

		entries, err := s.ListExports(ctx, "test")
		require.NoError(t, err, "ListExports")
		assert.Len(t, entries, 2, "should only include exports under tenant path")
		exporter.AssertExpectations(t)
	})

	t.Run("empty", func(t *testing.T) {
		s, _, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo{}, nil)

		entries, err := s.ListExports(ctx, "test")
		require.NoError(t, err, "ListExports")
		assert.Empty(t, entries)
	})

	t.Run("error", func(t *testing.T) {
		s, _, _, exporter := newTestStorage(t)

		exporter.On("ListExports", mock.Anything).Return([]nfs.ExportInfo(nil), fmt.Errorf("rpc error"))

		_, err := s.ListExports(ctx, "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "list exports failed")
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

// --- TestCreateClone ---

func TestCreateClone(t *testing.T) {
	ctx := context.Background()

	// setupSrcSnap creates a source snapshot with data/ dir and metadata.
	setupSrcSnap := func(t *testing.T, bp, name string) {
		t.Helper()
		snapDir := filepath.Join(bp, config.SnapshotsDir, name)
		dataDir := filepath.Join(snapDir, config.DataDir)
		require.NoError(t, os.MkdirAll(dataDir, 0o755))
		writeSnapshotMetadata(t, snapDir, SnapshotMetadata{Name: name, Volume: "srcvol", ReadOnly: true})
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
					setupSrcSnap(t, bp, "mysnap")
				}
				if tt.name == "already_exists" {
					cloneDir := filepath.Join(bp, "existing")
					require.NoError(t, os.MkdirAll(cloneDir, 0o755))
					require.NoError(t, writeMetadataAtomic(
						filepath.Join(cloneDir, config.MetadataFile),
						CloneMetadata{Name: "existing", SourceSnapshot: "mysnap"},
					))
				}
				meta, err := s.CreateClone(ctx, "test", tt.req)
				requireStorageError(t, err, tt.code)
				if tt.name == "already_exists" {
					require.NotNil(t, meta, "should return existing metadata")
					assert.Equal(t, "existing", meta.Name)
				}
			})
		}
	})

	t.Run("success", func(t *testing.T) {
		s, bp, runner, _ := newTestStorage(t)
		setupSrcSnap(t, bp, "mysnap")

		meta, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "myclone", Snapshot: "mysnap",
		})
		require.NoError(t, err, "CreateClone")
		assert.Equal(t, "myclone", meta.Name)
		assert.Equal(t, "mysnap", meta.SourceSnapshot)
		assert.Equal(t, filepath.Join(bp, "myclone"), meta.Path)
		assert.False(t, meta.CreatedAt.IsZero(), "CreatedAt should be set")

		var ondisk CloneMetadata
		require.NoError(t, ReadMetadata(filepath.Join(bp, "myclone", config.MetadataFile), &ondisk))
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
		setupSrcSnap(t, bp, "mysnap")

		_, err := s.CreateClone(ctx, "test", CloneCreateRequest{
			Name: "failclone", Snapshot: "mysnap",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "btrfs snapshot failed")

		cloneDir := filepath.Join(bp, "failclone")
		_, statErr := os.Stat(cloneDir)
		assert.True(t, os.IsNotExist(statErr), "cloneDir should be cleaned up after failure")
	})

	t.Run("invalid_tenant", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		_, err := s.CreateClone(ctx, "bad tenant!", CloneCreateRequest{
			Name: "clone", Snapshot: "snap",
		})
		requireStorageError(t, err, ErrInvalid)
	})
}
