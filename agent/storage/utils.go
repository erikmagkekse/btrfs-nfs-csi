package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// --- Error types ---

const (
	ErrInvalid       = "INVALID"
	ErrNotFound      = "NOT_FOUND"
	ErrAlreadyExists = "ALREADY_EXISTS"
)

type StorageError struct {
	Code    string
	Message string
}

func (e *StorageError) Error() string { return e.Message }

// --- Validation ---

var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateName(name string) error {
	if !validName.MatchString(name) {
		return &StorageError{Code: ErrInvalid, Message: fmt.Sprintf("invalid name: %q (must be 1-64 chars, only a-z A-Z 0-9 _ -)", name)}
	}
	return nil
}

var validCompressionAlgo = map[string]bool{
	"zstd": true,
	"lzo":  true,
	"zlib": true,
}

func isValidCompression(s string) bool {
	if s == "" || s == "none" {
		return true
	}
	parts := strings.SplitN(s, ":", 2)
	if !validCompressionAlgo[parts[0]] {
		return false
	}
	if len(parts) == 2 {
		level, err := strconv.Atoi(parts[1])
		if err != nil || level < 1 || level > 15 {
			return false
		}
	}
	return true
}

// --- File mode ---

// fileMode converts a traditional Unix octal mode (e.g. 0o2750) to an os.FileMode.
// Go's os.FileMode uses its own bit layout for setuid/setgid/sticky, so passing
// a raw Unix octal value like os.FileMode(0o2770) silently drops the special bits.
// See https://pkg.go.dev/os#FileMode and https://github.com/golang/go/issues/44575.
func fileMode(unixMode uint64) os.FileMode {
	m := os.FileMode(unixMode & 0o777)
	if unixMode&0o4000 != 0 {
		m |= os.ModeSetuid
	}
	if unixMode&0o2000 != 0 {
		m |= os.ModeSetgid
	}
	if unixMode&0o1000 != 0 {
		m |= os.ModeSticky
	}
	return m
}

func unixMode(m os.FileMode) uint64 {
	mode := uint64(m.Perm())
	if m&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if m&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if m&os.ModeSticky != 0 {
		mode |= 0o1000
	}
	return mode
}

// --- Metadata IO ---

// ghetto mutex pool - because sync.Map told us "i'll hold your locks forever babe"
// and we believed it until OOM said otherwise. ref-counted so we don't
// accidentally give two goroutines different locks for the same path.
var metaLocksMu sync.Mutex
var metaLocksMap = map[string]*refMutex{}

type refMutex struct {
	mu   sync.Mutex
	refs int
}

func metaLock(path string) *refMutex {
	metaLocksMu.Lock()
	rm, ok := metaLocksMap[path]
	if !ok {
		rm = &refMutex{}
		metaLocksMap[path] = rm
	}
	rm.refs++
	metaLocksMu.Unlock()
	rm.mu.Lock()
	return rm
}

func metaUnlock(path string, rm *refMutex) {
	rm.mu.Unlock()
	metaLocksMu.Lock()
	rm.refs--
	if rm.refs == 0 {
		delete(metaLocksMap, path)
	}
	metaLocksMu.Unlock()
}

func writeMetadataAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadMetadata(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func UpdateMetadata[T any](path string, fn func(*T)) error {
	rm := metaLock(path)
	defer metaUnlock(path, rm)

	var meta T
	if err := ReadMetadata(path, &meta); err != nil {
		return err
	}
	fn(&meta)
	return writeMetadataAtomic(path, &meta)
}
