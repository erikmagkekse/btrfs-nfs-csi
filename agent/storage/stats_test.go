package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDeviceIOStats(t *testing.T) {
	// Real sysfs /sys/block/sda/stat output (whitespace-separated)
	// Fields: read_ios read_merges read_sectors read_ticks
	//         write_ios write_merges write_sectors write_ticks
	//         ios_in_progress io_ticks weighted_io_ticks
	input := "   12345   1234  246912  5678   54321   5432  1975308  8765   2   45678   56789"

	t.Run("success", func(t *testing.T) {
		stats, err := parseDeviceIOStats(input)
		require.NoError(t, err)

		assert.Equal(t, uint64(12345), stats.ReadIOs)
		assert.Equal(t, uint64(246912*512), stats.ReadBytes) // sectors * 512
		assert.Equal(t, uint64(5678), stats.ReadTimeMs)
		assert.Equal(t, uint64(54321), stats.WriteIOs)
		assert.Equal(t, uint64(1975308*512), stats.WriteBytes) // sectors * 512
		assert.Equal(t, uint64(8765), stats.WriteTimeMs)
		assert.Equal(t, uint64(2), stats.IOsInProgress)
		assert.Equal(t, uint64(45678), stats.IOTimeMs)
		assert.Equal(t, uint64(56789), stats.WeightedIOTimeMs)
	})

	t.Run("too few fields", func(t *testing.T) {
		_, err := parseDeviceIOStats("1 2 3")
		assert.ErrorContains(t, err, "expected at least 11 fields")
	})

	t.Run("invalid number", func(t *testing.T) {
		_, err := parseDeviceIOStats("abc 2 3 4 5 6 7 8 9 10 11")
		assert.ErrorContains(t, err, "parse field 0")
	})

	t.Run("zeros", func(t *testing.T) {
		stats, err := parseDeviceIOStats("0 0 0 0 0 0 0 0 0 0 0")
		require.NoError(t, err)
		assert.Equal(t, uint64(0), stats.ReadIOs)
		assert.Equal(t, uint64(0), stats.WriteIOs)
		assert.Equal(t, uint64(0), stats.IOsInProgress)
	})

	t.Run("extra fields ignored", func(t *testing.T) {
		// Some kernels add extra fields (discard, flush stats)
		stats, err := parseDeviceIOStats("100 0 200 10 300 0 400 20 1 30 40 50 60 70 80 90 100")
		require.NoError(t, err)
		assert.Equal(t, uint64(100), stats.ReadIOs)
		assert.Equal(t, uint64(200*512), stats.ReadBytes)
		assert.Equal(t, uint64(300), stats.WriteIOs)
		assert.Equal(t, uint64(400*512), stats.WriteBytes)
	})
}

func TestResolveBlockDevice(t *testing.T) {
	// mountinfo format: id parent major:minor root mount_point opts ... - fstype source super_opts
	mountinfo := "22 1 8:1 / / rw,relatime - ext4 /dev/sda1 rw\n" +
		"30 22 0:52 /data /mnt/btrfs rw,relatime - btrfs /dev/vdb rw,space_cache=v2\n" +
		"35 22 0:53 / /mnt/other rw - tmpfs tmpfs rw\n"

	t.Run("btrfs mount", func(t *testing.T) {
		f := writeTempFile(t, mountinfo)
		name, err := resolveBlockDeviceFrom("/mnt/btrfs/tenant1", f)
		require.NoError(t, err)
		assert.Equal(t, "/dev/vdb", name)
	})

	t.Run("longest prefix wins", func(t *testing.T) {
		info := "22 1 8:1 / / rw - ext4 /dev/sda1 rw\n" +
			"30 22 0:52 / /mnt rw - btrfs /dev/vdb rw\n" +
			"35 30 0:53 / /mnt/btrfs rw - btrfs /dev/vdc rw\n"
		f := writeTempFile(t, info)
		name, err := resolveBlockDeviceFrom("/mnt/btrfs/data", f)
		require.NoError(t, err)
		assert.Equal(t, "/dev/vdc", name)
	})

	t.Run("no match", func(t *testing.T) {
		info := "30 22 0:52 / /mnt/btrfs rw - btrfs /dev/vdb rw\n"
		f := writeTempFile(t, info)
		_, err := resolveBlockDeviceFrom("/nonexistent", f)
		assert.ErrorContains(t, err, "no mount found")
	})

	t.Run("root fallback", func(t *testing.T) {
		f := writeTempFile(t, mountinfo)
		name, err := resolveBlockDeviceFrom("/home/user", f)
		require.NoError(t, err)
		// sda1 partition detection requires sysfs, without it we get the base device name
		assert.Contains(t, name, "/dev/sda")
	})
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "mountinfo")
	require.NoError(t, os.WriteFile(f, []byte(content), 0o644))
	return f
}
