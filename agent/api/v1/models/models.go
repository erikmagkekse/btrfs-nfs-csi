// Package models defines the wire-format types for the btrfs-nfs-csi agent API (v1).
//
// This package is dependency-free (stdlib only) and safe to import from both
// the HTTP client and the server without pulling in either side's dependencies.
//
// # Request types
//
// Request types mirror the storage-layer definitions with identical JSON tags.
// They are independent copies so that client consumers do not need to import
// the storage package.
//
// # Response types
//
// Every list endpoint returns a summary variant and a detail variant.
// Detail responses include all fields (path, labels, etc.), while summary
// responses contain only the most commonly needed fields.
//
// List responses include pagination fields:
//   - Total: total number of items matching the query
//   - Next:  opaque cursor token for the next page (empty on last page)
//
// # Error handling
//
// API errors are represented as [AgentError]. Use [IsConflict], [IsNotFound],
// and [IsLocked] to classify errors by HTTP status.
package models

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// --- Volume requests ---

// VolumeCreateRequest creates a new btrfs subvolume.
// POST /v1/volumes
type VolumeCreateRequest struct {
	Name        string            `json:"name"`             // ^[a-zA-Z0-9_-]{1,128}$
	SizeBytes   uint64            `json:"size_bytes"`       // subvolume size in bytes
	NoCOW       bool              `json:"nocow"`            // disable copy-on-write (chattr +C)
	Compression string            `json:"compression"`      // "zstd", "zstd:3", "zlib", "zlib:5", "lzo", or ""
	QuotaBytes  uint64            `json:"quota_bytes"`      // btrfs qgroup limit (0 = no quota)
	UID         int               `json:"uid"`              // owner UID (0-65534)
	GID         int               `json:"gid"`              // owner GID (0-65534)
	Mode        string            `json:"mode"`             // octal permission string, e.g. "0755" (max 7777)
	Labels      map[string]string `json:"labels,omitempty"` // key: ^[a-z0-9][a-z0-9._-]{0,62}$, value: ^[a-zA-Z0-9._-]{0,128}$
}

// VolumeUpdateRequest patches volume properties. Nil fields are left unchanged.
// PATCH /v1/volumes/:name
type VolumeUpdateRequest struct {
	SizeBytes   *uint64            `json:"size_bytes,omitempty"`  // new size (must be >= current)
	NoCOW       *bool              `json:"nocow,omitempty"`       // toggle copy-on-write
	Compression *string            `json:"compression,omitempty"` // see VolumeCreateRequest.Compression
	UID         *int               `json:"uid,omitempty"`         // new owner UID (0-65534)
	GID         *int               `json:"gid,omitempty"`         // new owner GID (0-65534)
	Mode        *string            `json:"mode,omitempty"`        // octal permission string (max 7777)
	Labels      *map[string]string `json:"labels,omitempty"`      // replaces all labels
}

// --- Snapshot requests ---

// SnapshotCreateRequest creates a read-only btrfs snapshot of a volume.
// POST /v1/snapshots
type SnapshotCreateRequest struct {
	Volume string            `json:"volume"` // source volume name
	Name   string            `json:"name"`   // ^[a-zA-Z0-9_-]{1,128}$
	Labels map[string]string `json:"labels,omitempty"`
}

// --- Clone requests ---

// CloneCreateRequest creates a new volume from a snapshot.
// POST /v1/clones
type CloneCreateRequest struct {
	Snapshot string            `json:"snapshot"` // source snapshot name
	Name     string            `json:"name"`     // ^[a-zA-Z0-9_-]{1,128}$
	Labels   map[string]string `json:"labels,omitempty"`
}

// VolumeCloneRequest creates a new volume from another volume (snapshot + clone).
// POST /v1/volumes/clone
type VolumeCloneRequest struct {
	Source string            `json:"source"` // source volume name
	Name   string            `json:"name"`   // ^[a-zA-Z0-9_-]{1,128}$
	Labels map[string]string `json:"labels,omitempty"`
}

// --- Export requests ---

// VolumeExportCreateRequest adds an NFS export for a volume.
// POST /v1/volumes/:name/export
type VolumeExportCreateRequest struct {
	Client string            `json:"client"` // client IP address (IPv4 or IPv6)
	Labels map[string]string `json:"labels,omitempty"`
}

// VolumeExportDeleteRequest removes an NFS export for a volume.
// DELETE /v1/volumes/:name/export
type VolumeExportDeleteRequest struct {
	Client string            `json:"client"` // client IP address (IPv4 or IPv6)
	Labels map[string]string `json:"labels,omitempty"`
}

// --- Task requests ---

// TaskCreateRequest creates a background task (scrub, test).
// POST /v1/tasks/:type
type TaskCreateRequest struct {
	Timeout string            `json:"timeout,omitempty"` // Go duration string, e.g. "6h", "30m"
	Opts    map[string]string `json:"opts,omitempty"`    // task-specific options
	Labels  map[string]string `json:"labels,omitempty"`
}

// --- Volume responses ---

// VolumeResponse is the summary representation of a volume.
type VolumeResponse struct {
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by,omitempty"`
	SizeBytes uint64    `json:"size_bytes"`
	UsedBytes uint64    `json:"used_bytes"`
	Exports   int       `json:"clients"`
	CreatedAt time.Time `json:"created_at"`
}

// VolumeDetailResponse is the full representation of a volume.
type VolumeDetailResponse struct {
	Name         string                 `json:"name"`
	CreatedBy    string                 `json:"created_by,omitempty"`
	Path         string                 `json:"path"`
	SizeBytes    uint64                 `json:"size_bytes"`
	NoCOW        bool                   `json:"nocow"`
	Compression  string                 `json:"compression"`
	QuotaBytes   uint64                 `json:"quota_bytes"`
	UsedBytes    uint64                 `json:"used_bytes"`
	UID          int                    `json:"uid"`
	GID          int                    `json:"gid"`
	Mode         string                 `json:"mode"`
	Labels       map[string]string      `json:"labels,omitempty"`
	Exports      []ExportDetailResponse `json:"clients"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	LastAttachAt *time.Time             `json:"last_attach_at,omitempty"`
}

// VolumeListResponse is returned by GET /v1/volumes.
type VolumeListResponse struct {
	Volumes []VolumeResponse `json:"volumes"`
	Total   int              `json:"total"`
	Next    string           `json:"next,omitempty"`
}

// VolumeDetailListResponse is returned by GET /v1/volumes?detail=true.
type VolumeDetailListResponse struct {
	Volumes []VolumeDetailResponse `json:"volumes"`
	Total   int                    `json:"total"`
	Next    string                 `json:"next,omitempty"`
}

// --- Snapshot responses ---

// SnapshotResponse is the summary representation of a snapshot.
type SnapshotResponse struct {
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by,omitempty"`
	Volume    string    `json:"volume"`
	SizeBytes uint64    `json:"size_bytes"`
	UsedBytes uint64    `json:"used_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// SnapshotDetailResponse is the full representation of a snapshot.
type SnapshotDetailResponse struct {
	Name           string            `json:"name"`
	CreatedBy      string            `json:"created_by,omitempty"`
	Volume         string            `json:"volume"`
	Path           string            `json:"path"`
	SizeBytes      uint64            `json:"size_bytes"`
	UsedBytes      uint64            `json:"used_bytes"`
	ExclusiveBytes uint64            `json:"exclusive_bytes"`
	// Source volume properties, preserved for clone fallback.
	QuotaBytes  uint64 `json:"quota_bytes,omitempty"` // btrfs qgroup limit from source volume
	NoCOW       bool   `json:"nocow,omitempty"`       // copy-on-write disabled on source volume
	Compression string `json:"compression,omitempty"` // compression algorithm from source volume
	UID         int    `json:"uid,omitempty"`          // owner UID from source volume
	GID         int    `json:"gid,omitempty"`          // owner GID from source volume
	Mode        string `json:"mode,omitempty"`         // permission mode from source volume
	Labels         map[string]string `json:"labels,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// SnapshotListResponse is returned by GET /v1/snapshots.
type SnapshotListResponse struct {
	Snapshots []SnapshotResponse `json:"snapshots"`
	Total     int                `json:"total"`
	Next      string             `json:"next,omitempty"`
}

// SnapshotDetailListResponse is returned by GET /v1/snapshots?detail=true.
type SnapshotDetailListResponse struct {
	Snapshots []SnapshotDetailResponse `json:"snapshots"`
	Total     int                      `json:"total"`
	Next      string                   `json:"next,omitempty"`
}

// --- Export responses ---

// ExportResponse is the summary representation of an NFS export.
type ExportResponse struct {
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by,omitempty"`
	Client    string    `json:"client"`
	CreatedAt time.Time `json:"created_at"`
}

// ExportDetailResponse is the full representation of an NFS export.
type ExportDetailResponse struct {
	Name      string            `json:"name"`
	CreatedBy string            `json:"created_by,omitempty"`
	Client    string            `json:"client"`
	Labels    map[string]string `json:"labels,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// ExportListResponse is returned by GET /v1/exports.
type ExportListResponse struct {
	Exports []ExportResponse `json:"exports"`
	Total   int              `json:"total"`
	Next    string           `json:"next,omitempty"`
}

// ExportDetailListResponse is returned by GET /v1/exports?detail=true.
type ExportDetailListResponse struct {
	Exports []ExportDetailResponse `json:"exports"`
	Total   int                    `json:"total"`
	Next    string                 `json:"next,omitempty"`
}

// --- Stats responses ---

// StatsResponse is returned by GET /v1/stats.
type StatsResponse struct {
	TenantName string                  `json:"tenant_name"`
	Statfs     StatfsResponse          `json:"statfs"`
	Btrfs      FilesystemStatsResponse `json:"btrfs"`
}

// StatfsResponse contains statfs(2) filesystem counters.
type StatfsResponse struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

// DeviceStatsResponse contains per-device statistics.
type DeviceStatsResponse struct {
	DevID          string                `json:"devid"`
	Device         string                `json:"device"`
	Missing        bool                  `json:"missing"`
	SizeBytes      uint64                `json:"size_bytes"`
	AllocatedBytes uint64                `json:"allocated_bytes"`
	IO             DeviceIOStatsResponse `json:"io"`
	Errors         DeviceErrorsResponse  `json:"errors"`
}

// DeviceIOStatsResponse contains I/O counters from /sys/block.
type DeviceIOStatsResponse struct {
	ReadBytesTotal        uint64 `json:"read_bytes_total"`
	ReadIOsTotal          uint64 `json:"read_ios_total"`
	ReadTimeMsTotal       uint64 `json:"read_time_ms_total"`
	WriteBytesTotal       uint64 `json:"write_bytes_total"`
	WriteIOsTotal         uint64 `json:"write_ios_total"`
	WriteTimeMsTotal      uint64 `json:"write_time_ms_total"`
	IOsInProgress         uint64 `json:"ios_in_progress"`
	IOTimeMsTotal         uint64 `json:"io_time_ms_total"`
	WeightedIOTimeMsTotal uint64 `json:"weighted_io_time_ms_total"`
}

// DeviceErrorsResponse contains btrfs device error counters.
type DeviceErrorsResponse struct {
	ReadErrs       uint64 `json:"read_errs"`
	WriteErrs      uint64 `json:"write_errs"`
	FlushErrs      uint64 `json:"flush_errs"`
	CorruptionErrs uint64 `json:"corruption_errs"`
	GenerationErrs uint64 `json:"generation_errs"`
}

// FilesystemStatsResponse contains btrfs filesystem-level statistics.
type FilesystemStatsResponse struct {
	TotalBytes         uint64                `json:"total_bytes"`
	UsedBytes          uint64                `json:"used_bytes"`
	FreeBytes          uint64                `json:"free_bytes"`
	UnallocatedBytes   uint64                `json:"unallocated_bytes"`
	MetadataUsedBytes  uint64                `json:"metadata_used_bytes"`
	MetadataTotalBytes uint64                `json:"metadata_total_bytes"`
	DataRatio          float64               `json:"data_ratio"`
	Devices            []DeviceStatsResponse `json:"devices"`
}

// --- Health response ---

// HealthResponse is returned by GET /healthz (unauthenticated).
type HealthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	UptimeSeconds int    `json:"uptime_seconds"`
}

// --- Task responses ---

// TaskCreateResponse is returned by POST /v1/tasks/:type.
type TaskCreateResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// TaskResponse is the summary representation of a background task.
type TaskResponse struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Status      string            `json:"status"`
	Progress    int               `json:"progress"`
	Opts        map[string]string `json:"opts,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
}

// TaskDetailResponse is the full representation of a background task.
type TaskDetailResponse struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Status      string            `json:"status"`
	Progress    int               `json:"progress"`
	Opts        map[string]string `json:"opts,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
	Result      json.RawMessage   `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
}

// TaskListResponse is returned by GET /v1/tasks.
type TaskListResponse struct {
	Tasks []TaskResponse `json:"tasks"`
	Total int            `json:"total"`
	Next  string         `json:"next,omitempty"`
}

// TaskDetailListResponse is returned by GET /v1/tasks?detail=true.
type TaskDetailListResponse struct {
	Tasks []TaskDetailResponse `json:"tasks"`
	Total int                  `json:"total"`
	Next  string               `json:"next,omitempty"`
}

// --- Error response ---

// ErrorResponse is the JSON body returned on 4xx/5xx errors.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// --- Constants ---

// Health status values returned in [HealthResponse].
const (
	HealthStatusOK       = "ok"
	HealthStatusDegraded = "degraded"
)

// Task status values returned in [TaskResponse] and [TaskDetailResponse].
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// Task type identifiers for POST /v1/tasks/:type.
const (
	TaskTypeScrub = "scrub"
	TaskTypeTest  = "test"
)

// --- Pagination helpers ---

// ListOpts configures list endpoint queries (pagination + label filtering).
type ListOpts struct {
	After  string   // opaque cursor from a previous response's Next field
	Limit  int      // items per page (0 = use client/server default)
	Labels []string // label filters in "key=value" format
}

// Query builds url.Values for a list request. defaultLimit is used when Limit is 0.
func (o ListOpts) Query(defaultLimit int) url.Values {
	q := GenerateLabelQuery(o.Labels)
	if o.After != "" {
		q.Set("after", o.After)
	}
	limit := o.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

// GenerateLabelQuery converts label filters to url.Values with repeated "label" keys.
func GenerateLabelQuery(labels []string) url.Values {
	v := make(url.Values)
	for _, l := range labels {
		v.Add("label", l)
	}
	return v
}

// --- Error types ---

// AgentError represents an HTTP error response from the agent API.
type AgentError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("agent error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// IsConflict reports whether err is a 409 Conflict response.
func IsConflict(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusConflict
	}
	return false
}

// IsNotFound reports whether err is a 404 Not Found response.
func IsNotFound(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusNotFound
	}
	return false
}

// IsLocked reports whether err is a 423 Locked response (e.g. volume has active exports).
func IsLocked(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusLocked
	}
	return false
}
