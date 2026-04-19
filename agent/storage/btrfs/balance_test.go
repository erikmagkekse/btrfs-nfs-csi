package btrfs

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBalanceStatus(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want BalanceStatus
	}{
		{
			name: "idle",
			out:  "No balance found on '/mnt'\n",
			want: BalanceStatus{},
		},
		{
			name: "running_with_progress",
			out: strings.Join([]string{
				"Balance on '/mnt' is running",
				"3 out of about 20 chunks balanced (3 considered),  85% left",
				"Dumping filters: flags 0x1, state 0x1, force is off",
				"  DATA (flags 0x2): balancing, usage=50",
			}, "\n"),
			want: BalanceStatus{Running: true, ChunksDone: 3, ChunksTotal: 20},
		},
		{
			name: "paused",
			out: strings.Join([]string{
				"Balance on '/mnt' is paused",
				"5 out of about 10 chunks balanced (5 considered),  50% left",
			}, "\n"),
			want: BalanceStatus{Running: true, Paused: true, ChunksDone: 5, ChunksTotal: 10},
		},
		{
			name: "cancelling",
			out: strings.Join([]string{
				"Balance on '/mnt' is cancelling",
				"7 out of about 10 chunks balanced (7 considered),  30% left",
			}, "\n"),
			want: BalanceStatus{Running: true, ChunksDone: 7, ChunksTotal: 10},
		},
		{
			name: "empty",
			out:  "",
			want: BalanceStatus{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBalanceStatus(tt.out)
			require.NoError(t, err)
			assert.Equal(t, tt.want, *got)
		})
	}
}

func TestBalanceStart_BuildsCorrectArgs(t *testing.T) {
	m := &utils.MockRunner{Out: ""}
	mgr := newTestManager(m)

	err := mgr.BalanceStart(context.Background(), "/mnt", []string{"-dusage=50", "-mconvert=raid1"})
	require.NoError(t, err)
	require.Len(t, m.Calls, 1)

	assert.Equal(t, []string{"balance", "start", "-dusage=50", "-mconvert=raid1", "/mnt"}, m.Calls[0])
}

func TestBalanceStart_NoArgs(t *testing.T) {
	m := &utils.MockRunner{Out: ""}
	mgr := newTestManager(m)

	err := mgr.BalanceStart(context.Background(), "/mnt", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"balance", "start", "/mnt"}, m.Calls[0])
}

func TestBalanceStatus_ParsesIdle(t *testing.T) {
	m := &utils.MockRunner{Out: "No balance found on '/mnt'\n"}
	mgr := newTestManager(m)

	st, err := mgr.BalanceStatus(context.Background(), "/mnt")
	require.NoError(t, err)
	assert.False(t, st.Running)
}

func TestBalanceStatus_NonZeroExitStillParses(t *testing.T) {
	// `btrfs balance status` exits non-zero when no balance is running.
	// We expect the parser to still handle the output.
	m := &utils.MockRunner{Out: "No balance found on '/mnt'\n", Err: fmt.Errorf("exit 1")}
	mgr := newTestManager(m)

	st, err := mgr.BalanceStatus(context.Background(), "/mnt")
	require.NoError(t, err)
	assert.False(t, st.Running)
}

func TestBalanceCancel_BuildsCorrectArgs(t *testing.T) {
	m := &utils.MockRunner{Out: ""}
	mgr := newTestManager(m)

	err := mgr.BalanceCancel(context.Background(), "/mnt")
	require.NoError(t, err)
	assert.Equal(t, []string{"balance", "cancel", "/mnt"}, m.Calls[0])
}
