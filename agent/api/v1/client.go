package v1

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
	"strconv"
	"time"

	env "github.com/caarlos0/env/v11"
	"github.com/erikmagkekse/btrfs-nfs-csi/config"
)

func generateLabelQuery(labels []string) url.Values {
	v := make(url.Values)
	for _, l := range labels {
		v.Add("label", l)
	}
	return v
}

type ListOpts struct {
	After  string
	Limit  int
	Labels []string
}

func (o ListOpts) query() url.Values {
	q := generateLabelQuery(o.Labels)
	if o.After != "" {
		q.Set("after", o.After)
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	return q
}

const DefaultTimeout = 30 * time.Second

type ClientConfig struct {
	Timeout       time.Duration `env:"AGENT_HTTP_CLIENT_TIMEOUT" envDefault:"30s"`
	TLSSkipVerify bool          `env:"AGENT_HTTP_CLIENT_TLS_SKIP_VERIFY"`
	Identity      string        `env:"AGENT_CSI_IDENTITY"`
}

type Client struct {
	url      string
	token    string
	http     *http.Client
	identity string
}

// NewClient creates a client, parsing AGENT_HTTP_CLIENT_* and AGENT_CSI_IDENTITY env vars.
// identity is the fallback when AGENT_CSI_IDENTITY is unset.
func NewClient(url, token, identity string) *Client {
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

func newClient(url, token string, cfg ClientConfig) *Client {
	hc := &http.Client{Timeout: cfg.Timeout}
	if cfg.TLSSkipVerify {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &Client{
		url:      url,
		token:    token,
		http:     hc,
		identity: cfg.Identity,
	}
}

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

func (c *Client) CreateVolume(ctx context.Context, req VolumeCreateRequest) (*VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/volumes", req, &resp); err != nil {
		if IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+name, nil, nil)
}

func (c *Client) UpdateVolume(ctx context.Context, name string, req VolumeUpdateRequest) (*VolumeDetailResponse, error) {
	var resp VolumeDetailResponse
	if err := c.do(ctx, http.MethodPatch, "/v1/volumes/"+name, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateSnapshot(ctx context.Context, req SnapshotCreateRequest) (*SnapshotDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp SnapshotDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/snapshots", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteSnapshot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/snapshots/"+name, nil, nil)
}

func (c *Client) CreateClone(ctx context.Context, req CloneCreateRequest) (*VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/clones", req, &resp); err != nil {
		if IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CloneVolume(ctx context.Context, req VolumeCloneRequest) (*VolumeDetailResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp VolumeDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/volumes/clone", req, &resp); err != nil {
		if IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateVolumeExport(ctx context.Context, name string, cl string, labels map[string]string) error {
	return c.do(ctx, http.MethodPost, "/v1/volumes/"+name+"/export", VolumeExportCreateRequest{Client: cl, Labels: c.ensureIdentity(labels)}, nil)
}

func (c *Client) DeleteVolumeExport(ctx context.Context, name string, cl string, labels map[string]string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+name+"/export", VolumeExportDeleteRequest{Client: cl, Labels: labels}, nil)
}

func (c *Client) ListVolumes(ctx context.Context, opts ListOpts) (*VolumeListResponse, error) {
	var resp VolumeListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes?"+opts.query().Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumesDetail(ctx context.Context, opts ListOpts) (*VolumeDetailListResponse, error) {
	var resp VolumeDetailListResponse
	q := opts.query()
	q.Set("detail", "true")
	if err := c.do(ctx, http.MethodGet, "/v1/volumes?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetVolume(ctx context.Context, name string) (*VolumeDetailResponse, error) {
	var resp VolumeDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListSnapshots(ctx context.Context, opts ListOpts) (*SnapshotListResponse, error) {
	var resp SnapshotListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/snapshots?"+opts.query().Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumeSnapshots(ctx context.Context, volume string, opts ListOpts) (*SnapshotListResponse, error) {
	var resp SnapshotListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+volume+"/snapshots?"+opts.query().Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumeSnapshotsDetail(ctx context.Context, volume string, opts ListOpts) (*SnapshotDetailListResponse, error) {
	var resp SnapshotDetailListResponse
	q := opts.query()
	q.Set("detail", "true")
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+volume+"/snapshots?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListSnapshotsDetail(ctx context.Context, opts ListOpts) (*SnapshotDetailListResponse, error) {
	var resp SnapshotDetailListResponse
	q := opts.query()
	q.Set("detail", "true")
	if err := c.do(ctx, http.MethodGet, "/v1/snapshots?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSnapshot(ctx context.Context, name string) (*SnapshotDetailResponse, error) {
	var resp SnapshotDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/snapshots/"+name, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumeExports(ctx context.Context, opts ListOpts) (*ExportListResponse, error) {
	var resp ExportListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/exports?"+opts.query().Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumeExportsDetail(ctx context.Context, opts ListOpts) (*ExportDetailListResponse, error) {
	var resp ExportDetailListResponse
	q := opts.query()
	q.Set("detail", "true")
	if err := c.do(ctx, http.MethodGet, "/v1/exports?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CreateTask(ctx context.Context, taskType string, req TaskCreateRequest) (*TaskCreateResponse, error) {
	req.Labels = c.ensureIdentity(req.Labels)
	var resp TaskCreateResponse
	if err := c.do(ctx, http.MethodPost, "/v1/tasks/"+taskType, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListTasks(ctx context.Context, taskType string, opts ListOpts) (*TaskListResponse, error) {
	var resp TaskListResponse
	q := opts.query()
	if taskType != "" {
		q.Set("type", taskType)
	}
	if err := c.do(ctx, http.MethodGet, "/v1/tasks?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListTasksDetail(ctx context.Context, taskType string, opts ListOpts) (*TaskDetailListResponse, error) {
	var resp TaskDetailListResponse
	q := opts.query()
	q.Set("detail", "true")
	if taskType != "" {
		q.Set("type", taskType)
	}
	if err := c.do(ctx, http.MethodGet, "/v1/tasks?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetTask(ctx context.Context, id string) (*TaskDetailResponse, error) {
	var resp TaskDetailResponse
	if err := c.do(ctx, http.MethodGet, "/v1/tasks/"+id, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CancelTask(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/tasks/"+id, nil, nil)
}

func (c *Client) Stats(ctx context.Context) (*StatsResponse, error) {
	var resp StatsResponse
	if err := c.do(ctx, http.MethodGet, "/v1/stats", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Healthz(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.do(ctx, http.MethodGet, "/healthz", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return &AgentError{
				StatusCode: resp.StatusCode,
				Code:       errResp.Code,
				Message:    errResp.Error,
			}
		}
		return &AgentError{
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

type AgentError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("agent error %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

func IsConflict(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusConflict
	}
	return false
}

func IsNotFound(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusNotFound
	}
	return false
}

func IsLocked(err error) bool {
	if ae, ok := err.(*AgentError); ok {
		return ae.StatusCode == http.StatusLocked
	}
	return false
}
