package storage

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
