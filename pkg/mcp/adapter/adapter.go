package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"

	"github.com/example/pipeline-engine/internal/engine"
	gosdk "github.com/example/pipeline-engine/pkg/sdk/go"
)

const (
	jsonRPCVersion = "2.0"

	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603
)

// EngineClient represents the subset of the Go SDK consumed by the MCP adapter.
type EngineClient interface {
	CreateJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error)
	StreamJob(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error)
	GetJob(ctx context.Context, jobID string) (*engine.Job, error)
	CancelJob(ctx context.Context, jobID string, reason string) (*engine.Job, error)
	RerunJob(ctx context.Context, jobID string, payload gosdk.RerunRequest) (*engine.Job, error)
	UpsertProviderProfile(ctx context.Context, profile engine.ProviderProfile) error
	StreamExistingJob(ctx context.Context, jobID string, afterSeq uint64) (<-chan engine.StreamingEvent, error)
}

// SDKClient adapts the Go SDK client to EngineClient.
type SDKClient struct {
	client *gosdk.Client
}

// NewSDKClient wraps the Go SDK client for use with the adapter.
func NewSDKClient(client *gosdk.Client) *SDKClient {
	return &SDKClient{client: client}
}

func (c *SDKClient) CreateJob(ctx context.Context, req engine.JobRequest) (*engine.Job, error) {
	return c.client.CreateJob(ctx, req)
}

func (c *SDKClient) StreamJob(ctx context.Context, req engine.JobRequest) (<-chan engine.StreamingEvent, *engine.Job, error) {
	return c.client.StreamJobs(ctx, req)
}

func (c *SDKClient) GetJob(ctx context.Context, jobID string) (*engine.Job, error) {
	return c.client.GetJob(ctx, jobID)
}

func (c *SDKClient) CancelJob(ctx context.Context, jobID string, reason string) (*engine.Job, error) {
	return c.client.CancelJob(ctx, jobID, reason)
}

func (c *SDKClient) RerunJob(ctx context.Context, jobID string, payload gosdk.RerunRequest) (*engine.Job, error) {
	return c.client.RerunJob(ctx, jobID, payload)
}

func (c *SDKClient) UpsertProviderProfile(ctx context.Context, profile engine.ProviderProfile) error {
	return c.client.UpsertProviderProfile(ctx, profile)
}

func (c *SDKClient) StreamExistingJob(ctx context.Context, jobID string, afterSeq uint64) (<-chan engine.StreamingEvent, error) {
	if afterSeq > 0 {
		return c.client.StreamJobByID(ctx, jobID, afterSeq)
	}
	return c.client.StreamJobByID(ctx, jobID)
}

// Tool describes a MCP tool entry returned from tools/list.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]any         `json:"inputSchema,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Options configures the Adapter.
type Options struct {
	Client EngineClient
	Reader io.Reader
	Writer io.Writer
	Logger *log.Logger
	Tools  []Tool
}

// Adapter bridges MCP JSON-RPC traffic to the Pipeline Engine SDK.
type Adapter struct {
	client EngineClient
	reader io.Reader
	writer io.Writer
	logger *log.Logger
	tools  []Tool
	enc    *json.Encoder
	dec    *json.Decoder
	mu     sync.Mutex
}

// NewAdapter creates a new Adapter.
func NewAdapter(opts Options) *Adapter {
	reader := opts.Reader
	if reader == nil {
		reader = os.Stdin
	}
	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "[mcp] ", log.LstdFlags)
	}
	tools := opts.Tools
	if len(tools) == 0 {
		tools = defaultTools()
	}
	return &Adapter{
		client: opts.Client,
		reader: reader,
		writer: writer,
		logger: logger,
		tools:  tools,
		enc:    json.NewEncoder(writer),
		dec:    json.NewDecoder(reader),
	}
}

// Run starts processing MCP JSON-RPC messages until the context is cancelled or EOF.
func (a *Adapter) Run(ctx context.Context) error {
	if a.client == nil {
		return errors.New("adapter client is nil")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var req rpcRequest
		if err := a.dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if req.JSONRPC != "" && req.JSONRPC != jsonRPCVersion {
			a.respondError(req.ID, errCodeInvalidRequest, "invalid jsonrpc version", nil)
			continue
		}
		if req.Method == "" {
			a.respondError(req.ID, errCodeInvalidRequest, "method is required", nil)
			continue
		}

		switch req.Method {
		case "initialize":
			result := map[string]any{
				"protocolVersion": "0.1",
				"serverInfo": map[string]string{
					"name":    "pipeline-engine-mcp",
					"version": "0.1.0",
				},
				"capabilities": map[string]any{
					"prompts":      map[string]bool{"list": false},
					"resources":    map[string]bool{"list": false},
					"tools":        map[string]bool{"list": true, "call": true},
					"experimental": map[string]bool{},
				},
			}
			a.respondResult(req.ID, result)
		case "tools/list":
			result := map[string]any{"tools": a.tools}
			a.respondResult(req.ID, result)
		case "tools/call":
			a.handleToolCall(ctx, req)
		default:
			a.respondError(req.ID, errCodeMethodNotFound, "method not implemented", nil)
		}
	}
}

func (a *Adapter) handleToolCall(ctx context.Context, req rpcRequest) {
	if len(req.Params) == 0 {
		a.respondError(req.ID, errCodeInvalidParams, "missing params", nil)
		return
	}
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		a.respondError(req.ID, errCodeInvalidParams, "invalid params", err.Error())
		return
	}
	switch params.ToolName {
	case "startPipeline":
		a.handleStartPipeline(ctx, req.ID, params.Arguments)
	case "getJob":
		a.handleGetJob(ctx, req.ID, params.Arguments)
	case "cancelJob":
		a.handleCancelJob(ctx, req.ID, params.Arguments)
	case "rerunJob":
		a.handleRerunJob(ctx, req.ID, params.Arguments)
	case "upsertProviderProfile":
		a.handleUpsertProviderProfile(ctx, req.ID, params.Arguments)
	case "streamJob":
		a.handleStreamJob(ctx, req.ID, params.Arguments)
	default:
		a.respondError(req.ID, errCodeMethodNotFound, "tool not found", params.ToolName)
	}
}

func (a *Adapter) handleStartPipeline(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var args startPipelineArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid arguments", err.Error())
		return
	}
	if args.PipelineType == "" {
		a.respondError(id, errCodeInvalidParams, "pipeline_type is required", nil)
		return
	}
	req := engine.JobRequest{
		PipelineType:  engine.PipelineType(args.PipelineType),
		Input:         args.Input,
		Mode:          args.Mode,
		FromStepID:    args.FromStepID,
		ReuseUpstream: args.ReuseUpstream,
	}
	if args.Stream {
		eventCh, job, err := a.client.StreamJob(ctx, req)
		if err != nil {
			a.respondError(id, errCodeInternalError, "stream job failed", err.Error())
			return
		}
		a.emitToolEvent("startPipeline", engine.StreamingEvent{
			Event: "job_queued",
			JobID: job.ID,
			Data:  job,
		})
		for evt := range eventCh {
			select {
			case <-ctx.Done():
				return
			default:
			}
			a.emitToolEvent("startPipeline", evt)
		}
		a.respondResult(id, map[string]any{
			"job": job,
		})
		return
	}
	job, err := a.client.CreateJob(ctx, req)
	if err != nil {
		a.respondError(id, errCodeInternalError, "create job failed", err.Error())
		return
	}
	a.respondResult(id, map[string]any{"job": job})
}

func (a *Adapter) handleGetJob(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var args getJobArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid arguments", err.Error())
		return
	}
	if args.JobID == "" {
		a.respondError(id, errCodeInvalidParams, "job_id is required", nil)
		return
	}
	job, err := a.client.GetJob(ctx, args.JobID)
	if err != nil {
		a.respondError(id, errCodeInternalError, "get job failed", err.Error())
		return
	}
	a.respondResult(id, map[string]any{"job": job})
}

func (a *Adapter) handleCancelJob(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var args cancelJobArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid arguments", err.Error())
		return
	}
	if args.JobID == "" {
		a.respondError(id, errCodeInvalidParams, "job_id is required", nil)
		return
	}
	job, err := a.client.CancelJob(ctx, args.JobID, args.Reason)
	if err != nil {
		a.respondError(id, errCodeInternalError, "cancel job failed", err.Error())
		return
	}
	a.respondResult(id, map[string]any{"job": job})
}

func (a *Adapter) handleRerunJob(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var args rerunJobArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid arguments", err.Error())
		return
	}
	if args.JobID == "" {
		a.respondError(id, errCodeInvalidParams, "job_id is required", nil)
		return
	}
	req := gosdk.RerunRequest{
		FromStepID:    args.FromStepID,
		ReuseUpstream: args.ReuseUpstream,
		OverrideInput: args.OverrideInput,
	}
	job, err := a.client.RerunJob(ctx, args.JobID, req)
	if err != nil {
		a.respondError(id, errCodeInternalError, "rerun job failed", err.Error())
		return
	}
	a.respondResult(id, map[string]any{"job": job})
}

func (a *Adapter) handleUpsertProviderProfile(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var profile engine.ProviderProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid profile", err.Error())
		return
	}
	if profile.ID == "" {
		a.respondError(id, errCodeInvalidParams, "id is required", nil)
		return
	}
	if err := a.client.UpsertProviderProfile(ctx, profile); err != nil {
		a.respondError(id, errCodeInternalError, "upsert provider failed", err.Error())
		return
	}
	a.respondResult(id, map[string]any{"profile": profile})
}

func (a *Adapter) handleStreamJob(ctx context.Context, id json.RawMessage, raw json.RawMessage) {
	var args streamJobArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		a.respondError(id, errCodeInvalidParams, "invalid arguments", err.Error())
		return
	}
	if args.JobID == "" {
		a.respondError(id, errCodeInvalidParams, "job_id is required", nil)
		return
	}
	eventCh, err := a.client.StreamExistingJob(ctx, args.JobID, args.AfterSeq)
	if err != nil {
		a.respondError(id, errCodeInternalError, "stream job failed", err.Error())
		return
	}
	for evt := range eventCh {
		select {
		case <-ctx.Done():
			return
		default:
		}
		a.emitToolEvent("streamJob", evt)
	}
	a.respondResult(id, map[string]any{"job_id": args.JobID})
}

func (a *Adapter) respondResult(id json.RawMessage, result interface{}) {
	if len(id) == 0 {
		return
	}
	resp := rpcResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.enc.Encode(&resp)
}

func (a *Adapter) respondError(id json.RawMessage, code int, message string, data interface{}) {
	if len(id) == 0 {
		return
	}
	resp := rpcResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.enc.Encode(&resp)
}

func (a *Adapter) emitToolEvent(toolName string, evt engine.StreamingEvent) {
	notification := rpcNotification{
		JSONRPC: jsonRPCVersion,
		Method:  "tool_event",
		Params: map[string]any{
			"toolName": toolName,
			"event":    evt.Event,
			"kind":     classifyEventKind(evt.Event),
			"seq":      evt.Seq,
			"payload":  evt,
		},
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.enc.Encode(&notification)
}

func classifyEventKind(eventName string) string {
	switch eventName {
	case "provider_chunk":
		return "chunk"
	case "item_completed":
		return "result"
	case "error", "job_failed", "step_failed":
		return "error"
	default:
		return "status"
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type toolCallParams struct {
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
}

type startPipelineArgs struct {
	PipelineType  string          `json:"pipeline_type"`
	Input         engine.JobInput `json:"input"`
	Mode          string          `json:"mode,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	FromStepID    *engine.StepID  `json:"from_step_id,omitempty"`
	ReuseUpstream bool            `json:"reuse_upstream,omitempty"`
}

type getJobArgs struct {
	JobID string `json:"job_id"`
}

type cancelJobArgs struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason,omitempty"`
}

type rerunJobArgs struct {
	JobID         string           `json:"job_id"`
	FromStepID    *engine.StepID   `json:"from_step_id,omitempty"`
	ReuseUpstream bool             `json:"reuse_upstream,omitempty"`
	OverrideInput *engine.JobInput `json:"override_input,omitempty"`
}

type streamJobArgs struct {
	JobID    string `json:"job_id"`
	AfterSeq uint64 `json:"after_seq,omitempty"`
}

func defaultTools() []Tool {
	return []Tool{
		{
			Name:        "startPipeline",
			Description: "Create a new pipeline job via /v1/jobs",
			InputSchema: map[string]any{
				"type": "object",
				"required": []string{
					"pipeline_type",
					"input",
				},
				"properties": map[string]any{
					"pipeline_type": map[string]string{"type": "string"},
					"input":         map[string]string{"type": "object"},
					"mode":          map[string]string{"type": "string"},
					"stream":        map[string]string{"type": "boolean"},
				},
			},
		},
		{
			Name:        "getJob",
			Description: "Fetch job details via /v1/jobs/{id}",
			InputSchema: map[string]any{
				"type":       "object",
				"required":   []string{"job_id"},
				"properties": map[string]any{"job_id": map[string]string{"type": "string"}},
			},
		},
		{
			Name:        "cancelJob",
			Description: "Cancel a running job via /v1/jobs/{id}/cancel",
			InputSchema: map[string]any{
				"type":       "object",
				"required":   []string{"job_id"},
				"properties": map[string]any{"job_id": map[string]string{"type": "string"}, "reason": map[string]string{"type": "string"}},
			},
		},
		{
			Name:        "rerunJob",
			Description: "Rerun a job via /v1/jobs/{id}/rerun",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"job_id"},
				"properties": map[string]any{
					"job_id":         map[string]string{"type": "string"},
					"from_step_id":   map[string]string{"type": "string"},
					"reuse_upstream": map[string]string{"type": "boolean"},
				},
			},
		},
		{
			Name:        "upsertProviderProfile",
			Description: "Update ProviderProfile via /v1/config/providers",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"id", "kind"},
				"properties": map[string]any{
					"id":            map[string]string{"type": "string"},
					"kind":          map[string]string{"type": "string"},
					"base_uri":      map[string]string{"type": "string"},
					"api_key":       map[string]string{"type": "string"},
					"default_model": map[string]string{"type": "string"},
				},
			},
		},
		{
			Name:        "streamJob",
			Description: "Stream NDJSON events for an existing job via /v1/jobs/{id}/stream",
			InputSchema: map[string]any{
				"type":       "object",
				"required":   []string{"job_id"},
				"properties": map[string]any{"job_id": map[string]string{"type": "string"}},
			},
		},
	}
}
