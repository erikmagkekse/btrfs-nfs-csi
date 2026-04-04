package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/meta"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/nfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test utils for storage operations ---

// testStorageWithRunner creates a Storage with a configurable MockRunner and MockExporter.
// Sets defaultDataMode to "2770" (matching the default agent config).
func testStorageWithRunner(t *testing.T, runner *utils.MockRunner, exporter *nfs.MockExporter) (*Storage, string) {
	t.Helper()
	base := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				meta.ClearImmutable(path)
			}
			return nil
		})
	})
	tenant := "test"
	tenantPath := filepath.Join(base, tenant)
	require.NoError(t, os.MkdirAll(tenantPath, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tenantPath, config.SnapshotsDir), 0o755))

	mgr := btrfs.NewManagerWithRunner("btrfs", runner)
	s := &Storage{
		basePath:        base,
		mountPoint:      base,
		btrfs:           mgr,
		exporter:        exporter,
		tenants:         []string{tenant},
		defaultDirMode:  0o755,
		defaultDataMode: "2770",
		// immutableLabelKeys left nil to avoid requiring created-by in every test
		volumes:   meta.NewStore[VolumeMetadata](base),
		snapshots: meta.NewStore[SnapshotMetadata](base, config.SnapshotsDir),
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

func writeSnapshotMetadata(t *testing.T, s *Storage, snapDir string, meta SnapshotMetadata) {
	t.Helper()
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(snapDir, config.MetadataFile), data, 0o644))
	if s != nil {
		s.snapshots.Seed("test", meta.Name, &meta)
	}
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

// readTestJSON reads and unmarshals a JSON file (test-only disk read helper).
func readTestJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v))
}

// readVolumeMeta reads VolumeMetadata from disk into a fresh struct.
func readVolumeMeta(t *testing.T, volDir string) VolumeMetadata {
	t.Helper()
	var m VolumeMetadata
	readTestJSON(t, filepath.Join(volDir, config.MetadataFile), &m)
	return m
}

// seedVolume writes volume metadata to disk AND seeds the cache.
func seedVolume(t *testing.T, s *Storage, tenant, bp string, meta VolumeMetadata) string {
	t.Helper()
	dir := filepath.Join(bp, meta.Name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.MetadataFile), data, 0o644))
	s.volumes.Seed(tenant, meta.Name, &meta)
	return dir
}

// seedSnapshot writes snapshot metadata to disk AND seeds the cache.
func seedSnapshot(t *testing.T, s *Storage, tenant, bp string, meta SnapshotMetadata) string {
	t.Helper()
	dir := filepath.Join(bp, config.SnapshotsDir, meta.Name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	data, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.MetadataFile), data, 0o644))
	s.snapshots.Seed(tenant, meta.Name, &meta)
	return dir
}

func ptrUint64(v uint64) *uint64 { return &v }
func ptrInt(v int) *int          { return &v }
func ptrString(v string) *string { return &v }
func ptrBool(v bool) *bool       { return &v }

// --- unit tests ---

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "myvolume", false},
		{"with_hyphen", "vol-1", false},
		{"with_underscore", "under_score", false},
		{"single_char", "A", false},
		{"max_length_128", strings.Repeat("a", 128), false},
		{"pvc_name", "pvc-3f8a9b2c-1234-5678-9abc-def012345678", false},
		{"snapshot", "snap-vol01", false},
		{"snapcontent", "snapcontent-3f8a9b2c-1234-5678-9abc-def012345678", false},
		{"empty", "", true},
		{"too_long_129", strings.Repeat("a", 129), true},
		{"has_space", "has space", true},
		{"has_dot", "has.dot", true},
		{"has_slash", "path/slash", true},
		{"special_chars", "special!@#", true},
		{"k8s_namespace_slash", "default/my-vol", true},
		{"k8s_dotted_name", "my.volume.claim", true},
		{"colon_separator", "snap:content", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateName(tt.input)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var se *StorageError
			require.ErrorAs(t, err, &se)
			assert.Equal(t, ErrInvalid, se.Code)
		})
	}
}

func TestValidateLabels(t *testing.T) {
	tests := []struct {
		name    string
		labels  map[string]string
		wantErr bool
	}{
		{"nil", nil, false},
		{"empty", map[string]string{}, false},
		{"valid_single", map[string]string{"env": "prod"}, false},
		{"valid_multiple", map[string]string{"env": "prod", "team": "backend"}, false},
		{"valid_dots_dashes", map[string]string{"app.kubernetes.io": "my-app"}, false},
		{"empty_value", map[string]string{"env": ""}, false},
		{"too_many", map[string]string{
			"a": "1", "b": "2", "c": "3", "d": "4",
			"e": "5", "f": "6", "g": "7", "h": "8",
			"i": "9", "j": "10", "k": "11", "l": "12",
			"m": "13",
		}, true},
		{"key_uppercase", map[string]string{"Env": "prod"}, true},
		{"key_starts_with_dash", map[string]string{"-env": "prod"}, true},
		{"key_empty", map[string]string{"": "prod"}, true},
		{"key_too_long", map[string]string{strings.Repeat("a", 64): "v"}, true},
		{"value_has_slash", map[string]string{"env": "a/b"}, true},
		{"value_has_comma", map[string]string{"env": "a,b"}, true},
		{"value_has_space", map[string]string{"env": "a b"}, true},
		{"value_too_long", map[string]string{"env": strings.Repeat("x", 129)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLabels(tt.labels)
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			requireStorageError(t, err, ErrInvalid)
		})
	}
}

func TestRequireImmutableLabels(t *testing.T) {
	keys := []string{"created-by"}

	assert.NoError(t, requireImmutableLabels(keys, map[string]string{"created-by": "csi"}))
	assert.NoError(t, requireImmutableLabels(keys, map[string]string{"created-by": "cli", "extra": "ok"}))
	requireStorageError(t, requireImmutableLabels(keys, map[string]string{"extra": "ok"}), ErrInvalid)
	requireStorageError(t, requireImmutableLabels(keys, map[string]string{}), ErrInvalid)
	requireStorageError(t, requireImmutableLabels(keys, nil), ErrInvalid)
	assert.NoError(t, requireImmutableLabels(nil, nil), "nil keys = no requirements")
}

func TestProtectImmutableLabels(t *testing.T) {
	keys := []string{"created-by"}

	t.Run("preserves_on_omit", func(t *testing.T) {
		updated := map[string]string{"env": "prod"}
		require.NoError(t, protectImmutableLabels(keys, map[string]string{"created-by": "csi"}, updated))
		assert.Equal(t, "csi", updated["created-by"], "should be preserved")
	})

	t.Run("rejects_change", func(t *testing.T) {
		updated := map[string]string{"created-by": "hacker"}
		err := protectImmutableLabels(keys, map[string]string{"created-by": "csi"}, updated)
		requireStorageError(t, err, ErrInvalid)
	})

	t.Run("allows_same_value", func(t *testing.T) {
		updated := map[string]string{"created-by": "csi", "env": "prod"}
		require.NoError(t, protectImmutableLabels(keys, map[string]string{"created-by": "csi"}, updated))
	})
}

func TestFileMode(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected os.FileMode
	}{
		{"rwxr-xr-x", 0o755, os.FileMode(0o755)},
		{"rw-r--r--", 0o644, os.FileMode(0o644)},
		{"setuid", 0o4755, os.FileMode(0o755) | os.ModeSetuid},
		{"setuid_no_other_read", 0o4750, os.FileMode(0o750) | os.ModeSetuid},
		{"setgid", 0o2750, os.FileMode(0o750) | os.ModeSetgid},
		{"setgid_no_other_exec", 0o2744, os.FileMode(0o744) | os.ModeSetgid},
		{"sticky", 0o1777, os.FileMode(0o777) | os.ModeSticky},
		{"sticky_no_other", 0o1770, os.FileMode(0o770) | os.ModeSticky},
		{"setuid_setgid", 0o6755, os.FileMode(0o755) | os.ModeSetuid | os.ModeSetgid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, fileMode(tt.input))
		})
	}
}

func TestUnixMode(t *testing.T) {
	tests := []struct {
		name     string
		input    os.FileMode
		expected uint64
	}{
		{"rwxr-xr-x", os.FileMode(0o755), 0o755},
		{"rw-r--r--", os.FileMode(0o644), 0o644},
		{"setuid", os.FileMode(0o755) | os.ModeSetuid, 0o4755},
		{"setuid_no_other_read", os.FileMode(0o750) | os.ModeSetuid, 0o4750},
		{"setgid", os.FileMode(0o750) | os.ModeSetgid, 0o2750},
		{"setgid_no_other_exec", os.FileMode(0o744) | os.ModeSetgid, 0o2744},
		{"sticky", os.FileMode(0o777) | os.ModeSticky, 0o1777},
		{"sticky_no_other", os.FileMode(0o770) | os.ModeSticky, 0o1770},
		{"setuid_setgid", os.FileMode(0o755) | os.ModeSetuid | os.ModeSetgid, 0o6755},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, unixMode(tt.input))
		})
	}
}

func TestFileModeRoundtrip(t *testing.T) {
	values := []uint64{0o755, 0o644, 0o750, 0o700, 0o777, 0o4755, 0o4750, 0o2750, 0o2744, 0o1777, 0o1770, 0o6755}
	for _, v := range values {
		assert.Equal(t, v, unixMode(fileMode(v)), "roundtrip failed for %#o", v)
	}
}
