package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/example/pipeline-engine/internal/engine"
	gosdk "github.com/example/pipeline-engine/pkg/sdk/go"
)

func TestAdapterInitialize(t *testing.T) {
	client := &stubClient{}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	var buf bytes.Buffer
	adapter := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := adapter.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Result == nil {
		t.Fatalf("expected result, got nil")
	}
}

func TestAdapterStartPipeline(t *testing.T) {
	job := sampleJob("job-123")
	client := &stubClient{createJobResult: job}
	req := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"toolName":"startPipeline","arguments":{"pipeline_type":"demo","input":{"sources":[{"kind":"note","label":"m","content":"x"}]}}}}`
	var buf bytes.Buffer
	a := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", resp.Result)
	}
	jobData, ok := payload["job"].(map[string]any)
	if !ok || jobData["id"] != "job-123" {
		t.Fatalf("job result mismatch: %+v", jobData)
	}
	if client.createReq == nil || client.createReq.PipelineType != "demo" {
		t.Fatalf("expected client to receive request, got %#v", client.createReq)
	}
}

func TestAdapterStartPipelineStreamEmitsEvents(t *testing.T) {
	job := sampleJob("job-stream")
	events := []engine.StreamingEvent{
		{Event: "provider_chunk", JobID: job.ID, Data: map[string]string{"content": "chunk"}},
		{Event: "item_completed", JobID: job.ID, Data: map[string]string{"label": "default"}},
	}
	client := &stubClient{
		streamEvents:    events,
		streamJobResult: job,
	}
	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"toolName":"startPipeline","arguments":{"pipeline_type":"demo","input":{"sources":[{"kind":"note","label":"m","content":"x"}]},"stream":true}}}`
	var buf bytes.Buffer
	a := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var eventCount int
	var resp rpcResponse
	for _, line := range lines {
		if strings.Contains(line, `"method":"tool_event"`) {
			eventCount++
			var notification rpcNotification
			if err := json.Unmarshal([]byte(line), &notification); err != nil {
				t.Fatalf("decode tool event: %v", err)
			}
			params, _ := notification.Params.(map[string]any)
			if params["kind"] == nil {
				t.Fatalf("expected kind in params: %#v", params)
			}
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	// Expect job_queued + len(events) notifications.
	if eventCount != len(events)+1 {
		t.Fatalf("expected %d tool events, got %d", len(events)+1, eventCount)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	if payload["job"] == nil {
		t.Fatalf("expected job payload in result")
	}
}

func TestAdapterGetJob(t *testing.T) {
	job := sampleJob("job-xyz")
	client := &stubClient{getJobResult: job}
	req := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"toolName":"getJob","arguments":{"job_id":"job-xyz"}}}`
	var buf bytes.Buffer
	a := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestAdapterToolsList(t *testing.T) {
	client := &stubClient{}
	req := strings.Join([]string{
		`{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}`,
	}, "\n")
	var buf bytes.Buffer
	a := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	payload, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", resp.Result)
	}
	tools, ok := payload["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected tools list, got %#v", payload["tools"])
	}
}

func TestAdapterStreamJob(t *testing.T) {
	events := []engine.StreamingEvent{
		{Event: "provider_chunk", JobID: "job-9", Data: map[string]string{"content": "chunk"}},
		{Event: "job_completed", JobID: "job-9", Data: map[string]string{"status": "succeeded"}},
	}
	client := &stubClient{streamExisting: events}
	req := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"toolName":"streamJob","arguments":{"job_id":"job-9"}}}`
	var buf bytes.Buffer
	a := NewAdapter(Options{
		Client: client,
		Reader: strings.NewReader(req),
		Writer: &buf,
	})
	if err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var kindValues []string
	var resp rpcResponse
	for _, line := range lines {
		if strings.Contains(line, `"method":"tool_event"`) {
			var notification rpcNotification
			if err := json.Unmarshal([]byte(line), &notification); err != nil {
				t.Fatalf("decode tool event: %v", err)
			}
			params, _ := notification.Params.(map[string]any)
			if kind, _ := params["kind"].(string); kind != "" {
				kindValues = append(kindValues, kind)
			}
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
	}
	if len(kindValues) != len(events) {
		t.Fatalf("expected %d tool events, got %d", len(events), len(kindValues))
	}
	if kindValues[0] != "chunk" || kindValues[1] != "status" {
		t.Fatalf("unexpected kind values: %+v", kindValues)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error response: %+v", resp.Error)
	}
}

type stubClient struct {
	createReq       *engine.JobRequest
	createJobResult *engine.Job
	streamEvents    []engine.StreamingEvent
	streamJobResult *engine.Job
	streamExisting  []engine.StreamingEvent
	getJobResult    *engine.Job
	cancelJobResult *engine.Job
	rerunJobResult  *engine.Job
	profiles        []engine.ProviderProfile
}

func (s *stubClient) CreateJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
	s.createReq = &req
	return s.createJobResult, nil
}

func (s *stubClient) StreamJob(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
	ch := make(chan engine.StreamingEvent, len(s.streamEvents))
	go func(events []engine.StreamingEvent) {
		defer close(ch)
		for _, evt := range events {
			ch <- evt
		}
	}(s.streamEvents)
	return ch, s.streamJobResult, nil
}

func (s *stubClient) GetJob(ctx context.Context, jobID string) (*engine.Job, error) {
	return s.getJobResult, nil
}

func (s *stubClient) CancelJob(ctx context.Context, jobID string, reason string) (*engine.Job, error) {
	return s.cancelJobResult, nil
}

func (s *stubClient) RerunJob(ctx context.Context, jobID string, payload gosdk.RerunRequest) (*engine.Job, error) {
	return s.rerunJobResult, nil
}

func (s *stubClient) UpsertProviderProfile(ctx context.Context, profile engine.ProviderProfile) error {
	s.profiles = append(s.profiles, profile)
	return nil
}

func (s *stubClient) StreamExistingJob(ctx context.Context, jobID string, afterSeq uint64) (<-chan engine.StreamingEvent, error) {
	events := s.streamExisting
	if len(events) == 0 {
		events = s.streamEvents
	}
	ch := make(chan engine.StreamingEvent, len(events))
	go func() {
		defer close(ch)
		for _, evt := range events {
			ch <- evt
		}
	}()
	return ch, nil
}

func sampleJob(id string) *engine.Job {
	now := time.Unix(0, 0).UTC()
	return &engine.Job{
		ID:           id,
		PipelineType: "demo",
		Status:       engine.JobStatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
		Input: engine.JobInput{
			Sources: []engine.Source{{Kind: engine.SourceKindNote, Label: "m", Content: "x"}},
		},
	}
}
