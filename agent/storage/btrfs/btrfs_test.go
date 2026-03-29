package btrfs

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

func newTestManager(r utils.Runner) *Manager {
	return &Manager{bin: "btrfs", cmd: r}
}

func TestQgroupUsageEx(t *testing.T) {
	// $ btrfs subvolume show /mnt/data/vol1
	// /mnt/data/vol1
	// 	Name:			vol1
	// 	UUID:			abcdef-1234
	// 	...
	// 	Subvolume ID:		259
	// 	...
	showOutput := strings.Join([]string{
		"/mnt/data/vol1",
		"\tName:\t\t\tvol1",
		"\tUUID:\t\t\tabcdef-1234",
		"\tParent UUID:\t\t-",
		"\tReceived UUID:\t\t-",
		"\tCreation time:\t\t2025-01-01 00:00:00 +0000",
		"\tSubvolume ID:\t\t259",
		"\tGeneration:\t\t42",
		"\tGen at creation:\t42",
		"\tParent ID:\t\t5",
		"\tTop level ID:\t\t5",
		"\tFlags:\t\t\t-",
	}, "\n")

	// $ btrfs qgroup show -re --raw /mnt/data/vol1
	// qgroupid         rfer         excl
	// --------         ----         ----
	// 0/259        16384         8192
	qgroupOutput := strings.Join([]string{
		"qgroupid         rfer         excl",
		"--------         ----         ----",
		"0/259        16384         8192",
	}, "\n")

	t.Run("success", func(t *testing.T) {
		callIdx := 0
		m := &utils.MockRunner{
			RunFn: func(args []string) (string, error) {
				callIdx++
				if slices.Contains(args, "show") && !slices.Contains(args, "-re") {
					return showOutput, nil
				}
				return qgroupOutput, nil
			},
		}
		mgr := newTestManager(m)

		info, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		require.NoError(t, err)
		assert.Equal(t, uint64(16384), info.Referenced)
		assert.Equal(t, uint64(8192), info.Exclusive)
		require.Len(t, m.Calls, 2)
	})

	t.Run("show error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("show failed")}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		require.Error(t, err)
	})

	t.Run("missing subvolume id", func(t *testing.T) {
		m := &utils.MockRunner{Out: "some output without subvolume id\n"}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		assert.ErrorContains(t, err, "subvolume ID not found")
	})

	t.Run("qgroup show error", func(t *testing.T) {
		m := &utils.MockRunner{
			RunFn: func(args []string) (string, error) {
				if slices.Contains(args, "show") && !slices.Contains(args, "-re") {
					return showOutput, nil
				}
				return "", fmt.Errorf("qgroup show failed")
			},
		}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		require.Error(t, err)
	})

	t.Run("qgroup not found", func(t *testing.T) {
		m := &utils.MockRunner{
			RunFn: func(args []string) (string, error) {
				if slices.Contains(args, "show") && !slices.Contains(args, "-re") {
					return showOutput, nil
				}
				// return qgroup output with different ID
				return "0/999        16384         8192\n", nil
			},
		}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		assert.ErrorContains(t, err, "qgroup 0/259 not found")
	})
}

func TestQgroupUsage(t *testing.T) {
	showOutput := "  Subvolume ID:\t\t259\n"
	qgroupOutput := "0/259        16384         8192\n"

	m := &utils.MockRunner{
		RunFn: func(args []string) (string, error) {
			if slices.Contains(args, "show") && !slices.Contains(args, "-re") {
				return showOutput, nil
			}
			return qgroupOutput, nil
		},
	}
	mgr := newTestManager(m)

	used, err := mgr.QgroupUsage(context.Background(), "/mnt/data/vol1")
	require.NoError(t, err)
	assert.Equal(t, uint64(16384), used)
}

func TestSubvolumeList(t *testing.T) {
	// $ btrfs subvolume list -o /mnt/data
	// ID 259 gen 12 top level 5 path vol1
	// ID 260 gen 13 top level 5 path vol2
	// ID 261 gen 14 top level 5 path nested/vol3
	t.Run("multiple entries", func(t *testing.T) {
		m := &utils.MockRunner{
			Out: strings.Join([]string{
				"ID 259 gen 12 top level 5 path vol1",
				"ID 260 gen 13 top level 5 path vol2",
				"ID 261 gen 14 top level 5 path nested/vol3",
			}, "\n"),
		}
		mgr := newTestManager(m)

		subs, err := mgr.SubvolumeList(context.Background(), "/mnt/data")
		require.NoError(t, err)
		require.Len(t, subs, 3)

		want := []string{"vol1", "vol2", "nested/vol3"}
		for i, s := range subs {
			assert.Equal(t, want[i], s.Path)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		subs, err := mgr.SubvolumeList(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Empty(t, subs)
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("list failed")}
		mgr := newTestManager(m)

		_, err := mgr.SubvolumeList(context.Background(), "/mnt/data")
		require.Error(t, err)
	})
}

func TestDeviceErrors(t *testing.T) {
	t.Run("single device", func(t *testing.T) {
		out := strings.Join([]string{
			"[/dev/sda].write_io_errs    0",
			"[/dev/sda].read_io_errs     0",
			"[/dev/sda].flush_io_errs    0",
			"[/dev/sda].corruption_errs  0",
			"[/dev/sda].generation_errs  0",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		errs, err := mgr.DeviceErrors(context.Background(), "/mnt/data")
		require.NoError(t, err)
		require.Len(t, errs, 1)
		assert.Equal(t, "/dev/sda", errs[0].Device)
		assert.Equal(t, uint64(0), errs[0].ReadErrs)
	})

	t.Run("with errors", func(t *testing.T) {
		out := strings.Join([]string{
			"[/dev/nvme0n1].write_io_errs    3",
			"[/dev/nvme0n1].read_io_errs     5",
			"[/dev/nvme0n1].flush_io_errs    1",
			"[/dev/nvme0n1].corruption_errs  2",
			"[/dev/nvme0n1].generation_errs  4",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		errs, err := mgr.DeviceErrors(context.Background(), "/mnt/data")
		require.NoError(t, err)
		require.Len(t, errs, 1)
		assert.Equal(t, "/dev/nvme0n1", errs[0].Device)
		assert.Equal(t, uint64(5), errs[0].ReadErrs)
		assert.Equal(t, uint64(3), errs[0].WriteErrs)
		assert.Equal(t, uint64(1), errs[0].FlushErrs)
		assert.Equal(t, uint64(2), errs[0].CorruptionErrs)
		assert.Equal(t, uint64(4), errs[0].GenerationErrs)
	})

	t.Run("multi device", func(t *testing.T) {
		out := strings.Join([]string{
			"[/dev/sda].write_io_errs    1",
			"[/dev/sda].read_io_errs     2",
			"[/dev/sda].flush_io_errs    0",
			"[/dev/sda].corruption_errs  0",
			"[/dev/sda].generation_errs  0",
			"[/dev/sdb].write_io_errs    0",
			"[/dev/sdb].read_io_errs     0",
			"[/dev/sdb].flush_io_errs    3",
			"[/dev/sdb].corruption_errs  0",
			"[/dev/sdb].generation_errs  0",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		errs, err := mgr.DeviceErrors(context.Background(), "/mnt/data")
		require.NoError(t, err)
		require.Len(t, errs, 2)
		assert.Equal(t, "/dev/sda", errs[0].Device)
		assert.Equal(t, uint64(1), errs[0].WriteErrs)
		assert.Equal(t, uint64(2), errs[0].ReadErrs)
		assert.Equal(t, "/dev/sdb", errs[1].Device)
		assert.Equal(t, uint64(3), errs[1].FlushErrs)
	})

	t.Run("command error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("device stats failed")}
		mgr := newTestManager(m)

		_, err := mgr.DeviceErrors(context.Background(), "/mnt/data")
		require.Error(t, err)
	})

	t.Run("empty output", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		_, err := mgr.DeviceErrors(context.Background(), "/mnt/data")
		assert.ErrorContains(t, err, "no device found")
	})
}

func TestFilesystemUsage(t *testing.T) {
	fsUsageOutput := strings.Join([]string{
		"Overall:",
		"    Device size:                 107374182400",
		"    Device allocated:             53687091200",
		"    Device unallocated:           53687091200",
		"    Device missing:                         0",
		"    Device slack:                           0",
		"    Used:                         42949672960",
		"    Free (estimated):             64424509440      (min: 37580963840)",
		"    Free (statfs, currentfull):   64424509440",
		"    Data ratio:                          1.00",
		"    Metadata ratio:                      2.00",
		"    Global reserve:               536870912      (used: 0)",
		"    Multiple profiles:                     no",
		"",
		"Data,single: Size:48318382080, Used:41875931136",
		"   /dev/sda    48318382080",
		"",
		"Metadata,DUP: Size:5368709120, Used:2147483648",
		"   /dev/sda    10737418240",
		"",
		"System,DUP: Size:8388608, Used:16384",
		"   /dev/sda      16777216",
		"",
		"Unallocated:",
		"   /dev/sda    53687091200",
	}, "\n")

	t.Run("success", func(t *testing.T) {
		m := &utils.MockRunner{Out: fsUsageOutput}
		mgr := newTestManager(m)

		fu, err := mgr.FilesystemUsage(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Equal(t, uint64(107374182400), fu.TotalBytes)
		assert.Equal(t, uint64(53687091200), fu.UnallocatedBytes)
		assert.Equal(t, uint64(42949672960), fu.UsedBytes)
		assert.Equal(t, uint64(64424509440), fu.FreeBytes)
		assert.Equal(t, 1.0, fu.DataRatio)
		assert.Equal(t, uint64(5368709120), fu.MetadataTotalBytes)
		assert.Equal(t, uint64(2147483648), fu.MetadataUsedBytes)
	})

	t.Run("raid1 data ratio", func(t *testing.T) {
		out := strings.Join([]string{
			"Overall:",
			"    Device size:                 107374182400",
			"    Used:                         42949672960",
			"    Data ratio:                          2.00",
			"",
			"Data,RAID1: Size:48318382080, Used:41875931136",
			"Metadata,RAID1: Size:5368709120, Used:2147483648",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		fu, err := mgr.FilesystemUsage(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Equal(t, 2.0, fu.DataRatio)
	})

	t.Run("command error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("filesystem usage failed")}
		mgr := newTestManager(m)

		_, err := mgr.FilesystemUsage(context.Background(), "/mnt/data")
		require.Error(t, err)
	})
}

func TestSetCompression(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		err := mgr.SetCompression(context.Background(), "/mnt/data/vol1", "zstd")
		require.NoError(t, err)
		require.Len(t, m.Calls, 1)
	})

	t.Run("invalid rejected before exec", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		err := mgr.SetCompression(context.Background(), "/mnt/data/vol1", "zstd:420")
		require.Error(t, err)
		assert.Empty(t, m.Calls)
	})
}

func TestDevices(t *testing.T) {
	t.Run("single device", func(t *testing.T) {
		out := strings.Join([]string{
			"Label: none  uuid: abc-123",
			"\tTotal devices 1 FS bytes used 10.00GiB",
			"\tdevid    1 size 50.00GiB used 15.00GiB path /dev/vdb",
			"",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		devices, err := mgr.Devices(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Equal(t, []string{"/dev/vdb"}, devices)
	})

	t.Run("raid1 two devices", func(t *testing.T) {
		out := strings.Join([]string{
			"Label: none  uuid: abc-123",
			"\tTotal devices 2 FS bytes used 10.00GiB",
			"\tdevid    1 size 50.00GiB used 15.00GiB path /dev/sda",
			"\tdevid    2 size 50.00GiB used 15.00GiB path /dev/sdb",
			"",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		devices, err := mgr.Devices(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Equal(t, []string{"/dev/sda", "/dev/sdb"}, devices)
	})

	t.Run("dm device", func(t *testing.T) {
		out := strings.Join([]string{
			"Label: none  uuid: abc-123",
			"\tTotal devices 1 FS bytes used 10.00GiB",
			"\tdevid    1 size 50.00GiB used 15.00GiB path /dev/dm-0",
			"",
		}, "\n")
		m := &utils.MockRunner{Out: out}
		mgr := newTestManager(m)

		devices, err := mgr.Devices(context.Background(), "/mnt/data")
		require.NoError(t, err)
		assert.Equal(t, []string{"/dev/dm-0"}, devices)
	})

	t.Run("command error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("show failed")}
		mgr := newTestManager(m)

		_, err := mgr.Devices(context.Background(), "/mnt/data")
		require.Error(t, err)
	})

	t.Run("no devices in output", func(t *testing.T) {
		m := &utils.MockRunner{Out: "Label: none  uuid: abc-123\n"}
		mgr := newTestManager(m)

		_, err := mgr.Devices(context.Background(), "/mnt/data")
		assert.ErrorContains(t, err, "no devices found")
	})
}

