package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefragmentArgsFromOpts(t *testing.T) {
	tests := []struct {
		name    string
		opts    map[string]string
		want    []string
		wantErr bool
	}{
		{name: "empty_defaults_recursive", opts: nil, want: []string{"-r"}},
		{name: "recursive_explicit_true", opts: map[string]string{"recursive": "true"}, want: []string{"-r"}},
		{name: "recursive_false_omits_r", opts: map[string]string{"recursive": "false"}, want: []string{}},
		{name: "compress_zstd", opts: map[string]string{"compress": "zstd"}, want: []string{"-r", "-czstd"}},
		{name: "compress_lzo", opts: map[string]string{"compress": "lzo"}, want: []string{"-r", "-clzo"}},
		{name: "compress_zlib", opts: map[string]string{"compress": "zlib"}, want: []string{"-r", "-czlib"}},
		{name: "compress_none_no_flag", opts: map[string]string{"compress": "none"}, want: []string{"-r"}},
		{name: "threshold_valid", opts: map[string]string{"threshold": "1048576"}, want: []string{"-r", "-t", "1048576"}},
		{
			name: "combined",
			opts: map[string]string{"compress": "zstd", "recursive": "false", "threshold": "4096"},
			want: []string{"-czstd", "-t", "4096"},
		},
		{name: "unknown_key", opts: map[string]string{"bogus": "1"}, wantErr: true},
		{name: "compress_invalid_algo", opts: map[string]string{"compress": "snappy"}, wantErr: true},
		{name: "threshold_zero", opts: map[string]string{"threshold": "0"}, wantErr: true},
		{name: "threshold_negative", opts: map[string]string{"threshold": "-1"}, wantErr: true},
		{name: "threshold_not_int", opts: map[string]string{"threshold": "abc"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := defragmentArgsFromOpts(tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				var ve *config.ValidationError
				require.ErrorAs(t, err, &ve)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveDefragTarget(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "sub", "file.txt"), []byte("x"), 0o644))

	// Symlink pointing outside the base: build a "secret" dir next to base.
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret"), []byte("nope"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret"), filepath.Join(base, "escape-link")))
	// Symlink pointing within the base (valid).
	require.NoError(t, os.Symlink(filepath.Join(base, "sub"), filepath.Join(base, "inside-link")))

	t.Run("empty_returns_base", func(t *testing.T) {
		got, err := resolveDefragTarget(base, "")
		require.NoError(t, err)
		assert.Equal(t, base, got)
	})

	t.Run("valid_subdir", func(t *testing.T) {
		got, err := resolveDefragTarget(base, "sub")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "sub"), got)
	})

	t.Run("valid_file", func(t *testing.T) {
		got, err := resolveDefragTarget(base, "sub/file.txt")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "sub", "file.txt"), got)
	})

	t.Run("absolute_path_rejected", func(t *testing.T) {
		_, err := resolveDefragTarget(base, "/etc/passwd")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve)
	})

	t.Run("dotdot_rejected", func(t *testing.T) {
		_, err := resolveDefragTarget(base, "..")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve)
	})

	t.Run("dotdot_prefix_rejected", func(t *testing.T) {
		_, err := resolveDefragTarget(base, "../evil")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve)
	})

	t.Run("nonexistent_rejected", func(t *testing.T) {
		_, err := resolveDefragTarget(base, "does-not-exist")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve)
	})

	t.Run("symlink_inside_base_allowed", func(t *testing.T) {
		got, err := resolveDefragTarget(base, "inside-link")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "inside-link"), got)
	})

	t.Run("symlink_escape_rejected", func(t *testing.T) {
		_, err := resolveDefragTarget(base, "escape-link")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve, "symlink pointing outside base must be rejected")
	})

	t.Run("empty_path_but_base_missing_rejected", func(t *testing.T) {
		missing := filepath.Join(base, "not-a-real-volume")
		_, err := resolveDefragTarget(missing, "")
		var ve *config.ValidationError
		require.ErrorAs(t, err, &ve)
	})
}
