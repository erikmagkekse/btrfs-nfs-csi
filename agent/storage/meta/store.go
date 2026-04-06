package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
	"github.com/rs/zerolog/log"
)

const (
	fsIocGetFlags = 0x80086601 // FS_IOC_GETFLAGS
	fsIocSetFlags = 0x40086602 // FS_IOC_SETFLAGS
	fsImmutableFL = 0x00000010 // FS_IMMUTABLE_FL
)

func setImmutable(path string) { toggleImmutable(path, true) }

// ClearImmutable removes the immutable flag from a file.
func ClearImmutable(path string) { toggleImmutable(path, false) }

func toggleImmutable(path string, set bool) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	var flags int
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocGetFlags, uintptr(unsafe.Pointer(&flags)))
	if set {
		flags |= fsImmutableFL
	} else {
		flags &^= fsImmutableFL
	}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocSetFlags, uintptr(unsafe.Pointer(&flags)))
}

// Store is a generic in-memory metadata cache backed by disk.
// All reads go through cache (with disk fallback on miss).
// All writes update both disk and cache atomically.
//
// Path layout: basePath/tenant/[pathSegments...]/name/
//   - metadata at: Dir/config.MetadataFile
//   - data at:     Dir/config.DataDir
type Store[T any] struct {
	cache        sync.Map
	basePath     string
	pathSegments []string
}

// NewStore creates a path-aware metadata store.
// segments are optional path components between tenant and name
// (e.g. config.SnapshotsDir for snapshots).
func NewStore[T any](basePath string, segments ...string) *Store[T] {
	return &Store[T]{basePath: basePath, pathSegments: segments}
}

func (s *Store[T]) key(tenant, name string) string {
	return tenant + "/" + name
}

// Dir returns the base directory for the given entry.
func (s *Store[T]) Dir(tenant, name string) string {
	parts := make([]string, 0, 3+len(s.pathSegments))
	parts = append(parts, s.basePath, tenant)
	parts = append(parts, s.pathSegments...)
	parts = append(parts, name)
	return filepath.Join(parts...)
}

// MetaPath returns the metadata file path for the given entry.
func (s *Store[T]) MetaPath(tenant, name string) string {
	return filepath.Join(s.Dir(tenant, name), config.MetadataFile)
}

// DataPath returns the data directory path for the given entry.
func (s *Store[T]) DataPath(tenant, name string) string {
	return filepath.Join(s.Dir(tenant, name), config.DataDir)
}

// Seed populates the cache without writing to disk. Used at startup.
func (s *Store[T]) Seed(tenant, name string, val *T) {
	cp := *val
	s.cache.Store(s.key(tenant, name), &cp)
}

// Get returns metadata from cache. On cache miss, falls back to disk,
// logs a warning (indicates a bug or external change), and populates the cache.
func (s *Store[T]) Get(tenant, name string) (*T, error) {
	k := s.key(tenant, name)
	if v, ok := s.cache.Load(k); ok {
		return v.(*T), nil
	}
	diskPath := s.MetaPath(tenant, name)
	var val T
	if err := readJSON(diskPath, &val); err != nil {
		return nil, err
	}
	log.Warn().Str("key", k).Msg("metadata cache miss, loaded from disk")
	s.cache.Store(k, &val)
	return &val, nil
}

// Store writes metadata to disk (atomic) and updates the cache.
func (s *Store[T]) Store(tenant, name string, val *T) error {
	if err := writeJSONAtomic(s.MetaPath(tenant, name), val); err != nil {
		return err
	}
	cp := *val
	s.cache.Store(s.key(tenant, name), &cp)
	return nil
}

// Update performs a read-modify-write with per-path locking.
// Reads from cache (disk fallback), applies fn, writes to disk, updates cache.
func (s *Store[T]) Update(tenant, name string, fn func(*T)) (*T, error) {
	diskPath := s.MetaPath(tenant, name)
	rm := pathLock(diskPath)
	defer pathUnlock(diskPath, rm)

	val, err := s.Get(tenant, name)
	if err != nil {
		return nil, err
	}
	cp := *val
	fn(&cp)
	if err := writeJSONAtomic(diskPath, &cp); err != nil {
		return nil, err
	}
	s.cache.Store(s.key(tenant, name), &cp)
	return &cp, nil
}

// Delete removes an entry from cache and clears the immutable flag on disk.
func (s *Store[T]) Delete(tenant, name string) {
	ClearImmutable(s.MetaPath(tenant, name))
	s.cache.Delete(s.key(tenant, name))
}

// Range iterates over all cached entries.
func (s *Store[T]) Range(fn func(tenant, name string, val *T) bool) {
	s.cache.Range(func(k, v any) bool {
		tenant, name, _ := strings.Cut(k.(string), "/")
		return fn(tenant, name, v.(*T))
	})
}

// LoadFromDisk reads metadata from disk and seeds the cache. Used at startup.
func (s *Store[T]) LoadFromDisk(tenant, name string) (*T, error) {
	diskPath := s.MetaPath(tenant, name)
	var val T
	if err := readJSON(diskPath, &val); err != nil {
		return nil, err
	}
	setImmutable(diskPath)
	s.cache.Store(s.key(tenant, name), &val)
	return &val, nil
}

// Exists returns true if the key is in the cache.
func (s *Store[T]) Exists(tenant, name string) bool {
	_, ok := s.cache.Load(s.key(tenant, name))
	return ok
}

// per-path mutex pool for serializing read-modify-write operations
var (
	locksMu  sync.Mutex
	locksMap = map[string]*refMutex{}
)

type refMutex struct {
	mu   sync.Mutex
	refs int
}

func pathLock(path string) *refMutex {
	locksMu.Lock()
	rm, ok := locksMap[path]
	if !ok {
		rm = &refMutex{}
		locksMap[path] = rm
	}
	rm.refs++
	locksMu.Unlock()
	rm.mu.Lock()
	return rm
}

func pathUnlock(path string, rm *refMutex) {
	rm.mu.Unlock()
	locksMu.Lock()
	rm.refs--
	if rm.refs == 0 {
		delete(locksMap, path)
	}
	locksMu.Unlock()
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}

	ClearImmutable(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	setImmutable(path)
	return nil
}
