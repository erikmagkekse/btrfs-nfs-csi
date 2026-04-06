// Package client provides an HTTP client for the btrfs-nfs-csi agent API (v1).
//
// The client handles authentication, automatic pagination with optional
// concurrent prefetch, and identity label injection for all mutating operations.
//
// # Configuration
//
// Use [NewClient] for env-based configuration or [NewClientWithConfig] for
// programmatic setup. Environment variables:
//
//   - AGENT_HTTP_CLIENT_TIMEOUT     -- request timeout (default 30s)
//   - AGENT_HTTP_CLIENT_TLS_SKIP_VERIFY -- skip TLS verification
//   - AGENT_HTTP_CLIENT_PAGE_LIMIT  -- items per page (default 100)
//   - AGENT_HTTP_CLIENT_PREFETCH    -- max pages to prefetch (default 8, 0=sequential)
//   - AGENT_HTTP_CLIENT_PREFETCH_MB -- prefetch byte budget in MB (default 4, 0=unlimited)
//   - AGENT_CSI_IDENTITY            -- identity label value
//
// # Pagination
//
// All List methods auto-paginate internally. Pass fn to process pages as they
// arrive (producer-consumer pipeline with prefetch). Pass nil to collect all
// items into the returned response.
//
//	// Collect all volumes:
//	resp, err := c.ListVolumes(ctx, models.ListOpts{}, nil)
//
//	// Stream pages:
//	resp, err := c.ListVolumes(ctx, models.ListOpts{}, func(page []models.VolumeResponse) error {
//	    for _, v := range page { process(v) }
//	    return nil
//	})
//
// # Idempotent creates
//
// Create operations (volumes, snapshots, clones) return the existing resource
// and a conflict error on duplicates. Use [models.IsConflict] to detect this:
//
//	vol, err := c.CreateVolume(ctx, req)
//	if models.IsConflict(err) {
//	    // vol contains the existing volume
//	}
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	env "github.com/caarlos0/env/v11"
	"github.com/erikmagkekse/btrfs-nfs-csi/agent/api/v1/models"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

// DefaultTimeout is the HTTP client timeout when no explicit timeout is configured.
const DefaultTimeout = 30 * time.Second

// ClientConfig holds configuration for the agent API client.
// Fields are populated from environment variables when using [NewClient].
// Use [DefaultClientConfig] as a starting point for programmatic configuration:
//
//	cfg := client.DefaultClientConfig()
//	cfg.PageLimit = 50
//	c := client.NewClientWithConfig(url, token, cfg)
type ClientConfig struct {
	Timeout       time.Duration `env:"AGENT_HTTP_CLIENT_TIMEOUT" envDefault:"30s"`
	TLSSkipVerify bool          `env:"AGENT_HTTP_CLIENT_TLS_SKIP_VERIFY"`
	Identity      string        `env:"AGENT_CSI_IDENTITY"`
	PageLimit     int           `env:"AGENT_HTTP_CLIENT_PAGE_LIMIT" envDefault:"100"`
	Prefetch      int           `env:"AGENT_HTTP_CLIENT_PREFETCH" envDefault:"8"`
	PrefetchMB    int           `env:"AGENT_HTTP_CLIENT_PREFETCH_MB" envDefault:"4"`
	HTTPClient    *http.Client  `env:"-"`
}

// DefaultClientConfig returns a ClientConfig populated from AGENT_HTTP_CLIENT_*
// and AGENT_CSI_IDENTITY environment variables (falling back to env tag defaults).
// Use this as a base when calling [NewClientWithConfig]:
//
//	cfg := client.DefaultClientConfig()
//	cfg.PageLimit = 50
//	c := client.NewClientWithConfig(url, token, cfg)
func DefaultClientConfig() ClientConfig {
	cfg, err := env.ParseAs[ClientConfig]()
	if err != nil {
		return ClientConfig{
			Timeout:    DefaultTimeout,
			PageLimit:  100,
			Prefetch:   8,
			PrefetchMB: 4,
		}
	}
	return cfg
}

// Client is an HTTP client for the btrfs-nfs-csi agent API.
type Client struct {
	url           string
	token         string
	http          *http.Client
	identity      string
	pageLimit     int
	prefetch      int
	prefetchBytes int64
}

// NewClient creates a client, parsing AGENT_HTTP_CLIENT_* and AGENT_CSI_IDENTITY env vars.
// identity is the fallback when AGENT_CSI_IDENTITY is unset.
func NewClient(url, token, identity string) (*Client, error) {
	cfg, err := env.ParseAs[ClientConfig]()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid client env config: %v, using defaults\n", err)
		cfg = ClientConfig{Timeout: DefaultTimeout}
	}
	if cfg.Identity == "" {
		cfg.Identity = identity
	}
	return newClient(url, token, cfg)
}

// NewClientWithConfig creates a client with explicit configuration.
// Use this when embedding the client in third-party projects.
func NewClientWithConfig(url, token string, cfg ClientConfig) (*Client, error) {
	return newClient(url, token, cfg)
}

func newClient(url, token string, cfg ClientConfig) (*Client, error) {
	if url == "" {
		return nil, fmt.Errorf("agent URL must not be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("agent token must not be empty")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
		if cfg.TLSSkipVerify {
			hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		}
	}
	return &Client{
		url:           url,
		token:         token,
		http:          hc,
		identity:      cfg.Identity,
		pageLimit:     cfg.PageLimit,
		prefetch:      cfg.Prefetch,
		prefetchBytes: int64(cfg.PrefetchMB) * 1024 * 1024,
	}, nil
}

// Identity returns the client's identity label value (e.g. "cli", "k8s").
func (c *Client) Identity() string {
	return c.identity
}

func (c *Client) ensureIdentity(labels map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string)
	}
	if _, ok := labels[config.LabelCreatedBy]; !ok {
		labels[config.LabelCreatedBy] = c.Identity()
	}
	return labels
}

// --- Volumes ---

// CreateVolume creates a btrfs subvolume.
// POST /v1/volumes -- returns the existing volume on conflict (409).
func (c *Client) CreateVolume(ctx context.Context, req models.VolumeCreateRequest) (*models.VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp models.VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/volumes", req, &resp); err != nil {
		if models.IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

// DeleteVolume deletes a volume and its btrfs subvolume.
// DELETE /v1/volumes/:name
func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+name, nil, nil)
}

// UpdateVolume updates volume properties (size, labels, permissions).
// PATCH /v1/volumes/:name
func (c *Client) UpdateVolume(ctx context.Context, name string, req models.VolumeUpdateRequest) (*models.VolumeDetailResponse, error) {
	var resp models.VolumeDetailResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/volumes/"+name, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetVolume returns detailed metadata for a single volume.
// GET /v1/volumes/:name
func (c *Client) GetVolume(ctx context.Context, name string) (*models.VolumeDetailResponse, error) {
	var resp models.VolumeDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListVolumes returns all volumes (summary). Auto-paginates internally.
// GET /v1/volumes
func (c *Client) ListVolumes(ctx context.Context, opts models.ListOpts, fn func([]models.VolumeResponse) error) (*models.VolumeListResponse, error) {
	items, total, err := paginate(ctx, c, "/v1/volumes", opts.Query(c.pageLimit),
		func(raw []byte) ([]models.VolumeResponse, string, int, error) {
			var r models.VolumeListResponse
			return r.Volumes, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.VolumeListResponse{Volumes: items, Total: total}, nil
}

// ListVolumesDetail returns all volumes (full detail). Auto-paginates internally.
// GET /v1/volumes?detail=true
func (c *Client) ListVolumesDetail(ctx context.Context, opts models.ListOpts, fn func([]models.VolumeDetailResponse) error) (*models.VolumeDetailListResponse, error) {
	q := opts.Query(c.pageLimit)
	q.Set("detail", "true")
	items, total, err := paginate(ctx, c, "/v1/volumes", q,
		func(raw []byte) ([]models.VolumeDetailResponse, string, int, error) {
			var r models.VolumeDetailListResponse
			return r.Volumes, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.VolumeDetailListResponse{Volumes: items, Total: total}, nil
}

// --- Snapshots ---

// CreateSnapshot creates a read-only btrfs snapshot of a volume.
// POST /v1/snapshots -- returns the existing snapshot on conflict (409).
func (c *Client) CreateSnapshot(ctx context.Context, req models.SnapshotCreateRequest) (*models.SnapshotDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp models.SnapshotDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/snapshots", req, &resp); err != nil {
		if models.IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

// DeleteSnapshot deletes a snapshot and its btrfs subvolume.
// DELETE /v1/snapshots/:name
func (c *Client) DeleteSnapshot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/snapshots/"+name, nil, nil)
}

// GetSnapshot returns detailed metadata for a single snapshot.
// GET /v1/snapshots/:name
func (c *Client) GetSnapshot(ctx context.Context, name string) (*models.SnapshotDetailResponse, error) {
	var resp models.SnapshotDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/snapshots/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListSnapshots returns all snapshots (summary). Auto-paginates internally.
// GET /v1/snapshots
func (c *Client) ListSnapshots(ctx context.Context, opts models.ListOpts, fn func([]models.SnapshotResponse) error) (*models.SnapshotListResponse, error) {
	items, total, err := paginate(ctx, c, "/v1/snapshots", opts.Query(c.pageLimit),
		func(raw []byte) ([]models.SnapshotResponse, string, int, error) {
			var r models.SnapshotListResponse
			return r.Snapshots, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.SnapshotListResponse{Snapshots: items, Total: total}, nil
}

// ListSnapshotsDetail returns all snapshots (full detail). Auto-paginates internally.
// GET /v1/snapshots?detail=true
func (c *Client) ListSnapshotsDetail(ctx context.Context, opts models.ListOpts, fn func([]models.SnapshotDetailResponse) error) (*models.SnapshotDetailListResponse, error) {
	q := opts.Query(c.pageLimit)
	q.Set("detail", "true")
	items, total, err := paginate(ctx, c, "/v1/snapshots", q,
		func(raw []byte) ([]models.SnapshotDetailResponse, string, int, error) {
			var r models.SnapshotDetailListResponse
			return r.Snapshots, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.SnapshotDetailListResponse{Snapshots: items, Total: total}, nil
}

// ListVolumeSnapshots returns snapshots for a specific volume (summary).
// GET /v1/volumes/:name/snapshots
func (c *Client) ListVolumeSnapshots(ctx context.Context, volume string, opts models.ListOpts, fn func([]models.SnapshotResponse) error) (*models.SnapshotListResponse, error) {
	items, total, err := paginate(ctx, c, "/v1/volumes/"+volume+"/snapshots", opts.Query(c.pageLimit),
		func(raw []byte) ([]models.SnapshotResponse, string, int, error) {
			var r models.SnapshotListResponse
			return r.Snapshots, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.SnapshotListResponse{Snapshots: items, Total: total}, nil
}

// ListVolumeSnapshotsDetail returns snapshots for a specific volume (full detail).
// GET /v1/volumes/:name/snapshots?detail=true
func (c *Client) ListVolumeSnapshotsDetail(ctx context.Context, volume string, opts models.ListOpts, fn func([]models.SnapshotDetailResponse) error) (*models.SnapshotDetailListResponse, error) {
	q := opts.Query(c.pageLimit)
	q.Set("detail", "true")
	items, total, err := paginate(ctx, c, "/v1/volumes/"+volume+"/snapshots", q,
		func(raw []byte) ([]models.SnapshotDetailResponse, string, int, error) {
			var r models.SnapshotDetailListResponse
			return r.Snapshots, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.SnapshotDetailListResponse{Snapshots: items, Total: total}, nil
}

// --- Clones ---

// CreateClone creates a new volume from a snapshot.
// POST /v1/clones -- returns the existing volume on conflict (409).
func (c *Client) CreateClone(ctx context.Context, req models.CloneCreateRequest) (*models.VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp models.VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/clones", req, &resp); err != nil {
		if models.IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

// CloneVolume creates a new volume from another volume (snapshot + clone).
// POST /v1/volumes/clone -- returns the existing volume on conflict (409).
func (c *Client) CloneVolume(ctx context.Context, req models.VolumeCloneRequest) (*models.VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp models.VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/volumes/clone", req, &resp); err != nil {
		if models.IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

// --- Exports ---

// CreateVolumeExport adds an NFS export for a volume to the given client IP.
// POST /v1/volumes/:name/export
func (c *Client) CreateVolumeExport(ctx context.Context, name string, cl string, labels map[string]string) error {
	return c.do(ctx, http.MethodPost, "/v1/volumes/"+name+"/export", models.VolumeExportCreateRequest{Client: cl, Labels: c.ensureIdentity(labels)}, nil)
}

// DeleteVolumeExport removes an NFS export for a volume from the given client IP.
// DELETE /v1/volumes/:name/export
func (c *Client) DeleteVolumeExport(ctx context.Context, name string, cl string, labels map[string]string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+name+"/export", models.VolumeExportDeleteRequest{Client: cl, Labels: labels}, nil)
}

// ListVolumeExports returns all active NFS exports (summary). Auto-paginates internally.
// GET /v1/exports
func (c *Client) ListVolumeExports(ctx context.Context, opts models.ListOpts, fn func([]models.ExportResponse) error) (*models.ExportListResponse, error) {
	items, total, err := paginate(ctx, c, "/v1/exports", opts.Query(c.pageLimit),
		func(raw []byte) ([]models.ExportResponse, string, int, error) {
			var r models.ExportListResponse
			return r.Exports, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.ExportListResponse{Exports: items, Total: total}, nil
}

// ListVolumeExportsDetail returns all active NFS exports (full detail). Auto-paginates internally.
// GET /v1/exports?detail=true
func (c *Client) ListVolumeExportsDetail(ctx context.Context, opts models.ListOpts, fn func([]models.ExportDetailResponse) error) (*models.ExportDetailListResponse, error) {
	q := opts.Query(c.pageLimit)
	q.Set("detail", "true")
	items, total, err := paginate(ctx, c, "/v1/exports", q,
		func(raw []byte) ([]models.ExportDetailResponse, string, int, error) {
			var r models.ExportDetailListResponse
			return r.Exports, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.ExportDetailListResponse{Exports: items, Total: total}, nil
}

// --- Tasks ---

// CreateTask creates a background task (e.g. scrub, test).
// POST /v1/tasks/:type
func (c *Client) CreateTask(ctx context.Context, taskType string, req models.TaskCreateRequest) (*models.TaskCreateResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp models.TaskCreateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/tasks/"+taskType, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTasks returns background tasks (summary). Filter by taskType (empty = all).
// GET /v1/tasks
func (c *Client) ListTasks(ctx context.Context, taskType string, opts models.ListOpts, fn func([]models.TaskResponse) error) (*models.TaskListResponse, error) {
	q := opts.Query(c.pageLimit)
	if taskType != "" {
		q.Set("type", taskType)
	}
	items, total, err := paginate(ctx, c, "/v1/tasks", q,
		func(raw []byte) ([]models.TaskResponse, string, int, error) {
			var r models.TaskListResponse
			return r.Tasks, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.TaskListResponse{Tasks: items, Total: total}, nil
}

// ListTasksDetail returns background tasks (full detail). Filter by taskType (empty = all).
// GET /v1/tasks?detail=true
func (c *Client) ListTasksDetail(ctx context.Context, taskType string, opts models.ListOpts, fn func([]models.TaskDetailResponse) error) (*models.TaskDetailListResponse, error) {
	q := opts.Query(c.pageLimit)
	q.Set("detail", "true")
	if taskType != "" {
		q.Set("type", taskType)
	}
	items, total, err := paginate(ctx, c, "/v1/tasks", q,
		func(raw []byte) ([]models.TaskDetailResponse, string, int, error) {
			var r models.TaskDetailListResponse
			return r.Tasks, r.Next, r.Total, json.Unmarshal(raw, &r)
		}, fn)
	if err != nil {
		return nil, err
	}
	return &models.TaskDetailListResponse{Tasks: items, Total: total}, nil
}

// GetTask returns detailed metadata for a single task.
// GET /v1/tasks/:id
func (c *Client) GetTask(ctx context.Context, id string) (*models.TaskDetailResponse, error) {
	var resp models.TaskDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/tasks/"+id, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelTask cancels a running or pending task.
// DELETE /v1/tasks/:id
func (c *Client) CancelTask(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/tasks/"+id, nil, nil)
}

// --- Stats & Health ---

// Stats returns filesystem and device statistics.
// GET /v1/stats
func (c *Client) Stats(ctx context.Context) (*models.StatsResponse, error) {
	var resp models.StatsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/stats", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Healthz returns agent health status (does not require auth).
// GET /healthz
func (c *Client) Healthz(ctx context.Context) (*models.HealthResponse, error) {
	var resp models.HealthResponse
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// --- Internal ---

// paginate fetches all pages from a list endpoint.
// With fn: producer goroutine prefetches pages, consumer calls fn sequentially.
// Without fn (nil): collects all items into a single slice.
// With prefetch=0: all fetching is sequential regardless of fn.
func paginate[T any](ctx context.Context, c *Client, basePath string, q url.Values,
	extract func([]byte) ([]T, string, int, error),
	fn func([]T) error,
) ([]T, int, error) {
	type page struct {
		items []T
		next  string
		total int
		size  int64
		err   error
	}

	fetch := func(fetchCtx context.Context, pageURL string) page {
		var raw json.RawMessage
		if err := c.do(fetchCtx, http.MethodGet, pageURL, nil, &raw); err != nil {
			return page{err: err}
		}
		items, next, total, err := extract(raw)
		if err != nil {
			return page{err: fmt.Errorf("decode page: %w", err)}
		}
		return page{items: items, next: next, total: total, size: int64(len(raw))}
	}

	buildURL := func(after string) string {
		if after != "" {
			q.Set("after", after)
		}
		return basePath + "?" + q.Encode()
	}

	// With callback + prefetch: producer fetches all pages, consumer calls fn per page.
	if fn != nil && c.prefetch > 0 {
		pipeCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		ch := make(chan page, c.prefetch)
		maxBytes := c.prefetchBytes
		var pending atomic.Int64
		release := make(chan struct{}, 1)

		go func() {
			defer close(ch)
			after := ""
			for {
				if maxBytes > 0 {
					for pending.Load() >= maxBytes {
						select {
						case <-release:
						case <-pipeCtx.Done():
							return
						}
					}
				}
				p := fetch(pipeCtx, buildURL(after))
				if maxBytes > 0 {
					pending.Add(p.size)
				}
				select {
				case ch <- p:
				case <-pipeCtx.Done():
					return
				}
				if p.err != nil || p.next == "" {
					return
				}
				after = p.next
			}
		}()

		var total int
		for p := range ch {
			if p.err != nil {
				return nil, total, p.err
			}
			total = p.total
			if err := fn(p.items); err != nil {
				return nil, total, err
			}
			if maxBytes > 0 {
				pending.Add(-p.size)
				select {
				case release <- struct{}{}:
				default:
				}
			}
		}
		return nil, total, nil
	}

	// Sequential: no prefetch, or no callback.
	var all []T
	var total int
	after := ""
	for {
		p := fetch(ctx, buildURL(after))
		if p.err != nil {
			return nil, 0, p.err
		}
		total = p.total
		if fn != nil {
			if err := fn(p.items); err != nil {
				return nil, total, err
			}
		} else {
			if all == nil && total > 0 {
				all = make([]T, 0, total)
			}
			all = append(all, p.items...)
		}
		if p.next == "" {
			break
		}
		after = p.next
	}
	return all, total, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// on 409 Conflict, parse body into result so caller gets the existing record
		if resp.StatusCode == http.StatusConflict && result != nil && len(respBody) > 0 {
			_ = json.Unmarshal(respBody, result)
		}
		var errResp models.ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &models.AgentError{
				StatusCode: resp.StatusCode,
				Code:       errResp.Code,
				Message:    errResp.Error,
			}
		}
		return &models.AgentError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
