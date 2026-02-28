package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	url   string
	token string
	http  *http.Client
}

func NewClient(url, token string) *Client {
	return &Client{
		url:   url,
		token: token,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) CreateVolume(ctx context.Context, req VolumeCreateRequest) (*VolumeDetailResponse, error) {
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
	var resp SnapshotDetailResponse
	if err := c.do(ctx, http.MethodPost, "/v1/snapshots", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteSnapshot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/snapshots/"+name, nil, nil)
}

func (c *Client) CreateClone(ctx context.Context, req CloneCreateRequest) (*CloneResponse, error) {
	var resp CloneResponse
	if err := c.do(ctx, http.MethodPost, "/v1/clones", req, &resp); err != nil {
		if IsConflict(err) {
			return &resp, err
		}
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ExportVolume(ctx context.Context, name string, cl string) error {
	return c.do(ctx, http.MethodPost, "/v1/volumes/"+name+"/export", ExportRequest{Client: cl}, nil)
}

func (c *Client) UnexportVolume(ctx context.Context, name string, cl string) error {
	return c.do(ctx, http.MethodDelete, "/v1/volumes/"+name+"/export", ExportRequest{Client: cl}, nil)
}

func (c *Client) ListVolumes(ctx context.Context) (*VolumeListResponse, error) {
	var resp VolumeListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes", nil, &resp); err != nil {
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

func (c *Client) ListSnapshots(ctx context.Context) (*SnapshotListResponse, error) {
	var resp SnapshotListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/snapshots", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVolumeSnapshots(ctx context.Context, volume string) (*SnapshotListResponse, error) {
	var resp SnapshotListResponse
	if err := c.do(ctx, http.MethodGet, "/v1/volumes/"+volume+"/snapshots", nil, &resp); err != nil {
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
