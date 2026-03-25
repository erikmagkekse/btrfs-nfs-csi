package driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TestResolveNodeIP ---

func TestResolveNodeIP(t *testing.T) {
	t.Run("static_fallback", func(t *testing.T) {
		ip, err := ResolveNodeIP("10.0.0.1", "", "")
		require.NoError(t, err)
		assert.Equal(t, "10.0.0.1", ip)
	})

	t.Run("all_empty", func(t *testing.T) {
		_, err := ResolveNodeIP("", "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "one of DRIVER_NODE_IP")
	})

	t.Run("invalid_interface", func(t *testing.T) {
		_, err := ResolveNodeIP("10.0.0.1", "doesnotexist99", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DRIVER_STORAGE_INTERFACE")
	})

	t.Run("invalid_cidr", func(t *testing.T) {
		_, err := ResolveNodeIP("10.0.0.1", "", "notacidr")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DRIVER_STORAGE_CIDR")
	})

	t.Run("interface_takes_priority", func(t *testing.T) {
		// invalid interface should error even though nodeIP is set
		_, err := ResolveNodeIP("10.0.0.1", "doesnotexist99", "")
		require.Error(t, err, "interface should be tried before falling back to nodeIP")
	})

	t.Run("cidr_takes_priority_over_nodeip", func(t *testing.T) {
		// invalid CIDR should error even though nodeIP is set
		_, err := ResolveNodeIP("10.0.0.1", "", "notacidr")
		require.Error(t, err, "CIDR should be tried before falling back to nodeIP")
	})

	t.Run("cidr_no_match", func(t *testing.T) {
		_, err := ResolveNodeIP("", "", "192.0.2.0/24")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no address found")
	})
}
