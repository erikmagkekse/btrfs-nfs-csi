package storage

import "time"

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
	Clients      []string          `json:"clients,omitempty"`
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
	Path   string `json:"path"`
	Client string `json:"client"`
}
