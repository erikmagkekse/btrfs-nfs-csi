// Package secret manages the agent's persistent installation secret and
// derives purpose-specific subkeys via HKDF. The root secret is entropyLen
// bytes of crypto/rand, persisted on first boot and stable across restarts.
package secret

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

const (
	backupSuffix   = ".bak"
	secretFileMode = 0o600
	entropyLen     = 512

	// purposeFingerprintV1 is the HKDF info string for the fingerprint
	// subkey. Version it (...V2 ...) when the fingerprint format changes
	// so existing deployments keep the same HMAC key.
	purposeFingerprintV1 = "fingerprint-v1"
)

// Manager owns the derived subkeys the agent needs. The root secret is
// not retained after construction.
type Manager struct {
	fpKey []byte
	idCtr atomic.Uint64
}

// NewManager loads or creates the root secret at dir/name and derives the
// subkeys the agent needs. A backup at dir/name.bak is kept in lockstep; a
// primary/backup mismatch aborts startup.
func NewManager(dir, name string) (*Manager, error) {
	root, err := loadOrCreate(dir, name)
	if err != nil {
		return nil, err
	}
	fpKey, err := hkdf.Key(sha256.New, root, nil, purposeFingerprintV1, 32)
	// zeroize the root before returning so it does not linger in the
	// allocator's free list. Not a hard guarantee (Go won't stop the GC
	// from copying beforehand), but shrinks the window.
	for i := range root {
		root[i] = 0
	}
	if err != nil {
		return nil, fmt.Errorf("derive fingerprint key: %w", err)
	}
	return &Manager{fpKey: fpKey}, nil
}

// Fingerprint renders HMAC-SHA256(fpKey, token) as a 64-char hex string.
func (m *Manager) Fingerprint(token string) string {
	h := hmac.New(sha256.New, m.fpKey)
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

// GenerateID returns a hex ID of n bytes (2*n hex chars) derived from
// HMAC(key, counter). No syscalls, no entropy pool, safe for high-frequency
// use. n is clamped to [1, 32].
func (m *Manager) GenerateID(n int) string {
	if n < 1 {
		n = 1
	} else if n > 32 {
		n = 32
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], m.idCtr.Add(1))
	h := hmac.New(sha256.New, m.fpKey)
	h.Write(buf[:])
	var digest [32]byte
	return hex.EncodeToString(h.Sum(digest[:0])[:n])
}

func loadOrCreate(dir, name string) ([]byte, error) {
	primary := filepath.Join(dir, name)
	backup := primary + backupSuffix

	pData, pErr := readExisting(primary)
	if pErr != nil {
		return nil, fmt.Errorf("read primary secret: %w", pErr)
	}
	bData, bErr := readExisting(backup)
	if bErr != nil {
		return nil, fmt.Errorf("read secret backup: %w", bErr)
	}

	switch {
	case pData != nil && bData != nil:
		if !bytes.Equal(pData, bData) {
			return nil, fmt.Errorf("secret mismatch: %s and %s differ, inspect both and remove the stale one before restarting", primary, backup)
		}
		return pData, nil
	case pData != nil:
		if err := writeAtomic(backup, pData, secretFileMode); err != nil {
			return nil, fmt.Errorf("heal secret backup: %w", err)
		}
		return pData, nil
	case bData != nil:
		if err := writeAtomic(primary, bData, secretFileMode); err != nil {
			return nil, fmt.Errorf("heal primary secret: %w", err)
		}
		return bData, nil
	default:
		fresh, err := generate()
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(primary, fresh, secretFileMode); err != nil {
			return nil, fmt.Errorf("persist primary secret: %w", err)
		}
		if err := writeAtomic(backup, fresh, secretFileMode); err != nil {
			return nil, fmt.Errorf("persist secret backup: %w", err)
		}
		return fresh, nil
	}
}

// readExisting distinguishes absent (nil, nil) from broken (nil, err):
// absent is recoverable by restoring from the other replica. Permissions
// are checked: any group/world bits abort startup so a chmod-by-mistake
// (or a malicious actor) can't turn the secret into a leaked credential.
// A truncated file (size below entropyLen, e.g. from a writeAtomic crash
// before rename) is treated as broken so low-entropy input is never fed
// to HKDF as IKM. Larger files are accepted to keep the format
// forward-compatible if entropyLen ever shrinks.
func readExisting(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf("%s has insecure permissions %#o, expected %#o (chmod %#o and restart)", path, mode, secretFileMode, secretFileMode)
	}
	if size := info.Size(); size < entropyLen {
		return nil, fmt.Errorf("%s is %d bytes, expected at least %d (truncated write?), inspect both replicas and remove the broken one before restarting", path, size, entropyLen)
	}
	return os.ReadFile(path)
}

func generate() ([]byte, error) {
	var entropy [entropyLen]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, fmt.Errorf("generate entropy: %w", err)
	}
	return entropy[:], nil
}

// writeAtomic uses temp+rename so a mid-write crash can't leave a truncated
// file that would silently become a new root secret on next boot.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
