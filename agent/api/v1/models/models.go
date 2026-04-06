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
	Name      string    `json:"name"`                 // volume name
	CreatedBy string    `json:"created_by,omitempty"` // identity that created this volume (e.g. "cli", "controller")
	SizeBytes uint64    `json:"size_bytes"`           // subvolume size in bytes
	UsedBytes uint64    `json:"used_bytes"`           // bytes used (btrfs qgroup accounting)
	Exports   int       `json:"clients"`              // number of active NFS exports
	CreatedAt time.Time `json:"created_at"`           // creation timestamp (UTC)
}

// VolumeDetailResponse is the full representation of a volume.
type VolumeDetailResponse struct {
	Name         string                 `json:"name"`                     // volume name
	CreatedBy    string                 `json:"created_by,omitempty"`     // identity that created this volume
	Path         string                 `json:"path"`                     // absolute path on the agent host
	SizeBytes    uint64                 `json:"size_bytes"`               // subvolume size in bytes
	NoCOW        bool                   `json:"nocow"`                    // copy-on-write disabled (chattr +C)
	Compression  string                 `json:"compression"`              // compression algorithm (e.g. "zstd", "zlib", "lzo", "")
	QuotaBytes   uint64                 `json:"quota_bytes"`              // btrfs qgroup limit in bytes
	UsedBytes    uint64                 `json:"used_bytes"`               // bytes used (btrfs qgroup accounting)
	UID          int                    `json:"uid"`                      // owner UID
	GID          int                    `json:"gid"`                      // owner GID
	Mode         string                 `json:"mode"`                     // octal permission string (e.g. "0755")
	Labels       map[string]string      `json:"labels,omitempty"`         // user-defined labels
	Exports      []ExportDetailResponse `json:"clients"`                  // active NFS exports
	CreatedAt    time.Time              `json:"created_at"`               // creation timestamp (UTC)
	UpdatedAt    time.Time              `json:"updated_at"`               // last update timestamp (UTC)
	LastAttachAt *time.Time             `json:"last_attach_at,omitempty"` // last NFS export attach timestamp (UTC)
}

// VolumeListResponse is returned by GET /v1/volumes.
type VolumeListResponse struct {
	Volumes []VolumeResponse `json:"volumes"`        // list of volumes
	Total   int              `json:"total"`          // total number of volumes matching the query
	Next    string           `json:"next,omitempty"` // opaque cursor for the next page (empty on last page)
}

// VolumeDetailListResponse is returned by GET /v1/volumes?detail=true.
type VolumeDetailListResponse struct {
	Volumes []VolumeDetailResponse `json:"volumes"`        // list of volumes with full details
	Total   int                    `json:"total"`          // total number of volumes matching the query
	Next    string                 `json:"next,omitempty"` // opaque cursor for the next page
}

// --- Snapshot responses ---

// SnapshotResponse is the summary representation of a snapshot.
type SnapshotResponse struct {
	Name      string    `json:"name"`                 // snapshot name
	CreatedBy string    `json:"created_by,omitempty"` // identity that created this snapshot
	Volume    string    `json:"volume"`               // source volume name
	SizeBytes uint64    `json:"size_bytes"`           // size in bytes (from source volume at snapshot time)
	UsedBytes uint64    `json:"used_bytes"`           // bytes used (btrfs qgroup accounting)
	CreatedAt time.Time `json:"created_at"`           // creation timestamp (UTC)
}

// SnapshotDetailResponse is the full representation of a snapshot.
type SnapshotDetailResponse struct {
	Name           string `json:"name"`                 // snapshot name
	CreatedBy      string `json:"created_by,omitempty"` // identity that created this snapshot
	Volume         string `json:"volume"`               // source volume name
	Path           string `json:"path"`                 // absolute path on the agent host
	SizeBytes      uint64 `json:"size_bytes"`           // size in bytes (from source volume at snapshot time)
	UsedBytes      uint64 `json:"used_bytes"`           // bytes used (btrfs qgroup accounting)
	ExclusiveBytes uint64 `json:"exclusive_bytes"`      // exclusive bytes (not shared with other snapshots)
	// Source volume properties, preserved for clone fallback.
	QuotaBytes  uint64            `json:"quota_bytes,omitempty"` // btrfs qgroup limit from source volume
	NoCOW       bool              `json:"nocow,omitempty"`       // copy-on-write disabled on source volume
	Compression string            `json:"compression,omitempty"` // compression algorithm from source volume
	UID         int               `json:"uid,omitempty"`         // owner UID from source volume
	GID         int               `json:"gid,omitempty"`         // owner GID from source volume
	Mode        string            `json:"mode,omitempty"`        // permission mode from source volume
	Labels      map[string]string `json:"labels,omitempty"`      // user-defined labels
	CreatedAt   time.Time         `json:"created_at"`            // creation timestamp (UTC)
	UpdatedAt   time.Time         `json:"updated_at"`            // last update timestamp (UTC)
}

// SnapshotListResponse is returned by GET /v1/snapshots.
type SnapshotListResponse struct {
	Snapshots []SnapshotResponse `json:"snapshots"`      // list of snapshots
	Total     int                `json:"total"`          // total number of snapshots matching the query
	Next      string             `json:"next,omitempty"` // opaque cursor for the next page
}

// SnapshotDetailListResponse is returned by GET /v1/snapshots?detail=true.
type SnapshotDetailListResponse struct {
	Snapshots []SnapshotDetailResponse `json:"snapshots"`      // list of snapshots with full details
	Total     int                      `json:"total"`          // total number of snapshots matching the query
	Next      string                   `json:"next,omitempty"` // opaque cursor for the next page
}

// --- Export responses ---

// ExportResponse is the summary representation of an NFS export.
type ExportResponse struct {
	Name      string    `json:"name"`                 // volume name
	CreatedBy string    `json:"created_by,omitempty"` // identity that created this export
	Client    string    `json:"client"`               // client IP address (IPv4 or IPv6)
	CreatedAt time.Time `json:"created_at"`           // creation timestamp (UTC)
}

// ExportDetailResponse is the full representation of an NFS export.
type ExportDetailResponse struct {
	Name      string            `json:"name"`                 // volume name
	CreatedBy string            `json:"created_by,omitempty"` // identity that created this export
	Client    string            `json:"client"`               // client IP address (IPv4 or IPv6)
	Labels    map[string]string `json:"labels,omitempty"`     // user-defined labels
	CreatedAt time.Time         `json:"created_at"`           // creation timestamp (UTC)
}

// ExportListResponse is returned by GET /v1/exports.
type ExportListResponse struct {
	Exports []ExportResponse `json:"exports"`        // list of exports
	Total   int              `json:"total"`          // total number of exports matching the query
	Next    string           `json:"next,omitempty"` // opaque cursor for the next page
}

// ExportDetailListResponse is returned by GET /v1/exports?detail=true.
type ExportDetailListResponse struct {
	Exports []ExportDetailResponse `json:"exports"`        // list of exports with full details
	Total   int                    `json:"total"`          // total number of exports matching the query
	Next    string                 `json:"next,omitempty"` // opaque cursor for the next page
}

// --- Stats responses ---

// StatsResponse is returned by GET /v1/stats.
type StatsResponse struct {
	TenantName string                  `json:"tenant_name"` // tenant name
	Statfs     StatfsResponse          `json:"statfs"`      // statfs(2) counters
	Btrfs      FilesystemStatsResponse `json:"btrfs"`       // btrfs-specific filesystem statistics
}

// StatfsResponse contains statfs(2) filesystem counters.
type StatfsResponse struct {
	TotalBytes uint64 `json:"total_bytes"` // total filesystem size in bytes
	UsedBytes  uint64 `json:"used_bytes"`  // bytes used
	FreeBytes  uint64 `json:"free_bytes"`  // bytes free
}

// DeviceStatsResponse contains per-device statistics.
type DeviceStatsResponse struct {
	DevID          string                `json:"devid"`           // btrfs device ID
	Device         string                `json:"device"`          // block device path (e.g. "/dev/sda1")
	Missing        bool                  `json:"missing"`         // true if device is missing from the filesystem
	SizeBytes      uint64                `json:"size_bytes"`      // device size in bytes
	AllocatedBytes uint64                `json:"allocated_bytes"` // bytes allocated on this device
	IO             DeviceIOStatsResponse `json:"io"`              // I/O counters from /sys/block
	Errors         DeviceErrorsResponse  `json:"errors"`          // btrfs error counters
}

// DeviceIOStatsResponse contains I/O counters from /sys/block.
type DeviceIOStatsResponse struct {
	ReadBytesTotal        uint64 `json:"read_bytes_total"`          // total bytes read
	ReadIOsTotal          uint64 `json:"read_ios_total"`            // total read I/O operations
	ReadTimeMsTotal       uint64 `json:"read_time_ms_total"`        // total read time in milliseconds
	WriteBytesTotal       uint64 `json:"write_bytes_total"`         // total bytes written
	WriteIOsTotal         uint64 `json:"write_ios_total"`           // total write I/O operations
	WriteTimeMsTotal      uint64 `json:"write_time_ms_total"`       // total write time in milliseconds
	IOsInProgress         uint64 `json:"ios_in_progress"`           // I/O operations currently in flight
	IOTimeMsTotal         uint64 `json:"io_time_ms_total"`          // total I/O time in milliseconds
	WeightedIOTimeMsTotal uint64 `json:"weighted_io_time_ms_total"` // weighted I/O time in milliseconds
}

// DeviceErrorsResponse contains btrfs device error counters.
type DeviceErrorsResponse struct {
	ReadErrs       uint64 `json:"read_errs"`       // read errors
	WriteErrs      uint64 `json:"write_errs"`      // write errors
	FlushErrs      uint64 `json:"flush_errs"`      // flush errors
	CorruptionErrs uint64 `json:"corruption_errs"` // data corruption errors
	GenerationErrs uint64 `json:"generation_errs"` // generation mismatch errors
}

// FilesystemStatsResponse contains btrfs filesystem-level statistics.
type FilesystemStatsResponse struct {
	TotalBytes         uint64                `json:"total_bytes"`          // total filesystem size in bytes
	UsedBytes          uint64                `json:"used_bytes"`           // bytes used
	FreeBytes          uint64                `json:"free_bytes"`           // bytes free (total - used)
	UnallocatedBytes   uint64                `json:"unallocated_bytes"`    // bytes not yet allocated to any chunk
	MetadataUsedBytes  uint64                `json:"metadata_used_bytes"`  // metadata bytes used
	MetadataTotalBytes uint64                `json:"metadata_total_bytes"` // metadata bytes allocated
	DataRatio          float64               `json:"data_ratio"`           // data replication ratio (e.g. 1.0 for single, 2.0 for RAID1)
	Devices            []DeviceStatsResponse `json:"devices"`              // per-device statistics
}

// --- Health response ---

// HealthResponse is returned by GET /healthz (unauthenticated).
type HealthResponse struct {
	Status        string `json:"status"`         // "ok" or "degraded"
	Version       string `json:"version"`        // agent version string
	Commit        string `json:"commit"`         // git commit hash
	UptimeSeconds int    `json:"uptime_seconds"` // seconds since agent start
}

// --- Task responses ---

// TaskCreateResponse is returned by POST /v1/tasks/:type.
type TaskCreateResponse struct {
	TaskID string `json:"task_id"` // unique task ID (UUID)
	Status string `json:"status"`  // initial status ("pending")
}

// TaskResponse is the summary representation of a background task.
type TaskResponse struct {
	ID          string            `json:"id"`                     // unique task ID (UUID)
	Type        string            `json:"type"`                   // task type ("scrub", "test")
	CreatedBy   string            `json:"created_by,omitempty"`   // identity that created this task
	Status      string            `json:"status"`                 // "pending", "running", "completed", "failed", "cancelled"
	Progress    int               `json:"progress"`               // completion percentage (0-100)
	Opts        map[string]string `json:"opts,omitempty"`         // task-specific options
	Timeout     string            `json:"timeout,omitempty"`      // Go duration string (e.g. "6h")
	Error       string            `json:"error,omitempty"`        // error message if failed
	CreatedAt   time.Time         `json:"created_at"`             // creation timestamp (UTC)
	StartedAt   *time.Time        `json:"started_at,omitempty"`   // execution start timestamp (UTC)
	CompletedAt *time.Time        `json:"completed_at,omitempty"` // completion timestamp (UTC)
}

// TaskDetailResponse is the full representation of a background task.
type TaskDetailResponse struct {
	ID          string            `json:"id"`                     // unique task ID (UUID)
	Type        string            `json:"type"`                   // task type ("scrub", "test")
	CreatedBy   string            `json:"created_by,omitempty"`   // identity that created this task
	Status      string            `json:"status"`                 // "pending", "running", "completed", "failed", "cancelled"
	Progress    int               `json:"progress"`               // completion percentage (0-100)
	Opts        map[string]string `json:"opts,omitempty"`         // task-specific options
	Labels      map[string]string `json:"labels,omitempty"`       // user-defined labels
	Timeout     string            `json:"timeout,omitempty"`      // Go duration string (e.g. "6h")
	Result      json.RawMessage   `json:"result,omitempty"`       // task-specific result payload (JSON)
	Error       string            `json:"error,omitempty"`        // error message if failed
	CreatedAt   time.Time         `json:"created_at"`             // creation timestamp (UTC)
	StartedAt   *time.Time        `json:"started_at,omitempty"`   // execution start timestamp (UTC)
	CompletedAt *time.Time        `json:"completed_at,omitempty"` // completion timestamp (UTC)
}

// TaskListResponse is returned by GET /v1/tasks.
type TaskListResponse struct {
	Tasks []TaskResponse `json:"tasks"`          // list of tasks
	Total int            `json:"total"`          // total number of tasks matching the query
	Next  string         `json:"next,omitempty"` // opaque cursor for the next page
}

// TaskDetailListResponse is returned by GET /v1/tasks?detail=true.
type TaskDetailListResponse struct {
	Tasks []TaskDetailResponse `json:"tasks"`          // list of tasks with full details
	Total int                  `json:"total"`          // total number of tasks matching the query
	Next  string               `json:"next,omitempty"` // opaque cursor for the next page
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
	Limit  int      // items per page (0 = pagination disabled, negative = use client default)
	Labels []string // label filters in "key=value" format
}

// Query builds url.Values for a list request. defaultLimit is used when Limit is negative.
func (o ListOpts) Query(defaultLimit int) url.Values {
	q := GenerateLabelQuery(o.Labels)
	if o.After != "" {
		q.Set("after", o.After)
	}
	limit := o.Limit
	if limit < 0 {
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
