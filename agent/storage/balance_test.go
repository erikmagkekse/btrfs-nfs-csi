package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/agent/storage/btrfs"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceArgsFromOpts(t *testing.T) {
	tests := []struct {
		name    string
		opts    map[string]string
		want    []string
		wantErr bool
	}{
		{name: "empty", opts: nil, want: nil},
		{name: "dusage", opts: map[string]string{"dusage": "50"}, want: []string{"-dusage=50"}},
		{name: "musage", opts: map[string]string{"musage": "75"}, want: []string{"-musage=75"}},
		{name: "susage", opts: map[string]string{"susage": "10"}, want: []string{"-susage=10"}},
		{name: "dprofiles_raid1", opts: map[string]string{"dprofiles": "raid1"}, want: []string{"-dprofiles=raid1"}},
		{name: "ddevid", opts: map[string]string{"ddevid": "2"}, want: []string{"-ddevid=2"}},
		{name: "dconvert_raid1", opts: map[string]string{"dconvert": "raid1"}, want: []string{"-dconvert=raid1"}},
		{name: "dconvert_soft", opts: map[string]string{"dconvert": "raid1,soft"}, want: []string{"-dconvert=raid1,soft"}},
		{name: "force", opts: map[string]string{"force": "true"}, want: []string{"-f"}},
		{name: "force_false_omitted", opts: map[string]string{"force": "false"}, want: []string{}},
		{
			name: "combined",
			opts: map[string]string{"dusage": "50", "mconvert": "raid1", "force": "true"},
			want: []string{"-dusage=50", "-mconvert=raid1", "-f"},
		},
		{name: "unknown_key", opts: map[string]string{"bogus": "1"}, wantErr: true},
		{name: "dusage_out_of_range", opts: map[string]string{"dusage": "101"}, wantErr: true},
		{name: "dusage_negative", opts: map[string]string{"dusage": "-1"}, wantErr: true},
		{name: "dusage_not_int", opts: map[string]string{"dusage": "abc"}, wantErr: true},
		{name: "invalid_profile", opts: map[string]string{"dprofiles": "raid99"}, wantErr: true},
		{name: "invalid_convert_profile", opts: map[string]string{"dconvert": "nope"}, wantErr: true},
		{name: "invalid_convert_soft_profile", opts: map[string]string{"dconvert": "bogus,soft"}, wantErr: true},
		{name: "ddevid_zero", opts: map[string]string{"ddevid": "0"}, wantErr: true},
		{name: "ddevid_negative", opts: map[string]string{"ddevid": "-1"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := balanceArgsFromOpts(tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				var ve *config.ValidationError
				require.ErrorAs(t, err, &ve)
				return
			}
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestFinalizeBalanceResult(t *testing.T) {
	midFlight := &btrfs.BalanceStatus{Running: true, Paused: false, ChunksDone: 1, ChunksTotal: 3}

	t.Run("nil_input_returns_nil", func(t *testing.T) {
		assert.Nil(t, finalizeBalanceResult(nil, nil, nil))
	})

	t.Run("successful_completion_fills_chunks", func(t *testing.T) {
		got := finalizeBalanceResult(midFlight, nil, nil)
		require.NotNil(t, got)
		assert.False(t, got.Running)
		assert.False(t, got.Paused)
		assert.Equal(t, uint64(3), got.ChunksDone)
		assert.Equal(t, uint64(3), got.ChunksTotal)
	})

	t.Run("cancelled_preserves_partial_chunks", func(t *testing.T) {
		// user cancelled mid-flight: ctx error present -> keep partial progress
		got := finalizeBalanceResult(midFlight, context.Canceled, context.Canceled)
		require.NotNil(t, got)
		assert.False(t, got.Running, "running must be cleared even on cancel")
		assert.False(t, got.Paused)
		assert.Equal(t, uint64(1), got.ChunksDone, "partial chunks must not be forged")
		assert.Equal(t, uint64(3), got.ChunksTotal)
	})

	t.Run("balance_error_preserves_partial_chunks", func(t *testing.T) {
		// btrfs command failed mid-flight: keep partial progress, don't forge
		got := finalizeBalanceResult(midFlight, errors.New("enospc"), nil)
		require.NotNil(t, got)
		assert.False(t, got.Running)
		assert.Equal(t, uint64(1), got.ChunksDone)
	})

	t.Run("paused_snapshot_cleared", func(t *testing.T) {
		paused := &btrfs.BalanceStatus{Running: true, Paused: true, ChunksDone: 2, ChunksTotal: 5}
		got := finalizeBalanceResult(paused, nil, nil)
		require.NotNil(t, got)
		assert.False(t, got.Running)
		assert.False(t, got.Paused)
	})

	t.Run("zero_total_not_fabricated", func(t *testing.T) {
		// edge case: completed but last poll had total=0 (e.g. balance finished
		// before the first poll fired, then something else loaded lastStatus)
		empty := &btrfs.BalanceStatus{Running: false, ChunksTotal: 0}
		got := finalizeBalanceResult(empty, nil, nil)
		require.NotNil(t, got)
		assert.Equal(t, uint64(0), got.ChunksDone)
		assert.Equal(t, uint64(0), got.ChunksTotal)
	})
}
