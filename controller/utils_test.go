package controller

import (
	"testing"

	"github.com/erikmagkekse/btrfs-nfs-csi/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestPageToken ---

func TestPageToken(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		token := encodePageToken("my-sc", "vol-42")
		pt, err := decodePageToken(token)
		require.NoError(t, err)
		assert.Equal(t, "my-sc", pt.SC)
		assert.Equal(t, "vol-42", pt.After)
	})

	t.Run("empty_token", func(t *testing.T) {
		pt, err := decodePageToken("")
		require.NoError(t, err)
		assert.Empty(t, pt.SC)
		assert.Empty(t, pt.After)
	})

	t.Run("invalid_token", func(t *testing.T) {
		_, err := decodePageToken("not-valid-base64!!!")
		require.Error(t, err)
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

func TestParseNodeHostname(t *testing.T) {
	assert.Equal(t, "worker-1", parseNodeHostname("worker-1|10.0.0.1"))
	assert.Equal(t, "node", parseNodeHostname("node|"))
	assert.Equal(t, "single", parseNodeHostname("single"))
}
