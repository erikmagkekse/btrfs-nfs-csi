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

func TestIsValidCompression(t *testing.T) {
	valid := []string{"", "none", "zstd", "lzo", "zlib", "zstd:1", "zstd:15", "zlib:9"}
	for _, s := range valid {
		assert.True(t, IsValidCompression(s), "IsValidCompression(%q) should be true", s)
	}

	invalid := []string{"doesnotexist", "zstd:0", "zstd:16", "zstd:420", "zstd:abc", "lz4", "gzip"}
	for _, s := range invalid {
		assert.False(t, IsValidCompression(s), "IsValidCompression(%q) should be false", s)
	}
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
