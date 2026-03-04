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

// --- unit tests ---

// K8s allows actually 128 chars for PVC / Snapshot names, never have seen that
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
		{"max_length_64", strings.Repeat("a", 64), false},
		{"pvc_name", "pvc-3f8a9b2c-1234-5678-9abc-def012345678", false},
		{"snapshot", "snap-vol01", false},
		{"snapcontent", "snapcontent-3f8a9b2c-1234-5678-9abc-def012345678", false},
		{"empty", "", true},
		{"too_long_65", strings.Repeat("a", 65), true},
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
