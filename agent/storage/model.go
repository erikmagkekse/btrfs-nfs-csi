package storage

import (
	"encoding/json"
	"maps"
	"sort"
	"time"

	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

// Persisted metadata types

type VolumeMetadata struct {
	Name         string            `json:"name"`
	Path         string            `json:"path"`
	SizeBytes    uint64            `json:"size_bytes"`
	NoCOW        bool              `json:"nocow"`
	Compression  string            `json:"compression"`
	QuotaBytes   uint64            `json:"quota_bytes"`
	UsedBytes    uint64            `json:"used_bytes"`
	UID          int               `json:"uid"`
	GID          int               `json:"gid"`
	Mode         string            `json:"mode"`
	Labels       map[string]string `json:"labels,omitempty"`
	Exports      []ExportMetadata       `json:"clients,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LastAttachAt *time.Time        `json:"last_attach_at,omitempty"`
}

type SnapshotMetadata struct {
	Name           string            `json:"name"`
	Volume         string            `json:"volume"`
	Path           string            `json:"path"`
	SizeBytes      uint64            `json:"size_bytes"`
	UsedBytes      uint64            `json:"used_bytes"`
	ExclusiveBytes uint64            `json:"exclusive_bytes"`
	ReadOnly       bool              `json:"readonly"`
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// Request types

type VolumeCreateRequest struct {
	Name        string            `json:"name"`
	SizeBytes   uint64            `json:"size_bytes"`
	NoCOW       bool              `json:"nocow"`
	Compression string            `json:"compression"`
	QuotaBytes  uint64            `json:"quota_bytes"`
	UID         int               `json:"uid"`
	GID         int               `json:"gid"`
	Mode        string            `json:"mode"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type VolumeUpdateRequest struct {
	SizeBytes   *uint64            `json:"size_bytes,omitempty"`
	NoCOW       *bool              `json:"nocow,omitempty"`
	Compression *string            `json:"compression,omitempty"`
	UID         *int               `json:"uid,omitempty"`
	GID         *int               `json:"gid,omitempty"`
	Mode        *string            `json:"mode,omitempty"`
	Labels      *map[string]string `json:"labels,omitempty"`
}

type SnapshotCreateRequest struct {
	Volume string            `json:"volume"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

type CloneCreateRequest struct {
	Snapshot string            `json:"snapshot"`
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels,omitempty"`
}

type VolumeCloneRequest struct {
	Source string            `json:"source"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

func (m VolumeMetadata) GetLabels() map[string]string   { return m.Labels }
func (m SnapshotMetadata) GetLabels() map[string]string { return m.Labels }

type PaginatedResult[T any] struct {
	Items []T
	Total int
	Next  string // cursor for next page, empty = end of list
}

func paginateSlice[T any](items []T, keyFn func(T) string, after string, limit int) *PaginatedResult[T] {
	total := len(items)
	if after != "" {
		for i, item := range items {
			if keyFn(item) > after {
				items = items[i:]
				break
			}
			if i == len(items)-1 {
				return &PaginatedResult[T]{Total: total}
			}
		}
	}
	result := &PaginatedResult[T]{Total: total}
	if limit > 0 && len(items) > limit {
		result.Next = keyFn(items[limit-1])
		result.Items = items[:limit]
	} else {
		result.Items = items
	}
	return result
}

type ExportEntry struct {
	Name      string
	Client    string
	Labels    map[string]string
	CreatedAt time.Time
}

func (e ExportEntry) GetLabels() map[string]string { return e.Labels }

type ExportMetadata struct {
	IP        string            `json:"ip"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

func uniqueExportIPs(clients []ExportMetadata) []string {
	seen := map[string]struct{}{}
	for _, c := range clients {
		seen[c.IP] = struct{}{}
	}
	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips
}

func CountUniqueExportIPs(clients []ExportMetadata) int {
	seen := map[string]struct{}{}
	for _, c := range clients {
		seen[c.IP] = struct{}{}
	}
	return len(seen)
}

func hasExport(clients []ExportMetadata, ip string, labels map[string]string) bool {
	for _, c := range clients {
		if c.IP == ip && maps.Equal(c.Labels, labels) {
			return true
		}
	}
	return false
}

func exportsForIP(clients []ExportMetadata, ip string) int {
	n := 0
	for _, c := range clients {
		if c.IP == ip {
			n++
		}
	}
	return n
}

// labelsContain reports whether stored contains all key-value pairs from match.
func labelsContain(stored, match map[string]string) bool {
	for k, v := range match {
		if stored[k] != v {
			return false
		}
	}
	return true
}

func (m *VolumeMetadata) UnmarshalJSON(data []byte) error {
	type Alias VolumeMetadata
	aux := &struct {
		Exports json.RawMessage `json:"clients,omitempty"`
		*Alias
	}{Alias: (*Alias)(m)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if len(aux.Exports) == 0 {
		return nil
	}
	var refs []ExportMetadata
	if err := json.Unmarshal(aux.Exports, &refs); err == nil {
		m.Exports = refs
		return nil
	}
	var ips []string
	if err := json.Unmarshal(aux.Exports, &ips); err != nil {
		return err
	}
	m.Exports = make([]ExportMetadata, len(ips))
	for i, ip := range ips {
		m.Exports[i] = ExportMetadata{
			IP:        ip,
			Labels:    map[string]string{config.LabelCreatedBy: "migrated"},
			CreatedAt: time.Now().UTC(),
		}
	}
	return nil
}
