package nfs

import (
	"context"
	"fmt"
	"hash/crc32"
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

func newTestExporter(m utils.Runner) *kernelExporter {
	return &kernelExporter{bin: "exportfs", cmd: m}
}

func TestExport(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		if err := e.Export(context.Background(), "/data/vol1", "10.0.0.1"); err != nil {
			t.Fatalf("Export() error: %v", err)
		}

		if len(m.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(m.Calls))
		}

		args := strings.Join(m.Calls[0], " ")
		if !strings.Contains(args, "-o") {
			t.Error("expected -o flag in args")
		}
		if !strings.Contains(args, "10.0.0.1:/data/vol1") {
			t.Errorf("expected client:path in args, got: %s", args)
		}

		fsid := crc32.ChecksumIEEE([]byte("/data/vol1")) & fsidMask
		if fsid == 0 {
			fsid = 1
		}
		if !strings.Contains(args, fmt.Sprintf("fsid=%d", fsid)) {
			t.Errorf("expected fsid=%d in args, got: %s", fsid, args)
		}
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("permission denied")}
		e := newTestExporter(m)

		if err := e.Export(context.Background(), "/data/vol1", "10.0.0.1"); err == nil {
			t.Fatal("Export() should return error")
		}
	})
}

func TestUnexport(t *testing.T) {
	t.Run("with client", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		if err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1"); err != nil {
			t.Fatalf("Unexport() error: %v", err)
		}

		if len(m.Calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(m.Calls))
		}

		args := strings.Join(m.Calls[0], " ")
		if !strings.Contains(args, "-u") || !strings.Contains(args, "10.0.0.1:/data/vol1") {
			t.Errorf("expected '-u 10.0.0.1:/data/vol1', got: %s", args)
		}
	})

	t.Run("without client", func(t *testing.T) {
		// -v returns two clients, then -u is called for each
		m := &utils.MockRunner{
			RunFn: func(args []string) (string, error) {
				if slices.Contains(args, "-v") {
					return strings.Join([]string{
						"/data/vol1\t10.0.0.1(rw,fsid=1)",
						"/data/vol1\t10.0.0.2(rw,fsid=1)",
					}, "\n"), nil
				}
				return "", nil
			},
		}
		e := newTestExporter(m)

		if err := e.Unexport(context.Background(), "/data/vol1", ""); err != nil {
			t.Fatalf("Unexport() error: %v", err)
		}

		// 1 ListExports call + 2 unexport calls
		if len(m.Calls) != 3 {
			t.Fatalf("expected 3 calls, got %d", len(m.Calls))
		}
	})

	t.Run("not found ignored", func(t *testing.T) {
		m := &utils.MockRunner{
			Out: "Could not find /data/vol1",
			Err: fmt.Errorf("exportfs failed"),
		}
		e := newTestExporter(m)

		if err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1"); err != nil {
			t.Errorf("Unexport() should ignore not-found, got: %v", err)
		}
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{
			Out: "some other error",
			Err: fmt.Errorf("exportfs failed"),
		}
		e := newTestExporter(m)

		if err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1"); err == nil {
			t.Fatal("Unexport() should return error when not a not-found error")
		}
	})
}

func TestListExports(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("exportfs failed")}
		e := newTestExporter(m)

		exports, err := e.ListExports(context.Background())
		if err == nil {
			t.Fatal("ListExports() should return error")
		}
		if exports != nil {
			t.Errorf("ListExports() should return nil on error, got: %v", exports)
		}
	})
}

func TestParseExports(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []ExportInfo
	}{
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			// /data/vol1  10.0.0.1(rw,no_root_squash,fsid=123)
			name:   "single line export",
			output: "/data/vol1\t10.0.0.1(rw,no_root_squash,fsid=123)",
			want:   []ExportInfo{{Path: "/data/vol1", Client: "10.0.0.1"}},
		},
		{
			// /data/very/long/path/that/wraps
			//         10.0.0.2(rw,no_root_squash,fsid=456)
			name: "multiline long path",
			output: strings.Join([]string{
				"/data/very/long/path/that/wraps",
				"\t\t10.0.0.2(rw,no_root_squash,fsid=456)",
			}, "\n"),
			want: []ExportInfo{{Path: "/data/very/long/path/that/wraps", Client: "10.0.0.2"}},
		},
		{
			// /short      10.0.0.1(rw,fsid=1)
			// /very/long/path/name
			//             10.0.0.2(rw,fsid=2)
			// /another    10.0.0.3(rw,fsid=3)
			name: "mixed single and multiline",
			output: strings.Join([]string{
				"/short\t10.0.0.1(rw,fsid=1)",
				"/very/long/path/name",
				"\t\t10.0.0.2(rw,fsid=2)",
				"/another\t10.0.0.3(rw,fsid=3)",
			}, "\n"),
			want: []ExportInfo{
				{Path: "/short", Client: "10.0.0.1"},
				{Path: "/very/long/path/name", Client: "10.0.0.2"},
				{Path: "/another", Client: "10.0.0.3"},
			},
		},
		{
			// /shared  10.0.0.1(rw,fsid=1)
			// /shared  10.0.0.2(rw,fsid=1)
			name: "multiple clients for same path",
			output: strings.Join([]string{
				"/shared\t10.0.0.1(rw,fsid=1)",
				"/shared\t10.0.0.2(rw,fsid=1)",
			}, "\n"),
			want: []ExportInfo{
				{Path: "/shared", Client: "10.0.0.1"},
				{Path: "/shared", Client: "10.0.0.2"},
			},
		},
		{
			// (empty line)
			// (empty line)
			// /data  10.0.0.1(rw,fsid=1)
			// (empty line)
			name: "blank lines ignored",
			output: strings.Join([]string{
				"",
				"",
				"/data\t10.0.0.1(rw,fsid=1)",
				"",
			}, "\n"),
			want: []ExportInfo{{Path: "/data", Client: "10.0.0.1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExports(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("parseExports() returned %d exports, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("export[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
