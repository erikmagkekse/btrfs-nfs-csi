package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindMountPoint(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		mp, err := FindMountPoint("/")
		require.NoError(t, err)
		assert.Equal(t, "/", mp)
	})

	t.Run("tempdir", func(t *testing.T) {
		dir := t.TempDir()
		mp, err := FindMountPoint(dir)
		require.NoError(t, err)
		assert.NotEmpty(t, mp)
		assert.True(t, strings.HasPrefix(dir, mp) || dir == mp,
			"mount point %q should be a prefix of %q", mp, dir)
	})

	t.Run("nonexistent path still finds mount", func(t *testing.T) {
		mp, err := FindMountPoint("/nonexistent/path/that/does/not/exist")
		require.NoError(t, err)
		assert.NotEmpty(t, mp)
	})
}

func TestIsMountPoint(t *testing.T) {
	t.Run("root_is_mount_point", func(t *testing.T) {
		assert.True(t, IsMountPoint("/"))
	})

	t.Run("tempdir_is_not_mount_point", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, IsMountPoint(dir))
	})
}
