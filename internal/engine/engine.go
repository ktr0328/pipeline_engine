package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
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

// EngineConfig describes runtime configuration for the engine.
type EngineConfig struct {
	Providers []ProviderProfile
}

// BasicEngine is a naive single-node engine implementation intended for the v0 milestone.
type BasicEngine struct {
	store        JobStore
	checkpoint   StepCheckpointStore
	cancels      map[string]context.CancelFunc
	mu           sync.Mutex
	pipelineMu   sync.RWMutex
	pipelines    map[PipelineType]*PipelineDef
	jobPipeline  map[string]*PipelineDef
	jobPipeMu    sync.RWMutex
	checkpointMu sync.RWMutex
	checkpoints  map[string]map[StepID][]ResultItem
	providers    *ProviderRegistry
}

// NewBasicEngine returns an Engine implementation backed by the provided store.
func NewBasicEngine(store JobStore) *BasicEngine {
	return NewBasicEngineWithConfig(store, nil)
}

// NewBasicEngineWithConfig wires the engine with the provided configuration.
func NewBasicEngineWithConfig(store JobStore, cfg *EngineConfig) *BasicEngine {
	reg := NewProviderRegistry()
	RegisterDefaultProviderFactories(reg)
	for _, profile := range defaultProviderProfiles() {
		reg.RegisterProfile(profile)
	}
	if cfg != nil {
		for _, profile := range cfg.Providers {
			reg.RegisterProfile(profile)
		}
	}

	return &BasicEngine{
		store:       store,
		checkpoint:  detectCheckpointStore(store),
		cancels:     map[string]context.CancelFunc{},
		pipelines:   map[PipelineType]*PipelineDef{},
		jobPipeline: map[string]*PipelineDef{},
		checkpoints: map[string]map[StepID][]ResultItem{},
		providers:   reg,
	}
}

// RegisterPipeline registers or replaces a pipeline definition.
func (e *BasicEngine) RegisterPipeline(def PipelineDef) {
	if def.Type == "" {
		return
	}
	e.pipelineMu.Lock()
	defer e.pipelineMu.Unlock()
	e.pipelines[def.Type] = clonePipeline(&def)
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

	pipeline := e.pipelineForType(req.PipelineType)
	if req.FromStepID != nil {
		if idx := findStepIndex(pipeline.Steps, *req.FromStepID); idx == -1 {
			return nil, fmt.Errorf("step %s not found in pipeline", *req.FromStepID)
		}
	}

	stepExecs := make([]StepExecution, len(pipeline.Steps))
	for i, step := range pipeline.Steps {
		stepExecs[i] = StepExecution{StepID: step.ID, Status: StepExecPending}
	}
	if len(stepExecs) == 0 {
		stepExecs = []StepExecution{{StepID: StepID("step-1"), Status: StepExecPending}}
	}

	now := time.Now().UTC()
	job := &Job{
		ID:              generateID(),
		PipelineType:    req.PipelineType,
		PipelineVersion: pipeline.Version,
		Status:          JobStatusQueued,
		CreatedAt:       now,
		UpdatedAt:       now,
		Input:           req.Input,
		Mode:            mode,
		ParentJobID:     req.ParentJobID,
		RerunFromStep:   req.FromStepID,
		ReuseUpstream:   req.ReuseUpstream,
		StepExecutions:  stepExecs,
	}

	e.cacheJobPipeline(job.ID, pipeline)

	if err := e.store.CreateJob(job); err != nil {
		e.removeJobPipeline(job.ID)
		return nil, err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	e.setCancel(job.ID, cancel)

	if mode == "sync" {
		e.executeJob(jobCtx, job.ID)
		cancel()
		finalJob, err := e.store.GetJob(job.ID)
		if err != nil {
			return nil, err
		}
		return finalJob, nil
	}

	go func() {
		defer cancel()
		e.executeJob(jobCtx, job.ID)
	}()

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
	defer e.clearCancel(jobID)
	defer e.removeJobPipeline(jobID)

	job, err := e.store.GetJob(jobID)
	if err != nil {
		return
	}

	pipeline := e.loadJobPipeline(jobID)
	if pipeline == nil {
		pipeline = e.pipelineForType(job.PipelineType)
	}
	if len(pipeline.Steps) == 0 {
		pipeline.Steps = []StepDef{
			{ID: StepID("step-1"), Kind: StepKindLLM, Mode: StepModeSingle, OutputType: ContentText, Export: true},
		}
	}

	if len(job.StepExecutions) != len(pipeline.Steps) {
		job.StepExecutions = make([]StepExecution, len(pipeline.Steps))
		for i, step := range pipeline.Steps {
			job.StepExecutions[i] = StepExecution{StepID: step.ID, Status: StepExecPending}
		}
	}

	stepOutputs := make(map[StepID][]ResultItem)
	startIndex := findStartIndex(pipeline, job.RerunFromStep)
	if job.ReuseUpstream && job.ParentJobID != nil {
		if reused := e.loadCheckpoints(*job.ParentJobID); len(reused) > 0 {
			for idx := 0; idx < startIndex && idx < len(pipeline.Steps); idx++ {
				step := pipeline.Steps[idx]
				if items, ok := reused[step.ID]; ok {
					stepOutputs[step.ID] = cloneResultItems(items)
					job.StepExecutions[idx].Status = StepExecSkipped
					if step.Export {
						appendExportedResults(job, items)
					}
				}
			}
		}
	}

	now := time.Now().UTC()
	job.Status = JobStatusRunning
	job.UpdatedAt = now
	if err := e.store.UpdateJob(job); err != nil {
		return
	}

	for idx, step := range pipeline.Steps {
		if job.ReuseUpstream && idx < startIndex {
			continue
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := ensureDependencies(step, stepOutputs); err != nil {
			e.failStep(job, idx, "missing_dependency", err.Error())
			return
		}

		start := time.Now().UTC()
		job.StepExecutions[idx].Status = StepExecRunning
		job.StepExecutions[idx].StartedAt = ptrTime(start)
		if err := e.store.UpdateJob(job); err != nil {
			return
		}

		prompt := buildPrompt(step, job, stepOutputs)
		items, execErr := e.runStep(ctx, job, idx, step, prompt, stepOutputs)
		if execErr != nil {
			code := "step_failed"
			if errors.Is(execErr, context.Canceled) {
				code = "cancelled"
			}
			e.failStep(job, idx, code, execErr.Error())
			return
		}

		finish := time.Now().UTC()
		job.StepExecutions[idx].Status = StepExecSuccess
		job.StepExecutions[idx].FinishedAt = ptrTime(finish)
		job.StepExecutions[idx].Error = nil
		job.UpdatedAt = finish
		stepOutputs[step.ID] = items
		e.saveCheckpoint(job.ID, step.ID, items)
		appendExportedResultsForStep(job, step, items)
		if err := e.store.UpdateJob(job); err != nil {
			return
		}
	}

	job.Status = JobStatusSucceeded
	job.UpdatedAt = time.Now().UTC()
	_ = e.store.UpdateJob(job)
}

func (e *BasicEngine) streamJob(ctx context.Context, ch chan<- StreamingEvent, jobID string) {
	defer close(ch)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	tracker := NewStreamingTracker()
	var lastStatus JobStatus
	for {
		job, err := e.store.GetJob(jobID)
		if err != nil {
			ch <- StreamingEvent{Event: "error", JobID: jobID, Data: err.Error()}
			return
		}

		if job.Status != lastStatus {
			lastStatus = job.Status
		}
		for _, event := range tracker.Diff(job) {
			ch <- event
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

func (e *BasicEngine) cacheJobPipeline(jobID string, def *PipelineDef) {
	if def == nil {
		return
	}
	e.jobPipeMu.Lock()
	defer e.jobPipeMu.Unlock()
	e.jobPipeline[jobID] = clonePipeline(def)
}

func (e *BasicEngine) loadJobPipeline(jobID string) *PipelineDef {
	e.jobPipeMu.RLock()
	defer e.jobPipeMu.RUnlock()
	if def, ok := e.jobPipeline[jobID]; ok {
		return clonePipeline(def)
	}
	return nil
}

func (e *BasicEngine) removeJobPipeline(jobID string) {
	e.jobPipeMu.Lock()
	defer e.jobPipeMu.Unlock()
	delete(e.jobPipeline, jobID)
}

func (e *BasicEngine) saveCheckpoint(jobID string, stepID StepID, items []ResultItem) {
	if e.checkpoint != nil {
		e.checkpoint.SaveCheckpoint(jobID, stepID, items)
		return
	}
	if len(items) == 0 {
		return
	}
	e.checkpointMu.Lock()
	defer e.checkpointMu.Unlock()
	stepMap, ok := e.checkpoints[jobID]
	if !ok {
		stepMap = map[StepID][]ResultItem{}
		e.checkpoints[jobID] = stepMap
	}
	stepMap[stepID] = cloneResultItems(items)
}

func (e *BasicEngine) loadCheckpoints(jobID string) map[StepID][]ResultItem {
	if e.checkpoint != nil {
		return e.checkpoint.LoadCheckpoints(jobID)
	}
	e.checkpointMu.RLock()
	defer e.checkpointMu.RUnlock()
	source, ok := e.checkpoints[jobID]
	if !ok {
		return nil
	}
	result := make(map[StepID][]ResultItem, len(source))
	for stepID, items := range source {
		result[stepID] = cloneResultItems(items)
	}
	return result
}

func (e *BasicEngine) pipelineForType(pt PipelineType) *PipelineDef {
	e.pipelineMu.RLock()
	def, ok := e.pipelines[pt]
	e.pipelineMu.RUnlock()
	if ok {
		return clonePipeline(def)
	}
	return defaultPipeline(pt)
}

func clonePipeline(def *PipelineDef) *PipelineDef {
	if def == nil {
		return defaultPipeline("")
	}
	copyDef := &PipelineDef{
		Type:    def.Type,
		Version: def.Version,
		Steps:   make([]StepDef, len(def.Steps)),
	}
	if copyDef.Version == "" {
		copyDef.Version = "v0"
	}
	if len(def.Steps) == 0 {
		copyDef.Steps = []StepDef{
			{ID: StepID("step-1"), Name: "default", Kind: StepKindLLM, Mode: StepModeSingle, OutputType: ContentText, Export: true},
		}
		return copyDef
	}
	for i, step := range def.Steps {
		cp := step
		if cp.ID == "" {
			cp.ID = StepID(fmt.Sprintf("step-%d", i+1))
		}
		if cp.Kind == "" {
			cp.Kind = StepKindLLM
		}
		if cp.Mode == "" {
			cp.Mode = StepModeSingle
		}
		if cp.OutputType == "" {
			cp.OutputType = ContentText
		}
		copyDef.Steps[i] = cp
	}
	return copyDef
}

func defaultPipeline(pt PipelineType) *PipelineDef {
	return &PipelineDef{
		Type:    pt,
		Version: "v0",
		Steps: []StepDef{
			{ID: StepID("step-1"), Name: "default", Kind: StepKindLLM, Mode: StepModeSingle, OutputType: ContentText, Export: true},
		},
	}
}

func findStepIndex(steps []StepDef, id StepID) int {
	for i, step := range steps {
		if step.ID == id {
			return i
		}
	}
	return -1
}

func findStartIndex(pipeline *PipelineDef, from *StepID) int {
	if pipeline == nil || from == nil {
		return 0
	}
	if idx := findStepIndex(pipeline.Steps, *from); idx >= 0 {
		return idx
	}
	return 0
}

func ensureDependencies(step StepDef, outputs map[StepID][]ResultItem) error {
	for _, dep := range step.DependsOn {
		if _, ok := outputs[dep]; !ok {
			return fmt.Errorf("dependency %s not satisfied for step %s", dep, step.ID)
		}
	}
	return nil
}

type promptContext struct {
	Job      *Job
	Step     StepDef
	Sources  []Source
	Options  *JobOptions
	Previous map[string][]ResultItem
}

func buildPrompt(step StepDef, job *Job, outputs map[StepID][]ResultItem) string {
	if step.Prompt == nil {
		return ""
	}
	ctx := promptContext{
		Job:      job,
		Step:     step,
		Sources:  job.Input.Sources,
		Options:  job.Input.Options,
		Previous: map[string][]ResultItem{},
	}
	for k, v := range outputs {
		ctx.Previous[string(k)] = cloneResultItems(v)
	}

	var b strings.Builder
	if step.Prompt.System != "" {
		b.WriteString(executeTemplateText(step.Prompt.System, ctx))
		b.WriteByte('\n')
	}
	if step.Prompt.User != "" {
		b.WriteString(executeTemplateText(step.Prompt.User, ctx))
	}
	return strings.TrimSpace(b.String())
}

func executeTemplateText(text string, data any) string {
	tpl, err := template.New("prompt").Parse(text)
	if err != nil {
		return text
	}
	var b strings.Builder
	if err := tpl.Execute(&b, data); err != nil {
		return text
	}
	return b.String()
}

func (e *BasicEngine) runStep(ctx context.Context, job *Job, execIdx int, step StepDef, prompt string, outputs map[StepID][]ResultItem) ([]ResultItem, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	time.Sleep(100 * time.Millisecond)

	provider, profile := e.resolveProvider(step)
	inputCtx := ProviderInput{
		Sources:  job.Input.Sources,
		Options:  job.Input.Options,
		Previous: outputs,
	}

	switch step.Mode {
	case StepModeFanOut:
		return e.runFanOutStep(ctx, execIdx, provider, profile, step, job, prompt, inputCtx)
	case StepModePerItem:
		var base []ResultItem
		if len(step.DependsOn) > 0 {
			lastDep := step.DependsOn[len(step.DependsOn)-1]
			if items, ok := outputs[lastDep]; ok {
				base = items
			}
		}
		if len(base) == 0 {
		return e.runFanOutStep(ctx, execIdx, provider, profile, step, job, prompt, inputCtx)
	}
	return e.runPerItemStep(ctx, execIdx, provider, profile, step, job, prompt, inputCtx, base)
	default:
		return e.runSingleStep(ctx, execIdx, provider, profile, step, job, prompt, inputCtx)
	}
}

func (e *BasicEngine) runSingleStep(ctx context.Context, execIdx int, provider Provider, profile ProviderProfile, step StepDef, job *Job, prompt string, input ProviderInput) ([]ResultItem, error) {
	resp, err := e.callProvider(ctx, provider, profile, step, prompt, input)
	if err != nil {
		return nil, err
	}
	e.recordChunks(job, execIdx, resp.Chunks)
	text := resp.Output
	meta := resp.Metadata
	if text == "" {
		text = fmt.Sprintf("step %s processed %d sources", step.ID, len(job.Input.Sources))
	}
	item := buildSingleResult(step, job, prompt, text, meta)
	return []ResultItem{item}, nil
}

func (e *BasicEngine) runFanOutStep(ctx context.Context, execIdx int, provider Provider, profile ProviderProfile, step StepDef, job *Job, prompt string, input ProviderInput) ([]ResultItem, error) {
	if len(job.Input.Sources) == 0 {
		return e.runSingleStep(ctx, execIdx, provider, profile, step, job, prompt, input)
	}
	items := make([]ResultItem, len(job.Input.Sources))
	for i, src := range job.Input.Sources {
		localInput := input
		localInput.Sources = []Source{src}
		resp, err := e.callProvider(ctx, provider, profile, step, prompt, localInput)
		if err != nil {
			return nil, err
		}
		e.recordChunks(job, execIdx, resp.Chunks)
		text := resp.Output
		meta := resp.Metadata
		if text == "" {
			text = fmt.Sprintf("step %s handled source %s", step.ID, src.Label)
		}
		items[i] = buildFanOutResult(step, prompt, src, i, text, meta)
	}
	return items, nil
}

func (e *BasicEngine) runPerItemStep(ctx context.Context, execIdx int, provider Provider, profile ProviderProfile, step StepDef, job *Job, prompt string, input ProviderInput, base []ResultItem) ([]ResultItem, error) {
	items := make([]ResultItem, len(base))
	for i, prev := range base {
		localInput := input
		localInput.Previous = map[StepID][]ResultItem{
			prev.StepID: {prev},
		}
		resp, err := e.callProvider(ctx, provider, profile, step, prompt, localInput)
		if err != nil {
			return nil, err
		}
		e.recordChunks(job, execIdx, resp.Chunks)
		text := resp.Output
		meta := resp.Metadata
		if text == "" {
			shard := ""
			if prev.ShardKey != nil {
				shard = *prev.ShardKey
			} else {
				shard = fmt.Sprintf("%s-%d", step.ID, i)
			}
			text = fmt.Sprintf("step %s refined shard %s", step.ID, shard)
		}
		items[i] = buildPerItemResult(step, prompt, prev, i, text, meta)
	}
	return items, nil
}

func buildSingleResult(step StepDef, job *Job, prompt, text string, meta map[string]any) ResultItem {
	label := step.Name
	if label == "" {
		label = string(step.ID)
	}
	data := map[string]any{
		"text":         text,
		"prompt":       prompt,
		"pipelineType": job.PipelineType,
	}
	mergeMeta(data, meta)
	return ResultItem{
		ID:          generateID(),
		Label:       label,
		StepID:      step.ID,
		Kind:        string(step.Kind),
		ContentType: ensureContentType(step.OutputType),
		Data:        data,
	}
}

func buildFanOutResult(step StepDef, prompt string, src Source, idx int, text string, meta map[string]any) ResultItem {
	label := step.Name
	if label == "" {
		label = string(step.ID)
	}
	data := map[string]any{
		"text":        text,
		"prompt":      prompt,
		"source_kind": src.Kind,
		"source":      src.Content,
	}
	mergeMeta(data, meta)
	shard := fmt.Sprintf("%s-%d", step.ID, idx)
	return ResultItem{
		ID:          generateID(),
		Label:       fmt.Sprintf("%s#%d", label, idx+1),
		StepID:      step.ID,
		ShardKey:    ptrString(shard),
		Kind:        string(step.Kind),
		ContentType: ensureContentType(step.OutputType),
		Data:        data,
	}
}

func buildPerItemResult(step StepDef, prompt string, prev ResultItem, idx int, text string, meta map[string]any) ResultItem {
	shard := fmt.Sprintf("%s-%d", step.ID, idx)
	if prev.ShardKey != nil {
		shard = *prev.ShardKey
	}
	data := map[string]any{
		"text":          text,
		"prompt":        prompt,
		"previous_step": prev.StepID,
	}
	mergeMeta(data, meta)
	return ResultItem{
		ID:          generateID(),
		Label:       fmt.Sprintf("%s#%d", step.Name, idx+1),
		StepID:      step.ID,
		ShardKey:    ptrString(shard),
		Kind:        string(step.Kind),
		ContentType: ensureContentType(step.OutputType),
		Data:        data,
	}
}

func (e *BasicEngine) callProvider(ctx context.Context, provider Provider, profile ProviderProfile, step StepDef, prompt string, input ProviderInput) (ProviderResponse, error) {
	if provider == nil {
		return ProviderResponse{}, nil
	}
	return provider.Call(ctx, ProviderRequest{
		Step:    step,
		Prompt:  prompt,
		Profile: profile,
		Input:   input,
	})
}

func (e *BasicEngine) recordChunks(job *Job, execIdx int, chunks []ProviderChunk) {
	if len(chunks) == 0 || execIdx < 0 || execIdx >= len(job.StepExecutions) {
		return
	}
	stepExec := &job.StepExecutions[execIdx]
	for _, chunk := range chunks {
		index := len(stepExec.Chunks)
		stepExec.Chunks = append(stepExec.Chunks, StepChunk{StepID: stepExec.StepID, Index: index, Content: chunk.Content})
	}
	job.UpdatedAt = time.Now().UTC()
	_ = e.store.UpdateJob(job)
}

func (e *BasicEngine) resolveProvider(step StepDef) (Provider, ProviderProfile) {
	if e.providers == nil {
		return nil, ProviderProfile{}
	}
	provider, profile, err := e.providers.Resolve(step)
	if err != nil {
		return nil, ProviderProfile{}
	}
	return provider, profile
}

func mergeMeta(dst map[string]any, meta map[string]any) {
	if len(meta) == 0 {
		return
	}
	for k, v := range meta {
		dst[k] = v
	}
}

func ensureContentType(ct ContentType) ContentType {
	if ct == "" {
		return ContentText
	}
	return ct
}

func appendExportedResultsForStep(job *Job, step StepDef, items []ResultItem) {
	if !step.Export || len(items) == 0 {
		return
	}
	appendExportedResults(job, items)
}

func appendExportedResults(job *Job, items []ResultItem) {
	if len(items) == 0 {
		return
	}
	if job.Result == nil {
		job.Result = &JobResult{}
	}
	job.Result.Items = append(job.Result.Items, cloneResultItems(items)...)
}

func cloneResultItems(items []ResultItem) []ResultItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]ResultItem, len(items))
	for i, item := range items {
		copyItem := item
		if item.ShardKey != nil {
			key := *item.ShardKey
			copyItem.ShardKey = &key
		}
		if data, ok := item.Data.(map[string]any); ok {
			copyItem.Data = cloneMap(data)
		}
		cloned[i] = copyItem
	}
	return cloned
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	clone := make(map[string]any, len(m))
	for k, v := range m {
		clone[k] = v
	}
	return clone
}

func ptrString(s string) *string {
	return &s
}

func (e *BasicEngine) failStep(job *Job, idx int, code, message string) {
	if idx < 0 || idx >= len(job.StepExecutions) {
		return
	}
	finish := time.Now().UTC()
	exec := &job.StepExecutions[idx]
	exec.Status = StepExecFailed
	exec.FinishedAt = ptrTime(finish)
	exec.Error = &JobError{Code: code, Message: message}
	job.Status = JobStatusFailed
	job.Error = exec.Error
	job.UpdatedAt = finish
	_ = e.store.UpdateJob(job)
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
