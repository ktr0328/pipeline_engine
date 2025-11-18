package gosdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
)

// Client is a tiny helper for invoking the Pipeline Engine HTTP API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// RerunRequest mirrors the server payload for rerunning jobs from a specific step.
type RerunRequest struct {
	FromStepID    *engine.StepID   `json:"from_step_id,omitempty"`
	ReuseUpstream bool             `json:"reuse_upstream,omitempty"`
	OverrideInput *engine.JobInput `json:"override_input,omitempty"`
}

// NewClient creates a client using the supplied baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// CreateJob sends a POST /v1/jobs request.
func (c *Client) CreateJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
	return c.postJob(ctx, "/v1/jobs", req)
}

// GetJob retrieves a job via GET /v1/jobs/{id}.
func (c *Client) GetJob(ctx context.Context, jobID string) (*engine.Job, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s", c.BaseURL, jobID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	return decodeJob(resp.Body)
}

// CancelJob cancels the job via POST /v1/jobs/{id}/cancel.
func (c *Client) CancelJob(ctx context.Context, jobID string, reason string) (*engine.Job, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s/cancel", c.BaseURL, jobID)
	payload := map[string]string{"reason": reason}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	return decodeJob(resp.Body)
}

// RerunJob triggers POST /v1/jobs/{id}/rerun to create a follow-up job.
func (c *Client) RerunJob(ctx context.Context, jobID string, payload RerunRequest) (*engine.Job, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s/rerun", c.BaseURL, jobID)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	return decodeJob(resp.Body)
}

func (c *Client) postJob(ctx context.Context, path string, req engine.JobRequest) (*engine.Job, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	return decodeJob(resp.Body)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	return c.HTTPClient
}

type jobEnvelope struct {
	Job engine.Job `json:"job"`
}

func decodeJob(body io.Reader) (*engine.Job, error) {
	var resp jobEnvelope
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp.Job, nil
}
