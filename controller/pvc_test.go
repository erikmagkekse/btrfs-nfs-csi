package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestVolumeParamsValidate ---

func TestVolumeParamsValidate(t *testing.T) {
	tests := []struct {
		name    string
		vp      volumeParams
		wantErr bool
	}{
		{name: "all_empty", vp: volumeParams{}},
		{name: "valid_all", vp: volumeParams{UID: "1000", GID: "1000", Mode: "0755"}},
		{name: "invalid_uid", vp: volumeParams{UID: "abc"}, wantErr: true},
		{name: "invalid_gid", vp: volumeParams{GID: "-1.5"}, wantErr: true},
		{name: "invalid_mode_not_octal", vp: volumeParams{Mode: "999"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.vp.validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// --- TestToUpdateRequest ---

func TestToUpdateRequest(t *testing.T) {
	t.Run("empty_no_change", func(t *testing.T) {
		vp := volumeParams{}
		_, changed := vp.toUpdateRequest()
		assert.False(t, changed)
	})

	t.Run("uid_gid_set", func(t *testing.T) {
		vp := volumeParams{UID: "1000", GID: "2000"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.UID)
		require.NotNil(t, req.GID)
		assert.Equal(t, 1000, *req.UID)
		assert.Equal(t, 2000, *req.GID)
	})

	t.Run("nocow_true", func(t *testing.T) {
		vp := volumeParams{NoCOW: "true"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.NoCOW)
		assert.True(t, *req.NoCOW)
	})

	t.Run("nocow_false", func(t *testing.T) {
		vp := volumeParams{NoCOW: "false"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.NoCOW)
		assert.False(t, *req.NoCOW)
	})

	t.Run("compression", func(t *testing.T) {
		vp := volumeParams{Compression: "zstd"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.Compression)
		assert.Equal(t, "zstd", *req.Compression)
	})

	t.Run("mode", func(t *testing.T) {
		vp := volumeParams{Mode: "0750"}
		req, changed := vp.toUpdateRequest()
		require.True(t, changed)
		require.NotNil(t, req.Mode)
		assert.Equal(t, "0750", *req.Mode)
	})
}
