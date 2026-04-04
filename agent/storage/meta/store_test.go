package meta

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMeta struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func testStore(t *testing.T) (*Store[testMeta], string) {
	t.Helper()
	dir := t.TempDir()
	return NewStore[testMeta](dir), dir
}

func TestStore_SeedAndGet(t *testing.T) {
	s, _ := testStore(t)
	m := &testMeta{Name: "foo", Value: 42}
	s.Seed("t", "k1", m)

	got, err := s.Get("t", "k1")
	require.NoError(t, err)
	assert.Equal(t, "foo", got.Name)
	assert.Equal(t, 42, got.Value)
}

func TestStore_GetCacheMiss_DiskFallback(t *testing.T) {
	s, dir := testStore(t)
	entryDir := filepath.Join(dir, "t", "k1")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, "metadata.json"), []byte(`{"name":"disk","value":99}`), 0644))

	got, err := s.Get("t", "k1")
	require.NoError(t, err)
	assert.Equal(t, "disk", got.Name)
	assert.Equal(t, 99, got.Value)

	// second call should hit cache even after removing file
	require.NoError(t, os.RemoveAll(entryDir))
	got2, err := s.Get("t", "k1")
	require.NoError(t, err)
	assert.Equal(t, "disk", got2.Name)
}

func TestStore_GetNotFound(t *testing.T) {
	s, _ := testStore(t)
	_, err := s.Get("t", "k1")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestStore_StoreWritesDisk(t *testing.T) {
	s, dir := testStore(t)
	entryDir := filepath.Join(dir, "t", "k1")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))

	m := &testMeta{Name: "written", Value: 7}
	require.NoError(t, s.Store("t", "k1", m))

	// verify disk
	var ondisk testMeta
	require.NoError(t, readJSON(filepath.Join(entryDir, "metadata.json"), &ondisk))
	assert.Equal(t, "written", ondisk.Name)

	// verify cache (remove file, still works)
	require.NoError(t, os.RemoveAll(entryDir))
	got, err := s.Get("t", "k1")
	require.NoError(t, err)
	assert.Equal(t, "written", got.Name)
}

func TestStore_Update(t *testing.T) {
	s, dir := testStore(t)
	entryDir := filepath.Join(dir, "t", "k1")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))

	require.NoError(t, s.Store("t", "k1", &testMeta{Name: "orig", Value: 1}))

	updated, err := s.Update("t", "k1", func(m *testMeta) {
		m.Value = 100
	})
	require.NoError(t, err)
	assert.Equal(t, 100, updated.Value)
	assert.Equal(t, "orig", updated.Name)

	// verify disk
	var ondisk testMeta
	require.NoError(t, readJSON(filepath.Join(entryDir, "metadata.json"), &ondisk))
	assert.Equal(t, 100, ondisk.Value)

	// verify cache
	got, _ := s.Get("t", "k1")
	assert.Equal(t, 100, got.Value)
}

func TestStore_Delete(t *testing.T) {
	s, _ := testStore(t)
	s.Seed("t", "k1", &testMeta{Name: "bye"})

	s.Delete("t", "k1")

	_, err := s.Get("t", "k1")
	require.Error(t, err)
}

func TestStore_Range(t *testing.T) {
	s, _ := testStore(t)
	s.Seed("t", "a", &testMeta{Name: "a"})
	s.Seed("t", "b", &testMeta{Name: "b"})
	s.Seed("t", "c", &testMeta{Name: "c"})

	var keys []string
	s.Range(func(tenant, name string, val *testMeta) bool {
		keys = append(keys, tenant+"/"+name)
		return true
	})
	assert.Len(t, keys, 3)
}

func TestStore_Exists(t *testing.T) {
	s, _ := testStore(t)
	assert.False(t, s.Exists("t", "k1"))

	s.Seed("t", "k1", &testMeta{Name: "x"})
	assert.True(t, s.Exists("t", "k1"))

	s.Delete("t", "k1")
	assert.False(t, s.Exists("t", "k1"))
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s, dir := testStore(t)
	entryDir := filepath.Join(dir, "t", "k")
	require.NoError(t, os.MkdirAll(entryDir, 0o755))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := &testMeta{Name: "concurrent", Value: i}
			_ = s.Store("t", "k", m)
			_, _ = s.Get("t", "k")
		}(i)
	}
	wg.Wait()

	got, err := s.Get("t", "k")
	require.NoError(t, err)
	assert.Equal(t, "concurrent", got.Name)
}

func TestStore_Dir(t *testing.T) {
	s := NewStore[testMeta]("/base")
	assert.Equal(t, "/base/tenant/vol1", s.Dir("tenant", "vol1"))

	sSnap := NewStore[testMeta]("/base", "snapshots")
	assert.Equal(t, "/base/tenant/snapshots/snap1", sSnap.Dir("tenant", "snap1"))
}

func TestStore_MetaPath(t *testing.T) {
	s := NewStore[testMeta]("/base")
	assert.Equal(t, "/base/t/v/metadata.json", s.MetaPath("t", "v"))
}

func TestStore_DataPath(t *testing.T) {
	s := NewStore[testMeta]("/base")
	assert.Equal(t, "/base/t/v/data", s.DataPath("t", "v"))
}
