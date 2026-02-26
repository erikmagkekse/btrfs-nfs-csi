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
)

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

func newTestManager(r utils.Runner) *Manager {
	return &Manager{cmd: r}
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
		if err != nil {
			t.Fatalf("QgroupUsageEx() error: %v", err)
		}

		if info.Referenced != 16384 {
			t.Errorf("Referenced = %d, want 16384", info.Referenced)
		}
		if info.Exclusive != 8192 {
			t.Errorf("Exclusive = %d, want 8192", info.Exclusive)
		}

		if len(m.Calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(m.Calls))
		}
	})

	t.Run("show error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("show failed")}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		if err == nil {
			t.Fatal("QgroupUsageEx() should return error when show fails")
		}
	})

	t.Run("missing subvolume id", func(t *testing.T) {
		m := &utils.MockRunner{Out: "some output without subvolume id\n"}
		mgr := newTestManager(m)

		_, err := mgr.QgroupUsageEx(context.Background(), "/mnt/data/vol1")
		if err == nil {
			t.Fatal("QgroupUsageEx() should return error when subvolume ID missing")
		}
		if !strings.Contains(err.Error(), "subvolume ID not found") {
			t.Errorf("unexpected error: %v", err)
		}
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
		if err == nil {
			t.Fatal("QgroupUsageEx() should return error when qgroup show fails")
		}
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
		if err == nil {
			t.Fatal("QgroupUsageEx() should return error when qgroup not found")
		}
		if !strings.Contains(err.Error(), "qgroup 0/259 not found") {
			t.Errorf("unexpected error: %v", err)
		}
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
	if err != nil {
		t.Fatalf("QgroupUsage() error: %v", err)
	}
	if used != 16384 {
		t.Errorf("QgroupUsage() = %d, want 16384", used)
	}
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
		if err != nil {
			t.Fatalf("SubvolumeList() error: %v", err)
		}

		if len(subs) != 3 {
			t.Fatalf("expected 3 subvolumes, got %d", len(subs))
		}

		want := []string{"vol1", "vol2", "nested/vol3"}
		for i, s := range subs {
			if s.Path != want[i] {
				t.Errorf("subs[%d].Path = %q, want %q", i, s.Path, want[i])
			}
		}
	})

	t.Run("empty output", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		subs, err := mgr.SubvolumeList(context.Background(), "/mnt/data")
		if err != nil {
			t.Fatalf("SubvolumeList() error: %v", err)
		}
		if len(subs) != 0 {
			t.Errorf("expected 0 subvolumes, got %d", len(subs))
		}
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("list failed")}
		mgr := newTestManager(m)

		_, err := mgr.SubvolumeList(context.Background(), "/mnt/data")
		if err == nil {
			t.Fatal("SubvolumeList() should return error")
		}
	})
}

func TestIsValidCompression(t *testing.T) {
	valid := []string{"", "none", "zstd", "lzo", "zlib", "zstd:1", "zstd:15", "zlib:9"}
	for _, s := range valid {
		if !IsValidCompression(s) {
			t.Errorf("IsValidCompression(%q) = false, want true", s)
		}
	}

	invalid := []string{"doesnotexist", "zstd:0", "zstd:16", "zstd:420", "zstd:abc", "lz4", "gzip"}
	for _, s := range invalid {
		if IsValidCompression(s) {
			t.Errorf("IsValidCompression(%q) = true, want false", s)
		}
	}
}

func TestSetCompression(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		if err := mgr.SetCompression(context.Background(), "/mnt/data/vol1", "zstd"); err != nil {
			t.Fatalf("SetCompression() error: %v", err)
		}
		if len(m.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(m.Calls))
		}
	})

	t.Run("invalid rejected before exec", func(t *testing.T) {
		m := &utils.MockRunner{Out: ""}
		mgr := newTestManager(m)

		if err := mgr.SetCompression(context.Background(), "/mnt/data/vol1", "zstd:420"); err == nil {
			t.Fatal("SetCompression(zstd:420) should return an error")
		}
		if len(m.Calls) != 0 {
			t.Error("should not call btrfs for invalid algorithm")
		}
	})
}
