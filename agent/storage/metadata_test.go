package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json!!!"), 0o644))

	var meta VolumeMetadata
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	err = json.Unmarshal(data, &meta)
	require.Error(t, err)
}
