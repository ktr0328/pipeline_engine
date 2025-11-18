package store

import (
	"errors"
	"sync"

	"github.com/example/pipeline-engine/internal/engine"
)

var (
	// ErrJobExists indicates that a job with the same ID already exists.
	ErrJobExists = errors.New("job already exists")
	// ErrJobNotFound indicates that the requested job does not exist.
	ErrJobNotFound = errors.New("job not found")
)

// MemoryStore keeps job data in-memory for local development.
type MemoryStore struct {
	mu           sync.RWMutex
	jobs         map[string]*engine.Job
	checkpoints  map[string]map[engine.StepID][]engine.ResultItem
}

// NewMemoryStore initializes a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:        map[string]*engine.Job{},
		checkpoints: map[string]map[engine.StepID][]engine.ResultItem{},
	}
}

// CreateJob stores a brand-new job.
func (s *MemoryStore) CreateJob(job *engine.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; ok {
		return ErrJobExists
	}

	s.jobs[job.ID] = cloneJob(job)
	return nil
}

// UpdateJob overwrites the stored job with the provided definition.
func (s *MemoryStore) UpdateJob(job *engine.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[job.ID]; !ok {
		return ErrJobNotFound
	}

	s.jobs[job.ID] = cloneJob(job)
	return nil
}

// GetJob returns the job that matches the provided identifier.
func (s *MemoryStore) GetJob(id string) (*engine.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, ok := s.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}

	return cloneJob(job), nil
}

// ListJobs returns all stored jobs.
func (s *MemoryStore) ListJobs() ([]*engine.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*engine.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		result = append(result, cloneJob(job))
	}
	return result, nil
}

func cloneJob(job *engine.Job) *engine.Job {
	if job == nil {
		return nil
	}

	copyJob := *job
	if job.StepExecutions != nil {
		steps := make([]engine.StepExecution, len(job.StepExecutions))
		copy(steps, job.StepExecutions)
		copyJob.StepExecutions = steps
	}
	if job.Result != nil {
		result := *job.Result
		if job.Result.Items != nil {
			items := make([]engine.ResultItem, len(job.Result.Items))
			copy(items, job.Result.Items)
			result.Items = items
		}
		copyJob.Result = &result
	}
	return &copyJob
}

// Ensure MemoryStore implements the JobStore interface.
var _ engine.JobStore = (*MemoryStore)(nil)

// StepCheckpointStore exposes persistence operations for step checkpoints.
type StepCheckpointStore interface {
	SaveCheckpoint(jobID string, stepID engine.StepID, items []engine.ResultItem)
	LoadCheckpoints(jobID string) map[engine.StepID][]engine.ResultItem
	ClearCheckpoints(jobID string)
}

func (s *MemoryStore) SaveCheckpoint(jobID string, stepID engine.StepID, items []engine.ResultItem) {
	if len(items) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.checkpoints[jobID]; !ok {
		s.checkpoints[jobID] = map[engine.StepID][]engine.ResultItem{}
	}
	s.checkpoints[jobID][stepID] = cloneResultItems(items)
}

func (s *MemoryStore) LoadCheckpoints(jobID string) map[engine.StepID][]engine.ResultItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	source := s.checkpoints[jobID]
	if source == nil {
		return nil
	}
	result := make(map[engine.StepID][]engine.ResultItem, len(source))
	for stepID, items := range source {
		result[stepID] = cloneResultItems(items)
	}
	return result
}

func (s *MemoryStore) ClearCheckpoints(jobID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.checkpoints, jobID)
}

func cloneResultItems(items []engine.ResultItem) []engine.ResultItem {
	if len(items) == 0 {
		return nil
	}
	result := make([]engine.ResultItem, len(items))
	for i, item := range items {
		copyItem := item
		if item.ShardKey != nil {
			key := *item.ShardKey
			copyItem.ShardKey = &key
		}
		if data, ok := item.Data.(map[string]any); ok {
			copyItem.Data = cloneMap(data)
		}
		result[i] = copyItem
	}
	return result
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
