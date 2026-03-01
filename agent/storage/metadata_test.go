package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMetadataCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json!!!"), 0o644))

	var meta VolumeMetadata
	err := ReadMetadata(path, &meta)
	require.Error(t, err)
}

func TestUpdateMetadataNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	err := UpdateMetadata(path, func(m *VolumeMetadata) {
		m.SizeBytes = 1024
	})
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestMetaLock(t *testing.T) {
	t.Run("serializes_same_path", func(t *testing.T) {
		path := "/test/serialize"
		var seq []int
		var mu sync.Mutex
		var wg sync.WaitGroup

		// First goroutine grabs the lock immediately
		rm := metaLock(path)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This will block until the first lock is released
			rm2 := metaLock(path)
			mu.Lock()
			seq = append(seq, 2)
			mu.Unlock()
			metaUnlock(path, rm2)
		}()

		// Give goroutine time to block on the lock
		mu.Lock()
		seq = append(seq, 1)
		mu.Unlock()
		metaUnlock(path, rm)

		wg.Wait()
		assert.Equal(t, []int{1, 2}, seq)
	})

	t.Run("cleanup_on_zero_refs", func(t *testing.T) {
		path := "/test/cleanup"
		rm := metaLock(path)

		metaLocksMu.Lock()
		_, exists := metaLocksMap[path]
		metaLocksMu.Unlock()
		assert.True(t, exists, "lock should exist while held")

		metaUnlock(path, rm)

		metaLocksMu.Lock()
		_, exists = metaLocksMap[path]
		metaLocksMu.Unlock()
		assert.False(t, exists, "lock should be removed after unlock with refs==0")
	})

	// Overengineered testing? At least we know now here is no risk of OOM
	t.Run("refcount_partial_unlock", func(t *testing.T) {
		path := "/test/refcount"
		rm1 := metaLock(path)

		var wg sync.WaitGroup
		wg.Add(1)
		var rm2 *refMutex
		go func() {
			defer wg.Done()
			rm2 = metaLock(path)
		}()

		metaUnlock(path, rm1)
		wg.Wait()

		// After first unlock, entry should still exist (refs == 1)
		metaLocksMu.Lock()
		_, exists := metaLocksMap[path]
		metaLocksMu.Unlock()
		assert.True(t, exists, "lock should still exist with refs > 0")

		metaUnlock(path, rm2)

		// After second unlock, entry should be gone (refs == 0)
		metaLocksMu.Lock()
		_, exists = metaLocksMap[path]
		metaLocksMu.Unlock()
		assert.False(t, exists, "lock should be removed after all refs released")
	})

	t.Run("independent_paths", func(t *testing.T) {
		pathA := "/test/indep/a"
		pathB := "/test/indep/b"
		var wg sync.WaitGroup

		rmA := metaLock(pathA)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Should not block — different path
			rmB := metaLock(pathB)
			metaUnlock(pathB, rmB)
		}()
		wg.Wait() // Would deadlock if pathB blocked on pathA
		metaUnlock(pathA, rmA)
	})
}
