package storage

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeMetadata_UnmarshalJSON_Migration(t *testing.T) {
	t.Run("old_format_string_array", func(t *testing.T) {
		data := `{"name":"vol","clients":["10.0.0.1","10.0.0.2"]}`
		var m VolumeMetadata
		require.NoError(t, json.Unmarshal([]byte(data), &m))
		require.Len(t, m.Exports, 2)
		assert.Equal(t, "10.0.0.1", m.Exports[0].IP)
		assert.Equal(t, "migrated", m.Exports[0].Labels["created-by"])
		assert.False(t, m.Exports[0].CreatedAt.IsZero(), "migrated exports should have a created_at")
		assert.Equal(t, "10.0.0.2", m.Exports[1].IP)
		assert.Equal(t, "vol", m.Name)
	})

	t.Run("new_format_client_ref", func(t *testing.T) {
		data := `{"name":"vol","clients":[{"ip":"10.0.0.1","labels":{"created-by":"csi"}}]}`
		var m VolumeMetadata
		require.NoError(t, json.Unmarshal([]byte(data), &m))
		require.Len(t, m.Exports, 1)
		assert.Equal(t, "10.0.0.1", m.Exports[0].IP)
		assert.Equal(t, "csi", m.Exports[0].Labels["created-by"])
	})

	t.Run("null_clients", func(t *testing.T) {
		data := `{"name":"vol","clients":null}`
		var m VolumeMetadata
		require.NoError(t, json.Unmarshal([]byte(data), &m))
		assert.Nil(t, m.Exports)
	})

	t.Run("empty_array", func(t *testing.T) {
		data := `{"name":"vol","clients":[]}`
		var m VolumeMetadata
		require.NoError(t, json.Unmarshal([]byte(data), &m))
		assert.Empty(t, m.Exports)
	})

	t.Run("no_clients_field", func(t *testing.T) {
		data := `{"name":"vol"}`
		var m VolumeMetadata
		require.NoError(t, json.Unmarshal([]byte(data), &m))
		assert.Nil(t, m.Exports)
	})

	t.Run("roundtrip_new_format", func(t *testing.T) {
		orig := VolumeMetadata{
			Name: "vol",
			Exports: []ExportMetadata{
				{IP: "10.0.0.1", Labels: map[string]string{"kubernetes.volume.id": "sc|vol1"}},
				{IP: "10.0.0.2"},
			},
		}
		data, err := json.Marshal(orig)
		require.NoError(t, err)

		var m VolumeMetadata
		require.NoError(t, json.Unmarshal(data, &m))
		assert.Equal(t, orig.Exports, m.Exports)
	})
}

func TestUniqueClientIPs(t *testing.T) {
	clients := []ExportMetadata{
		{IP: "10.0.0.2"},
		{IP: "10.0.0.1"},
		{IP: "10.0.0.2"},
		{IP: "10.0.0.3"},
	}
	ips := uniqueExportIPs(clients)
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, ips)
}

func TestCountUniqueExportIPs(t *testing.T) {
	clients := []ExportMetadata{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.1", Labels: map[string]string{"a": "b"}},
		{IP: "10.0.0.2"},
	}
	assert.Equal(t, 2, CountUniqueExportIPs(clients))
	assert.Equal(t, 0, CountUniqueExportIPs(nil))
}

func TestHasClientEntry(t *testing.T) {
	clients := []ExportMetadata{
		{IP: "10.0.0.1", Labels: map[string]string{"k": "v"}},
		{IP: "10.0.0.2"},
	}
	assert.True(t, hasExport(clients, "10.0.0.1", map[string]string{"k": "v"}))
	assert.False(t, hasExport(clients, "10.0.0.1", nil))
	assert.True(t, hasExport(clients, "10.0.0.2", nil))
	assert.False(t, hasExport(clients, "10.0.0.3", nil))
}

func TestRefsForIP(t *testing.T) {
	clients := []ExportMetadata{
		{IP: "10.0.0.1"},
		{IP: "10.0.0.1", Labels: map[string]string{"a": "b"}},
		{IP: "10.0.0.2"},
	}
	assert.Equal(t, 2, exportsForIP(clients, "10.0.0.1"))
	assert.Equal(t, 1, exportsForIP(clients, "10.0.0.2"))
	assert.Equal(t, 0, exportsForIP(clients, "10.0.0.3"))
}

func TestLabelsContain(t *testing.T) {
	stored := map[string]string{"created-by": "csi", "kubernetes.volume.id": "vol1", "kubernetes.node.name": "w1"}

	assert.True(t, labelsContain(stored, map[string]string{"kubernetes.volume.id": "vol1"}), "subset match")
	assert.True(t, labelsContain(stored, map[string]string{"kubernetes.volume.id": "vol1", "created-by": "csi"}), "multi-key subset")
	assert.True(t, labelsContain(stored, map[string]string{}), "empty match matches everything")
	assert.True(t, labelsContain(stored, nil), "nil match matches everything")
	assert.False(t, labelsContain(stored, map[string]string{"kubernetes.volume.id": "vol2"}), "wrong value")
	assert.False(t, labelsContain(stored, map[string]string{"missing-key": "x"}), "missing key")
	assert.True(t, labelsContain(nil, nil), "both nil")
	assert.True(t, labelsContain(nil, map[string]string{}), "nil stored, empty match")
	assert.False(t, labelsContain(nil, map[string]string{"a": "b"}), "nil stored, non-empty match")
}
