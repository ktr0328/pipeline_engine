package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// JobRequest represents the minimal payload required to start a job.
type JobRequest struct {
	PipelineType  PipelineType `json:"pipeline_type"`
	Input         JobInput     `json:"input"`
	Mode          string       `json:"mode,omitempty"`
	ParentJobID   *string      `json:"parent_job_id,omitempty"`
	FromStepID    *StepID      `json:"from_step_id,omitempty"`
	ReuseUpstream bool         `json:"reuse_upstream,omitempty"`
}

// Engine is the contract exposed to consumers such as the HTTP server.
type Engine interface {
	RunJob(ctx context.Context, req JobRequest) (*Job, error)
	RunJobStream(ctx context.Context, req JobRequest) (<-chan StreamingEvent, *Job, error)
	CancelJob(ctx context.Context, jobID string, reason string) error
	GetJob(ctx context.Context, jobID string) (*Job, error)
}

// JobStore is the minimal persistence contract required by the engine.
type JobStore interface {
	CreateJob(job *Job) error
	UpdateJob(job *Job) error
	GetJob(id string) (*Job, error)
	ListJobs() ([]*Job, error)
}

// BasicEngine is a naive single-node engine implementation intended for the v0 milestone.
type BasicEngine struct {
	store   JobStore
	cancels map[string]context.CancelFunc
	mu      sync.Mutex
}

// NewBasicEngine returns an Engine implementation backed by the provided store.
func NewBasicEngine(store JobStore) *BasicEngine {
	return &BasicEngine{
		store:   store,
		cancels: map[string]context.CancelFunc{},
	}
}

// RunJob creates a new job and schedules it for asynchronous execution.
func (e *BasicEngine) RunJob(ctx context.Context, req JobRequest) (*Job, error) {
	if req.PipelineType == "" {
		return nil, errors.New("pipeline_type is required")
	}

	mode := req.Mode
	if mode == "" {
		mode = "async"
	}

	now := time.Now().UTC()
	initialStepID := StepID("step-1")
	if req.FromStepID != nil && *req.FromStepID != "" {
		initialStepID = *req.FromStepID
	}
	job := &Job{
		ID:              generateID(),
		PipelineType:    req.PipelineType,
		PipelineVersion: "v0",
		Status:          JobStatusQueued,
		CreatedAt:       now,
		UpdatedAt:       now,
		Input:           req.Input,
		Mode:            mode,
		ParentJobID:     req.ParentJobID,
		RerunFromStep:   req.FromStepID,
		ReuseUpstream:   req.ReuseUpstream,
		StepExecutions: []StepExecution{
			{StepID: initialStepID, Status: StepExecPending},
		},
	}

	if err := e.store.CreateJob(job); err != nil {
		return nil, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	e.setCancel(job.ID, cancel)

	go e.executeJob(jobCtx, job.ID)

	return job, nil
}

// RunJobStream starts a job and returns a channel that emits status updates.
func (e *BasicEngine) RunJobStream(ctx context.Context, req JobRequest) (<-chan StreamingEvent, *Job, error) {
	job, err := e.RunJob(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	events := make(chan StreamingEvent)
	go e.streamJob(ctx, events, job.ID)
	return events, job, nil
}

// CancelJob attempts to cancel a running or queued job.
func (e *BasicEngine) CancelJob(ctx context.Context, jobID string, reason string) error {
	job, err := e.store.GetJob(jobID)
	if err != nil {
		return err
	}

	if isTerminal(job.Status) {
		return nil
	}

	if reason == "" {
		reason = "cancelled by user"
	}

	cancel := e.getCancel(jobID)
	if cancel != nil {
		cancel()
	}

	now := time.Now().UTC()
	job.Status = JobStatusCancelled
	job.Error = &JobError{Code: "cancelled", Message: reason}
	job.UpdatedAt = now

	for i := range job.StepExecutions {
		if job.StepExecutions[i].Status == StepExecRunning || job.StepExecutions[i].Status == StepExecPending {
			job.StepExecutions[i].Status = StepExecCancelled
			job.StepExecutions[i].FinishedAt = &now
		}
	}

	if err := e.store.UpdateJob(job); err != nil {
		return err
	}

	e.clearCancel(jobID)
	return nil
}

// GetJob loads a job from the backing store.
func (e *BasicEngine) GetJob(ctx context.Context, jobID string) (*Job, error) {
	return e.store.GetJob(jobID)
}

func (e *BasicEngine) executeJob(ctx context.Context, jobID string) {
	job, err := e.store.GetJob(jobID)
	if err != nil {
		return
	}

	now := time.Now().UTC()
	job.Status = JobStatusRunning
	job.UpdatedAt = now
	if len(job.StepExecutions) > 0 {
		job.StepExecutions[0].Status = StepExecRunning
		job.StepExecutions[0].StartedAt = ptrTime(now)
	}

	if err := e.store.UpdateJob(job); err != nil {
		return
	}

	workDone := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(workDone)
	}()

	select {
	case <-ctx.Done():
		return
	case <-workDone:
	}

	finish := time.Now().UTC()
	job.Status = JobStatusSucceeded
	job.UpdatedAt = finish
	dummyText := "dummy response from engine"
	job.Result = &JobResult{
		Items: []ResultItem{
			{
				ID:          generateID(),
				Label:       "summary",
				StepID:      job.StepExecutions[0].StepID,
				Kind:        "text",
				ContentType: ContentText,
				Data: map[string]any{
					"text": dummyText,
				},
			},
		},
	}
	if len(job.StepExecutions) > 0 {
		job.StepExecutions[0].Status = StepExecSuccess
		job.StepExecutions[0].FinishedAt = ptrTime(finish)
	}

	_ = e.store.UpdateJob(job)
	e.clearCancel(jobID)
}

func (e *BasicEngine) streamJob(ctx context.Context, ch chan<- StreamingEvent, jobID string) {
	defer close(ch)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	var lastStatus JobStatus
	for {
		job, err := e.store.GetJob(jobID)
		if err != nil {
			ch <- StreamingEvent{Event: "error", JobID: jobID, Data: err.Error()}
			return
		}

		if job.Status != lastStatus {
			lastStatus = job.Status
			ch <- StreamingEvent{Event: "job_status", JobID: job.ID, Data: job}
		}

		if isTerminal(job.Status) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *BasicEngine) setCancel(jobID string, cancel context.CancelFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancels[jobID] = cancel
}

func (e *BasicEngine) getCancel(jobID string) context.CancelFunc {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancels[jobID]
}

func (e *BasicEngine) clearCancel(jobID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cancels, jobID)
}

func isTerminal(status JobStatus) bool {
	switch status {
	case JobStatusSucceeded, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
