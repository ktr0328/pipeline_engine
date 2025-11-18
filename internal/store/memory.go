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
	mu   sync.RWMutex
	jobs map[string]*engine.Job
}

// NewMemoryStore initializes a new in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{jobs: map[string]*engine.Job{}}
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
