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
		// /tmp exists as a mount, so /tmp/nonexistent should find /tmp or /
		mp, err := FindMountPoint("/tmp/nonexistent")
		require.NoError(t, err)
		assert.NotEmpty(t, mp)
	})
}
