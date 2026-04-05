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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	os.Exit(m.Run())
}

const defaultOpts = "rw,nohide,crossmnt,no_root_squash,no_subtree_check"

func newTestExporter(m utils.Runner) *kernelExporter {
	return &kernelExporter{bin: "exportfs", cmd: m, opts: defaultOpts}
}

func TestExport(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Export(context.Background(), "/data/vol1", "10.0.0.1")
		require.NoError(t, err, "Export()")
		require.Len(t, m.Calls, 1)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "-o")
		assert.Contains(t, args, "10.0.0.1:/data/vol1")

		fsid := crc32.ChecksumIEEE([]byte("/data/vol1")) & fsidMask
		if fsid == 0 {
			fsid = 1
		}
		assert.Contains(t, args, fmt.Sprintf("fsid=%d", fsid))
	})

	t.Run("ipv6_brackets", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Export(context.Background(), "/data/vol1", "::1")
		require.NoError(t, err)
		require.Len(t, m.Calls, 1)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "[::1]:/data/vol1", "IPv6 must be bracket-wrapped")
		assert.NotContains(t, args, " ::1:/data/vol1", "bare IPv6 must not appear")
	})

	t.Run("ipv6_full_address", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Export(context.Background(), "/data/vol1", "2001:db8::1")
		require.NoError(t, err)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "[2001:db8::1]:/data/vol1")
	})

	t.Run("ipv4_no_brackets", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Export(context.Background(), "/data/vol1", "192.168.1.1")
		require.NoError(t, err)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "192.168.1.1:/data/vol1")
		assert.NotContains(t, args, "[192.168.1.1]")
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("permission denied")}
		e := newTestExporter(m)

		err := e.Export(context.Background(), "/data/vol1", "10.0.0.1")
		require.Error(t, err)
	})
}

func TestExportCustomOptions(t *testing.T) {
	t.Run("custom options passed through", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := &kernelExporter{bin: "exportfs", cmd: m, opts: "rw,no_root_squash,async"}

		err := e.Export(context.Background(), "/data/vol1", "10.0.0.1")
		require.NoError(t, err)
		require.Len(t, m.Calls, 1)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "rw,no_root_squash,async,fsid=")
		assert.NotContains(t, args, "nohide")
	})

	t.Run("fsid always appended", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := &kernelExporter{bin: "exportfs", cmd: m, opts: "rw"}

		err := e.Export(context.Background(), "/data/vol1", "10.0.0.1")
		require.NoError(t, err)

		args := strings.Join(m.Calls[0], " ")
		fsid := crc32.ChecksumIEEE([]byte("/data/vol1")) & fsidMask
		if fsid == 0 {
			fsid = 1
		}
		assert.Contains(t, args, fmt.Sprintf("rw,fsid=%d", fsid))
	})
}

func TestUnexport(t *testing.T) {
	t.Run("with client", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1")
		require.NoError(t, err, "Unexport()")
		require.Len(t, m.Calls, 1)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "-u")
		assert.Contains(t, args, "10.0.0.1:/data/vol1")
	})

	t.Run("with ipv6 client", func(t *testing.T) {
		m := &utils.MockRunner{}
		e := newTestExporter(m)

		err := e.Unexport(context.Background(), "/data/vol1", "::1")
		require.NoError(t, err)
		require.Len(t, m.Calls, 1)

		args := strings.Join(m.Calls[0], " ")
		assert.Contains(t, args, "[::1]:/data/vol1", "IPv6 must be bracket-wrapped in unexport")
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

		err := e.Unexport(context.Background(), "/data/vol1", "")
		require.NoError(t, err, "Unexport()")
		// 1 ListExports call + 2 unexport calls
		require.Len(t, m.Calls, 3)
	})

	t.Run("not found ignored", func(t *testing.T) {
		m := &utils.MockRunner{
			Out: "Could not find /data/vol1",
			Err: fmt.Errorf("exportfs failed"),
		}
		e := newTestExporter(m)

		err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1")
		assert.NoError(t, err, "Unexport() should ignore not-found")
	})

	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{
			Out: "some other error",
			Err: fmt.Errorf("exportfs failed"),
		}
		e := newTestExporter(m)

		err := e.Unexport(context.Background(), "/data/vol1", "10.0.0.1")
		require.Error(t, err)
	})
}

func TestListExports(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		m := &utils.MockRunner{Err: fmt.Errorf("exportfs failed")}
		e := newTestExporter(m)

		exports, err := e.ListExports(context.Background())
		require.Error(t, err)
		assert.Nil(t, exports)
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
		{
			name:   "ipv6 single line",
			output: "/data/vol1\t[::1](rw,no_root_squash,fsid=123)",
			want:   []ExportInfo{{Path: "/data/vol1", Client: "::1"}},
		},
		{
			name:   "ipv6 full address",
			output: "/data/vol1\t[2001:db8::1](rw,fsid=456)",
			want:   []ExportInfo{{Path: "/data/vol1", Client: "2001:db8::1"}},
		},
		{
			name: "ipv6 multiline",
			output: strings.Join([]string{
				"/data/very/long/path",
				"\t\t[fe80::1](rw,fsid=789)",
			}, "\n"),
			want: []ExportInfo{{Path: "/data/very/long/path", Client: "fe80::1"}},
		},
		{
			name: "mixed ipv4 and ipv6",
			output: strings.Join([]string{
				"/data/vol1\t10.0.0.1(rw,fsid=1)",
				"/data/vol2\t[::1](rw,fsid=2)",
			}, "\n"),
			want: []ExportInfo{
				{Path: "/data/vol1", Client: "10.0.0.1"},
				{Path: "/data/vol2", Client: "::1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExports(tt.output)
			require.Len(t, got, len(tt.want))
			for i := range got {
				assert.Equal(t, tt.want[i], got[i], "export[%d]", i)
			}
		})
	}
}
