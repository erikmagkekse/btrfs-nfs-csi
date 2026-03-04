package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestTenantPath ---

func TestTenantPath(t *testing.T) {
	s, bp, _, _ := newTestStorage(t)

	tests := []struct {
		name   string
		tenant string
		want   string
		code   string
	}{
		{name: "valid", tenant: "test", want: bp},
		{name: "invalid_name", tenant: "bad name!", code: ErrInvalid},
		{name: "not_found", tenant: "nonexistent", code: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := s.tenantPath(tt.tenant)
			if tt.code != "" {
				requireStorageError(t, err, tt.code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, path)
		})
	}
}

// --- TestStats ---

func TestStats(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		stats, err := s.Stats("test")
		require.NoError(t, err, "Stats")
		assert.NotZero(t, stats.TotalBytes, "TotalBytes should be > 0")
		assert.Equal(t, stats.TotalBytes, stats.UsedBytes+stats.FreeBytes,
			"Total = Used + Free: %d != %d + %d", stats.TotalBytes, stats.UsedBytes, stats.FreeBytes)
	})

	t.Run("invalid_tenant", func(t *testing.T) {
		s, _, _, _ := newTestStorage(t)

		_, err := s.Stats("bad name!")
		requireStorageError(t, err, ErrInvalid)
	})
}
