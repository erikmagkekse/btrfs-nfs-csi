package controller

import (
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// --- TestPaginate ---

func TestPaginate(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}

	t.Run("empty_slice", func(t *testing.T) {
		out, next, err := paginate([]string{}, "", 0)
		require.NoError(t, err)
		assert.Empty(t, out)
		assert.Empty(t, next)
	})

	t.Run("no_limit_no_token", func(t *testing.T) {
		out, next, err := paginate(items, "", 0)
		require.NoError(t, err)
		assert.Equal(t, items, out)
		assert.Empty(t, next)
	})

	t.Run("with_max_entries", func(t *testing.T) {
		out, next, err := paginate(items, "", 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, out)
		assert.Equal(t, "2", next)
	})

	t.Run("with_starting_token", func(t *testing.T) {
		out, next, err := paginate(items, "2", 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"c", "d"}, out)
		assert.Equal(t, "4", next)
	})

	t.Run("token_at_end", func(t *testing.T) {
		out, next, err := paginate(items, "4", 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"e"}, out)
		assert.Empty(t, next)
	})

	t.Run("token_beyond_length", func(t *testing.T) {
		out, next, err := paginate(items, "99", 0)
		require.NoError(t, err)
		assert.Empty(t, out)
		assert.Empty(t, next)
	})

	t.Run("invalid_token", func(t *testing.T) {
		_, _, err := paginate(items, "notanumber", 0)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Aborted, st.Code())
	})
}

// --- TestMakeVolumeID / TestParseVolumeID ---

func TestMakeVolumeID(t *testing.T) {
	id := utils.MakeVolumeID("my-sc", "my-vol")
	sc, name, err := utils.ParseVolumeID(id)
	require.NoError(t, err)
	assert.Equal(t, "my-sc", sc)
	assert.Equal(t, "my-vol", name)
}

func TestParseVolumeID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantSC  string
		wantVol string
		wantErr bool
	}{
		{name: "valid", id: "sc|vol", wantSC: "sc", wantVol: "vol"},
		{name: "pipe_in_name", id: "sc|vol|extra", wantSC: "sc", wantVol: "vol|extra"},
		{name: "no_separator", id: "nopipe", wantErr: true},
		{name: "empty_sc", id: "|vol", wantErr: true},
		{name: "empty_name", id: "sc|", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, name, err := utils.ParseVolumeID(tt.id)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSC, sc)
			assert.Equal(t, tt.wantVol, name)
		})
	}
}

// --- TestParseNodeIP ---

func TestParseNodeIP(t *testing.T) {
	tests := []struct {
		name    string
		nodeID  string
		wantIP  string
		wantErr bool
	}{
		{name: "valid", nodeID: "node1|10.0.0.1", wantIP: "10.0.0.1"},
		{name: "no_separator", nodeID: "node1", wantErr: true},
		{name: "empty_ip", nodeID: "node1|", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := parseNodeIP(tt.nodeID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantIP, ip)
		})
	}
}
