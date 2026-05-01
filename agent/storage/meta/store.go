package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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
		if !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", path).Bool("set", set).Msg("immutable: open failed")
		}
		return
	}
	defer func() { _ = f.Close() }()
	var flags int
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocGetFlags, uintptr(unsafe.Pointer(&flags))); errno != 0 {
		log.Warn().Err(errno).Str("path", path).Msg("immutable: FS_IOC_GETFLAGS failed")
		return
	}
	if set {
		flags |= fsImmutableFL
	} else {
		flags &^= fsImmutableFL
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fsIocSetFlags, uintptr(unsafe.Pointer(&flags))); errno != 0 {
		log.Warn().Err(errno).Str("path", path).Bool("set", set).Msg("immutable: FS_IOC_SETFLAGS failed")
	}
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
//
// Returns an error if tenant or name are not local paths (absolute, empty,
// or contain ".."). Callers should still validate inputs upstream via
// config.ValidateTenantName and config.ValidateSubvolumeName, both of which
// restrict to a strict regex. The filepath.IsLocal guard here is defence in
// depth so that any future weakening of upstream validation cannot turn this
// helper into a path traversal sink, and it lets static analysers recognise
// the sanitiser.
func (s *Store[T]) Dir(tenant, name string) (string, error) {
	if !filepath.IsLocal(tenant) || !filepath.IsLocal(name) {
		return "", fmt.Errorf("storage/meta: non-local path component: tenant=%q name=%q", tenant, name)
	}
	parts := make([]string, 0, 3+len(s.pathSegments))
	parts = append(parts, s.basePath, tenant)
	parts = append(parts, s.pathSegments...)
	parts = append(parts, name)
	return filepath.Join(parts...), nil
}

// MetaPath returns the metadata file path for the given entry.
func (s *Store[T]) MetaPath(tenant, name string) (string, error) {
	dir, err := s.Dir(tenant, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, config.MetadataFile), nil
}

// DataPath returns the data directory path for the given entry.
func (s *Store[T]) DataPath(tenant, name string) (string, error) {
	dir, err := s.Dir(tenant, name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, config.DataDir), nil
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
	diskPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return nil, err
	}
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
	metaPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(metaPath, val); err != nil {
		return err
	}
	cp := *val
	s.cache.Store(s.key(tenant, name), &cp)
	return nil
}

// Lock acquires the exclusive per-entry lock used by Update, and returns
// an unlock function the caller must invoke (typically via defer).
//
// Create-style callers should hold this lock across their cache check,
// filesystem operations, and Store, so concurrent creators of the same
// name are serialized and the losers observe the winner's cache entry
// on their second Get. Uses the same refcounted path mutex pool as
// Update, so Create and Update on the same entry are mutually exclusive.
//
// Lock respects ctx, returning ctx.Err() on cancel or timeout instead of
// blocking; callers translate that to BUSY. Request contexts usually have
// no deadline, so lockAcquireTimeout caps the wait at 30s.
const lockAcquireTimeout = 30 * time.Second

func (s *Store[T]) Lock(ctx context.Context, tenant, name string) (func(), error) {
	metaPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > lockAcquireTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lockAcquireTimeout)
		defer cancel()
	}
	return pathLockCtx(ctx, metaPath)
}

// Update performs a read-modify-write with per-path locking.
// Reads from cache (disk fallback), applies fn, writes to disk, updates cache.
func (s *Store[T]) Update(tenant, name string, fn func(*T)) (*T, error) {
	metaPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return nil, err
	}
	release := pathLock(metaPath)
	defer release()
	return s.UpdateLocked(tenant, name, fn)
}

// UpdateLocked is Update without acquiring the path lock. Caller must already
// hold it (via Lock()) to serialize the entire operation, e.g. when multiple
// reads and external side effects need to be consistent with the final write.
func (s *Store[T]) UpdateLocked(tenant, name string, fn func(*T)) (*T, error) {
	diskPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return nil, err
	}
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
// Returns true if the entry was present in the cache, false otherwise so
// callers can distinguish a real removal from a no-op. Skips the disk step
// silently if (tenant, name) is not a valid path.
func (s *Store[T]) Delete(tenant, name string) bool {
	if metaPath, err := s.MetaPath(tenant, name); err == nil {
		ClearImmutable(metaPath)
	}
	_, loaded := s.cache.LoadAndDelete(s.key(tenant, name))
	return loaded
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
	diskPath, err := s.MetaPath(tenant, name)
	if err != nil {
		return nil, err
	}
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

// per-path mutex pool for serializing read-modify-write operations.
//
// refMutex uses a buffered channel of size 1 as a semaphore instead of
// sync.Mutex, so waiters can select on context cancellation. A non-nil
// token in the channel means "unlocked"; Lock drains, Unlock refills.
var (
	locksMu  sync.Mutex
	locksMap = map[string]*refMutex{}
)

type refMutex struct {
	ch   chan struct{}
	refs int
}

// pathLockShared bumps refcount and returns (or creates) the refMutex for path.
func pathLockShared(path string) *refMutex {
	locksMu.Lock()
	rm, ok := locksMap[path]
	if !ok {
		rm = &refMutex{ch: make(chan struct{}, 1)}
		rm.ch <- struct{}{} // start in the unlocked state
		locksMap[path] = rm
	}
	rm.refs++
	locksMu.Unlock()
	return rm
}

// makeRelease returns the unlock closure that refills the semaphore and
// drops the refcount. Order matters: the chan refill must happen before
// releaseRef so any waiter observes the free state before the entry can
// be garbage-collected from locksMap.
func makeRelease(path string, rm *refMutex) func() {
	return func() {
		rm.ch <- struct{}{}
		releaseRef(path, rm)
	}
}

// pathLock blocks until the per-path semaphore is acquired. Used by Update
// and other callers that do not have a cancellable context.
func pathLock(path string) func() {
	rm := pathLockShared(path)
	<-rm.ch
	return makeRelease(path, rm)
}

// pathLockCtx blocks until the per-path semaphore is acquired or ctx is
// done, whichever happens first. On ctx cancellation the refcount is
// decremented so the map does not leak, and the caller receives ctx.Err().
func pathLockCtx(ctx context.Context, path string) (func(), error) {
	rm := pathLockShared(path)
	select {
	case <-rm.ch:
		return makeRelease(path, rm), nil
	case <-ctx.Done():
		releaseRef(path, rm)
		return nil, ctx.Err()
	}
}

// releaseRef drops one reference to rm and removes it from locksMap when
// the last waiter is gone. Caller must NOT hold locksMu.
func releaseRef(path string, rm *refMutex) {
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
