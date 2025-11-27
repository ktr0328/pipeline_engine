package gosdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// StreamJobs starts a streaming job by sending `POST /v1/jobs?stream=true` and
// returns a channel of StreamingEvent plus the accepted Job.
func (c *Client) StreamJobs(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, err
	}

	url := c.BaseURL + "/v1/jobs?stream=true"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, nil, err
	}

	jobLine, eventsCh, closeFn, err := readNDJSONStream(resp)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	var jobEvent engine.StreamingEvent
	if err := json.Unmarshal(jobLine, &jobEvent); err != nil {
		closeFn()
		return nil, nil, err
	}
	job, ok := jobEvent.Data.(map[string]interface{})
	if !ok {
		closeFn()
		return nil, nil, errors.New("invalid job_queued payload")
	}
	jobBytes, err := json.Marshal(job)
	if err != nil {
		closeFn()
		return nil, nil, err
	}
	var jobStruct engine.Job
	if err := json.Unmarshal(jobBytes, &jobStruct); err != nil {
		closeFn()
		return nil, nil, err
	}
	return eventsCh, &jobStruct, nil
}

func readNDJSONStream(resp *http.Response) ([]byte, chan engine.StreamingEvent, func(), error) {
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, nil, nil, fmt.Errorf("http error: %s", resp.Status)
	}
	reader := bufio.NewReader(resp.Body)
	firstLine, err := reader.ReadBytes('\n')
	if err != nil {
		resp.Body.Close()
		return nil, nil, nil, err
	}

	ch := make(chan engine.StreamingEvent)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		for {
			line, err := reader.ReadBytes('\n')
			if len(strings.TrimSpace(string(line))) == 0 && err == io.EOF {
				return
			}
			if len(line) == 0 {
				if err != nil {
					return
				}
				continue
			}
			var evt engine.StreamingEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr != nil {
				return
			}
			ch <- evt
			if err != nil {
				return
			}
		}
	}()
	return bytes.TrimSpace(firstLine), ch, func() {
		resp.Body.Close()
	}, nil
}

// UpsertProviderProfile calls POST /v1/config/providers to register or update a profile.
func (c *Client) UpsertProviderProfile(ctx context.Context, profile engine.ProviderProfile) error {
	body, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	url := c.BaseURL + "/v1/config/providers"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http error: %s", resp.Status)
	}
	return nil
}

// StreamJobByID fetches NDJSON events for an existing job via GET /v1/jobs/{id}/stream.
func (c *Client) StreamJobByID(ctx context.Context, jobID string) ([]engine.StreamingEvent, error) {
	url := fmt.Sprintf("%s/v1/jobs/%s/stream", c.BaseURL, jobID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http error: %s", resp.Status)
	}

	reader := bufio.NewReader(resp.Body)
	var events []engine.StreamingEvent
	for {
		line, err := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) == 0 && err == io.EOF {
			return events, nil
		}
		if len(line) == 0 {
			if err != nil {
				if errors.Is(err, io.EOF) {
					return events, nil
				}
				return nil, err
			}
			continue
		}
		var evt engine.StreamingEvent
		if unmarshalErr := json.Unmarshal(bytes.TrimSpace(line), &evt); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		events = append(events, evt)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil
			}
			return nil, err
		}
	}
}
